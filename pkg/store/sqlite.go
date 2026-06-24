// Package store — SQLiteAuditSink provides durable persistence for admission decisions.
//
// Architecture position:
//   AuditRingBuffer  — in-memory, fast; serves the live /api/audit dashboard queries.
//   JSONL sink       — append-only forensic log; survives restarts as a flat file.
//   SQLiteAuditSink  — structured, queryable, survives restarts; this file.
//
// The three are independent push targets wired together via SetPushHook chains in
// cmd/main.go and cmd/claude_hook.go. Any of them can be absent without breaking the
// others.
//
// Schema:
//   sessions  — one row per governed agent session; risk score updated on each decision.
//   decisions — one row per admission decision; includes HITL approval fields.
//
// In-process hook writes to the same DB file as the server. When the server starts
// later it reads the hook's history with no sync required.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite" // registers "sqlite" driver via database/sql
)

// SQLiteAuditSink writes AuditRecords to a local SQLite database.
type SQLiteAuditSink struct {
	db *sql.DB
	mu sync.Mutex // SQLite allows one writer at a time; this serialises pushes.
}

// NewSQLiteAuditSink opens (creating if necessary) the database at dbPath and
// applies the schema migration. The caller is responsible for calling Close.
func NewSQLiteAuditSink(dbPath string) (*SQLiteAuditSink, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", dbPath, err)
	}
	// One connection prevents SQLITE_BUSY on concurrent writes from hook subprocesses.
	db.SetMaxOpenConns(1)
	// Enable WAL so readers don't block the writer.
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	s := &SQLiteAuditSink{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *SQLiteAuditSink) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id           TEXT    PRIMARY KEY,
			started_at   DATETIME NOT NULL,
			agent        TEXT,
			risk_score   REAL    DEFAULT 0,
			tokens_used  INTEGER DEFAULT 0,
			cost_usd     REAL    DEFAULT 0,
			active       BOOLEAN DEFAULT 1
		);

		CREATE TABLE IF NOT EXISTS decisions (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			request_uid    TEXT,
			session_id     TEXT    NOT NULL,
			agent_id       TEXT,
			ts             DATETIME NOT NULL,
			tool           TEXT    NOT NULL,
			decision       TEXT    NOT NULL,
			policy_hit     TEXT,
			reason         TEXT,
			pipeline_ms    INTEGER,
			risk_score     REAL,
			registered     BOOLEAN,
			policy_version TEXT,
			approval_status TEXT,
			reviewed_by    TEXT,
			reviewed_at    DATETIME,
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		);

		CREATE INDEX IF NOT EXISTS idx_decisions_session  ON decisions(session_id);
		CREATE INDEX IF NOT EXISTS idx_decisions_ts       ON decisions(ts DESC);
		CREATE INDEX IF NOT EXISTS idx_decisions_decision ON decisions(decision);
	`)
	return err
}

// Push writes rec to the decisions table and upserts the corresponding session row.
// Safe to call from multiple goroutines; writes are serialised by an internal mutex.
func (s *SQLiteAuditSink) Push(rec AuditRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Upsert session — create if new, update risk_score on every decision.
	s.db.Exec(`
		INSERT INTO sessions (id, started_at, agent, risk_score, active)
		VALUES (?, ?, ?, ?, 1)
		ON CONFLICT(id) DO UPDATE SET
			risk_score = excluded.risk_score,
			active = 1
	`, rec.SessionID, rec.Timestamp, rec.AgentID, rec.RiskScore)

	// Insert decision row.
	s.db.Exec(`
		INSERT INTO decisions
			(request_uid, session_id, agent_id, ts, tool, decision, policy_hit,
			 reason, pipeline_ms, risk_score, registered, policy_version)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
	`,
		rec.RequestUID, rec.SessionID, rec.AgentID, rec.Timestamp,
		rec.ToolName, rec.Decision, rec.PolicyHit,
		rec.Reason, rec.DurationMs, rec.RiskScore, rec.Registered, rec.PolicyVersion,
	)
}

// PushFunc returns a func(AuditRecord) compatible with AuditRingBuffer.SetPushHook.
func (s *SQLiteAuditSink) PushFunc() func(AuditRecord) { return s.Push }

// MarkReviewed updates the HITL approval fields on a decision identified by requestUID.
// Called by the review API after an operator approves or denies a pending tool call.
func (s *SQLiteAuditSink) MarkReviewed(requestUID, status, reviewedBy string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`
		UPDATE decisions
		SET approval_status = ?, reviewed_by = ?, reviewed_at = ?
		WHERE request_uid = ?
	`, status, reviewedBy, time.Now(), requestUID)
	return err
}

// MarkSessionInactive sets active=0 when a session ends (eviction, budget exhaustion).
func (s *SQLiteAuditSink) MarkSessionInactive(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`UPDATE sessions SET active = 0 WHERE id = ?`, sessionID)
}

// QueryDecisions returns decisions matching f, newest-first, up to f.Limit.
func (s *SQLiteAuditSink) QueryDecisions(f AuditFilter) ([]AuditRecord, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	q := `SELECT request_uid, session_id, agent_id, ts, tool, decision,
	             policy_hit, reason, pipeline_ms, risk_score, registered, policy_version,
	             approval_status, reviewed_by, reviewed_at
	      FROM decisions WHERE 1=1`
	args := []any{}

	if f.Decision != "" {
		q += " AND decision = ?"
		args = append(args, f.Decision)
	}
	if f.ToolName != "" {
		q += " AND tool = ?"
		args = append(args, f.ToolName)
	}
	if f.SessionID != "" {
		q += " AND session_id = ?"
		args = append(args, f.SessionID)
	}
	q += " ORDER BY ts DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AuditRecord
	for rows.Next() {
		var rec AuditRecord
		var registered bool
		var approvalStatus, reviewedBy sql.NullString
		var reviewedAt sql.NullTime
		if err := rows.Scan(
			&rec.RequestUID, &rec.SessionID, &rec.AgentID, &rec.Timestamp,
			&rec.ToolName, &rec.Decision, &rec.PolicyHit, &rec.Reason,
			&rec.DurationMs, &rec.RiskScore, &registered, &rec.PolicyVersion,
			&approvalStatus, &reviewedBy, &reviewedAt,
		); err != nil {
			continue
		}
		rec.Registered = registered
		rec.ApprovalStatus = approvalStatus.String
		rec.ReviewedBy = reviewedBy.String
		if reviewedAt.Valid {
			rec.ReviewedAt = &reviewedAt.Time
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// QuerySessions returns all sessions, active-first.
func (s *SQLiteAuditSink) QuerySessions() ([]SQLiteSession, error) {
	rows, err := s.db.Query(`
		SELECT id, started_at, agent, risk_score, tokens_used, cost_usd, active
		FROM sessions
		ORDER BY active DESC, started_at DESC
		LIMIT 500
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SQLiteSession
	for rows.Next() {
		var ss SQLiteSession
		rows.Scan(&ss.ID, &ss.StartedAt, &ss.Agent, &ss.RiskScore,
			&ss.TokensUsed, &ss.CostUSD, &ss.Active)
		out = append(out, ss)
	}
	return out, rows.Err()
}

// Close closes the underlying database connection.
func (s *SQLiteAuditSink) Close() error { return s.db.Close() }

// SQLiteSession is a row from the sessions table.
type SQLiteSession struct {
	ID         string    `json:"id"`
	StartedAt  time.Time `json:"started_at"`
	Agent      string    `json:"agent"`
	RiskScore  float64   `json:"risk_score"`
	TokensUsed int64     `json:"tokens_used"`
	CostUSD    float64   `json:"cost_usd"`
	Active     bool      `json:"active"`
}

// DefaultDBPath returns the XDG-compliant default database location:
// ~/.local/share/perchguard/audit.db
// Falls back to ./snapshots/audit.db when the home directory is not available.
func DefaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "./snapshots/audit.db"
	}
	return filepath.Join(home, ".local", "share", "perchguard", "audit.db")
}
