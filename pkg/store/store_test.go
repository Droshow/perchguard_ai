package store_test

import (
	"sync"
	"testing"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

// ── MemoryStore ───────────────────────────────────────────────────────────────

func TestMemoryStore_SetGetDelete(t *testing.T) {
	ms := store.NewMemoryStore()

	_, ok := ms.Get("missing")
	if ok {
		t.Fatal("expected miss for unknown session")
	}

	ss := &store.SessionState{SessionID: "s1", RiskScore: 0.3}
	ms.Set("s1", ss)

	got, ok := ms.Get("s1")
	if !ok || got.SessionID != "s1" {
		t.Fatal("expected hit after Set")
	}

	ms.Delete("s1")
	_, ok = ms.Get("s1")
	if ok {
		t.Fatal("expected miss after Delete")
	}
}

func TestMemoryStore_List(t *testing.T) {
	ms := store.NewMemoryStore()
	ms.Set("a", &store.SessionState{SessionID: "a"})
	ms.Set("b", &store.SessionState{SessionID: "b"})

	all := ms.List()
	if len(all) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(all))
	}
}

func TestMemoryStore_ConcurrentAccess(t *testing.T) {
	ms := store.NewMemoryStore()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "s" + string(rune('0'+i%10))
			ms.Set(id, &store.SessionState{SessionID: id})
			ms.Get(id)
			ms.List()
		}(i)
	}
	wg.Wait()
}

// ── AuditRingBuffer ───────────────────────────────────────────────────────────

func makeRecord(decision, tool, session string) store.AuditRecord {
	return store.AuditRecord{
		Decision:  decision,
		ToolName:  tool,
		SessionID: session,
		Timestamp: time.Now(),
	}
}

func TestAuditRingBuffer_PushQuery(t *testing.T) {
	rb := store.NewAuditRingBuffer(10)

	rb.Push(makeRecord("ALLOW", "read_file", "s1"))
	rb.Push(makeRecord("DENY", "exec_cmd", "s1"))

	all := rb.Query(store.AuditFilter{Limit: 10})
	if len(all) != 2 {
		t.Fatalf("want 2 records, got %d", len(all))
	}
	// Query returns newest-first.
	if all[0].Decision != "DENY" {
		t.Errorf("want newest DENY first, got %s", all[0].Decision)
	}
}

func TestAuditRingBuffer_FilterByDecision(t *testing.T) {
	rb := store.NewAuditRingBuffer(20)
	rb.Push(makeRecord("ALLOW", "t1", "s1"))
	rb.Push(makeRecord("DENY", "t2", "s1"))
	rb.Push(makeRecord("DENY", "t3", "s2"))

	denies := rb.Query(store.AuditFilter{Decision: "DENY", Limit: 10})
	if len(denies) != 2 {
		t.Fatalf("want 2 DENY records, got %d", len(denies))
	}
}

func TestAuditRingBuffer_FilterBySession(t *testing.T) {
	rb := store.NewAuditRingBuffer(20)
	rb.Push(makeRecord("ALLOW", "t1", "s1"))
	rb.Push(makeRecord("DENY", "t2", "s2"))

	results := rb.Query(store.AuditFilter{SessionID: "s1", Limit: 10})
	if len(results) != 1 || results[0].SessionID != "s1" {
		t.Fatalf("want 1 record for s1, got %d", len(results))
	}
}

func TestAuditRingBuffer_FilterByTool(t *testing.T) {
	rb := store.NewAuditRingBuffer(20)
	rb.Push(makeRecord("ALLOW", "exec_cmd", "s1"))
	rb.Push(makeRecord("DENY", "read_file", "s1"))

	results := rb.Query(store.AuditFilter{ToolName: "exec_cmd", Limit: 10})
	if len(results) != 1 || results[0].ToolName != "exec_cmd" {
		t.Fatalf("want 1 exec_cmd record, got %d", len(results))
	}
}

func TestAuditRingBuffer_RolloverOverwrites(t *testing.T) {
	rb := store.NewAuditRingBuffer(3)
	for i := 0; i < 6; i++ {
		rb.Push(makeRecord("ALLOW", "t", "s"))
	}
	// Buffer capacity is 3 — never more than 3 records returned.
	all := rb.Query(store.AuditFilter{Limit: 100})
	if len(all) != 3 {
		t.Fatalf("want 3 after rollover, got %d", len(all))
	}
}

func TestAuditRingBuffer_DefaultLimit(t *testing.T) {
	rb := store.NewAuditRingBuffer(200)
	for i := 0; i < 150; i++ {
		rb.Push(makeRecord("ALLOW", "t", "s"))
	}
	// Zero limit should default to 100.
	all := rb.Query(store.AuditFilter{})
	if len(all) != 100 {
		t.Fatalf("want 100 (default limit), got %d", len(all))
	}
}

func TestAuditRingBuffer_PushHook(t *testing.T) {
	rb := store.NewAuditRingBuffer(10)
	var mu sync.Mutex
	var hooked []string
	rb.SetPushHook(func(rec store.AuditRecord) {
		mu.Lock()
		hooked = append(hooked, rec.Decision)
		mu.Unlock()
	})

	rb.Push(makeRecord("ALLOW", "t", "s"))
	rb.Push(makeRecord("DENY", "t", "s"))

	mu.Lock()
	n := len(hooked)
	mu.Unlock()
	if n != 2 {
		t.Fatalf("hook called %d times, want 2", n)
	}
}

// ── Reaper ────────────────────────────────────────────────────────────────────

func TestReaper_IdleTimeout(t *testing.T) {
	ms := store.NewMemoryStore()
	now := time.Now()
	ms.Set("old", &store.SessionState{
		SessionID: "old",
		CreatedAt: now.Add(-30 * time.Minute),
		UpdatedAt: now.Add(-20 * time.Minute), // idle 20m
	})
	ms.Set("fresh", &store.SessionState{
		SessionID: "fresh",
		CreatedAt: now.Add(-5 * time.Minute),
		UpdatedAt: now.Add(-1 * time.Minute), // idle 1m
	})

	var evicted []string
	reaper := store.NewReaper(ms, 15*time.Minute, 0, func(ss *store.SessionState, _ bool) {
		evicted = append(evicted, ss.SessionID)
	})
	reaper.SweepForTest()

	if len(evicted) != 1 || evicted[0] != "old" {
		t.Errorf("want only 'old' evicted, got %v", evicted)
	}
	if _, ok := ms.Get("old"); ok {
		t.Error("'old' should be deleted from store after eviction")
	}
	if _, ok := ms.Get("fresh"); !ok {
		t.Error("'fresh' should survive eviction")
	}
}

func TestReaper_MaxAge(t *testing.T) {
	ms := store.NewMemoryStore()
	now := time.Now()
	ms.Set("aged", &store.SessionState{
		SessionID: "aged",
		CreatedAt: now.Add(-2 * time.Hour),
		UpdatedAt: now.Add(-1 * time.Minute), // recently active
	})

	var evicted []string
	reaper := store.NewReaper(ms, 0, 1*time.Hour, func(ss *store.SessionState, _ bool) {
		evicted = append(evicted, ss.SessionID)
	})
	reaper.SweepForTest()

	if len(evicted) != 1 || evicted[0] != "aged" {
		t.Errorf("want 'aged' evicted by maxAge, got %v", evicted)
	}
}

func TestReaper_TerminatedEarlyFlag(t *testing.T) {
	ms := store.NewMemoryStore()
	now := time.Now()
	ms.Set("s1", &store.SessionState{
		SessionID: "s1",
		CreatedAt: now.Add(-30 * time.Minute),
		UpdatedAt: now.Add(-20 * time.Minute),
	})

	var flag bool
	reaper := store.NewReaper(ms, 15*time.Minute, 0, func(_ *store.SessionState, terminatedEarly bool) {
		flag = terminatedEarly
	})
	reaper.SweepForTest()

	if !flag {
		t.Error("onExpire should be called with terminatedEarly=true")
	}
}

// ── LineageStore ──────────────────────────────────────────────────────────────

func TestLineageRef_Deterministic(t *testing.T) {
	r1 := store.LineageRef("session-1", "uid-abc")
	r2 := store.LineageRef("session-1", "uid-abc")
	if r1 != r2 {
		t.Errorf("LineageRef must be deterministic: got %q then %q", r1, r2)
	}
	if len(r1) != 12 {
		t.Errorf("LineageRef should be 12 hex chars, got %d", len(r1))
	}
}

func TestLineageRef_Unique(t *testing.T) {
	// Same session, different UIDs must produce different refs.
	r1 := store.LineageRef("s1", "uid-1")
	r2 := store.LineageRef("s1", "uid-2")
	if r1 == r2 {
		t.Error("different UIDs in the same session must produce different refs")
	}
	// Same UID in different sessions must produce different refs.
	r3 := store.LineageRef("s2", "uid-1")
	if r1 == r3 {
		t.Error("same UID in different sessions must produce different refs")
	}
}

func TestLineageStore_GraphNilForUnknown(t *testing.T) {
	ls := store.NewLineageStore()
	if g := ls.Graph("no-such-session"); g != nil {
		t.Error("expected nil graph for unknown session")
	}
}

func TestLineageStore_AddEdgeAndHasAny(t *testing.T) {
	ls := store.NewLineageStore()

	ref1 := store.LineageRef("s1", "uid-1")
	ls.AddEdge("s1", ref1, nil, "read_file")

	g := ls.Graph("s1")
	if g == nil {
		t.Fatal("expected graph to exist after AddEdge")
	}
	if !g.HasAny([]string{ref1}) {
		t.Error("HasAny should return true for a ref that was added via AddEdge")
	}
	if g.HasAny([]string{"unknown-ref"}) {
		t.Error("HasAny should return false for a ref not in the graph")
	}
}

func TestLineageStore_Evict(t *testing.T) {
	ls := store.NewLineageStore()
	ref := store.LineageRef("s1", "uid-1")
	ls.AddEdge("s1", ref, nil, "read_file")

	ls.Evict("s1")
	if g := ls.Graph("s1"); g != nil {
		t.Error("graph should be nil after Evict")
	}
}

func TestLineageStore_ConcurrentAccess(t *testing.T) {
	ls := store.NewLineageStore()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sid := "sess-" + string(rune('A'+i%5))
			uid := "uid-" + string(rune('0'+i%10))
			ref := store.LineageRef(sid, uid)
			ls.AddEdge(sid, ref, nil, "tool")
			if g := ls.Graph(sid); g != nil {
				g.HasAny([]string{ref})
			}
		}(i)
	}
	wg.Wait()
}

// ── SessionStore.Children ─────────────────────────────────────────────────────

func TestMemoryStore_Children(t *testing.T) {
	ms := store.NewMemoryStore()
	ms.Set("parent", &store.SessionState{SessionID: "parent"})
	ms.Set("child-1", &store.SessionState{SessionID: "child-1", ParentSessionID: "parent"})
	ms.Set("child-2", &store.SessionState{SessionID: "child-2", ParentSessionID: "parent"})
	ms.Set("other", &store.SessionState{SessionID: "other", ParentSessionID: "other-parent"})

	children := ms.Children("parent")
	if len(children) != 2 {
		t.Fatalf("want 2 children of 'parent', got %d: %v", len(children), children)
	}
	childSet := make(map[string]bool)
	for _, c := range children {
		childSet[c] = true
	}
	if !childSet["child-1"] || !childSet["child-2"] {
		t.Errorf("expected child-1 and child-2, got %v", children)
	}
}

func TestMemoryStore_Children_NoneFound(t *testing.T) {
	ms := store.NewMemoryStore()
	ms.Set("solo", &store.SessionState{SessionID: "solo"})
	if kids := ms.Children("solo"); len(kids) != 0 {
		t.Errorf("want no children for root session, got %v", kids)
	}
}
