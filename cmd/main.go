// PerchGuard - Agentic Admission Controller
//
// Modes:
//
//	server (default) — HTTP admission proxy for autonomous agents
//	mcp-proxy        — MCP JSON-RPC proxy (intercepts tools/call)
//	copilot          — MCP proxy pre-loaded with copilot-profile.yaml (cost control focus)
//	watch            — terminal fleet dashboard
//	meter            — terminal budget meter (per-session cost / token / call tracking)
//	claude-hook      — Claude Code PreToolUse/PostToolUse hook subprocess
//	init             — register Claude Code hooks + print setup status
//	compliance       — export a regime-shaped compliance report from the audit store
//
// Run: go run cmd/main.go [--mode=server|mcp-proxy|copilot|wrap|watch|meter|claude-hook|init|compliance]
// Env: PERCHGUARD_POLICY          ./configs/policies.yaml
//
//	PERCHGUARD_ADDR             :8080
//	PERCHGUARD_API_KEY          (Bearer key for /api/* endpoints; auto-generated and logged if unset)
//	PERCHGUARD_LLM_API_KEY      (Anthropic key, required if semanticFirewall.enabled)
//	PERCHGUARD_LLM_MODEL        claude-haiku-4-5
//	PERCHGUARD_MCP_UPSTREAM     (URL of upstream MCP server, required in mcp-proxy/copilot mode)
//	PERCHGUARD_MCP_TRANSPORT    stdio (default) | http
//	PERCHGUARD_AGENT_ID         mcp-proxy
//	PERCHGUARD_AGENT_ROLE       developer_agent
//	PERCHGUARD_GOVERNED_MODEL   (model Copilot is using; drives cost estimation in copilot/mcp-proxy mode)
//	PERCHGUARD_DASHBOARD_ADDR   :8081 (browser dashboard in copilot mode; set to "" to disable)
//	PERCHGUARD_TLS_CERT         (path to TLS cert, enables HTTPS)
//	PERCHGUARD_TLS_KEY          (path to TLS key)
//	PERCHGUARD_CONTEXT_ROOT     (path to project context root; enables context enrichment + local sink)
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/admission/mutator"
	"github.com/Droshow/PerchGuard/perchguard/pkg/admission/quota"
	"github.com/Droshow/PerchGuard/perchguard/pkg/admission/validator"
	"github.com/Droshow/PerchGuard/perchguard/pkg/agent"
	"github.com/Droshow/PerchGuard/perchguard/pkg/api"
	"github.com/Droshow/PerchGuard/perchguard/pkg/audit"
	"github.com/Droshow/PerchGuard/perchguard/pkg/envutil"
	"github.com/Droshow/PerchGuard/perchguard/pkg/humanreview"
	"github.com/Droshow/PerchGuard/perchguard/pkg/llm"
	"github.com/Droshow/PerchGuard/perchguard/pkg/localfs"
	"github.com/Droshow/PerchGuard/perchguard/pkg/manifest"
	"github.com/Droshow/PerchGuard/perchguard/pkg/mcp"
	"github.com/Droshow/PerchGuard/perchguard/pkg/pii"
	"github.com/Droshow/PerchGuard/perchguard/pkg/policy"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
	"github.com/Droshow/PerchGuard/perchguard/pkg/telemetry"
)

func main() {
	mode := flag.String("mode", "server", "Operating mode: server | mcp-proxy | copilot | wrap | watch | meter | claude-hook | init | compliance")
	mcpConfig := flag.String("mcp-config", "", "Path to .vscode/mcp.json (wrap mode). Auto-discovered if empty.")
	postHook := flag.Bool("post", false, "PostToolUse path for claude-hook mode")
	initProfile := flag.String("profile", "", "Policy profile name for init mode (e.g. open-banking)")
	uninstall := flag.Bool("uninstall", false, "Remove PerchGuard hooks (init mode)")

	// watch / meter mode flags
	watchAddr := flag.String("watch-addr", "", "PerchGuard server address to watch/meter (e.g. http://localhost:8080). Env: PERCHGUARD_WATCH_ADDR")
	watchKey := flag.String("watch-key", "", "API key for watch/meter mode. Env: PERCHGUARD_API_KEY")
	watchInterval := flag.Duration("watch-interval", 5*time.Second, "Poll interval for watch/meter mode")
	meterSession := flag.String("meter-session", "", "Session ID to display in meter mode. Empty = all active sessions")
	meterOnce := flag.Bool("once", false, "Print once and exit (meter mode; useful for shell/tmux status bars)")

	// dashboard flag — copilot mode starts a browser UI on this address
	dashboardAddr := flag.String("dashboard-addr", "", "Address for the browser dashboard (default :8081 in copilot mode). Env: PERCHGUARD_DASHBOARD_ADDR")

	// compliance mode flags
	complianceRegime := flag.String("regime", "eu-ai-act", "Compliance regime for --mode=compliance (e.g. eu-ai-act)")
	complianceDoc := flag.String("doc", "", "Compliance document to export for --mode=compliance (e.g. annex-iv, art26-coverage)")
	compliancePolicy := flag.String("policy", "", "Policy file path for --mode=compliance. Defaults to PERCHGUARD_POLICY or ./configs/policies.yaml")
	complianceDB := flag.String("db", "", "Audit SQLite db path for --mode=compliance. Defaults to the standard PerchGuard audit db location")
	complianceManifest := flag.String("manifest", "", "Optional AgentManifest path for --mode=compliance (seeds Annex IV §1)")
	complianceSince := flag.String("since", "", "RFC3339 lower bound for --mode=compliance (empty = unbounded)")
	complianceUntil := flag.String("until", "", "RFC3339 upper bound for --mode=compliance (empty = unbounded)")
	complianceFormat := flag.String("format", "markdown", "Output format for --mode=compliance: markdown | json")
	complianceOut := flag.String("out", "", "Output file for --mode=compliance (empty = stdout)")

	flag.Parse()

	// claude-hook and init are self-contained — return before loading policy/telemetry.
	if *mode == "claude-hook" {
		runClaudeHook(*postHook)
		return
	}
	if *mode == "init" {
		runInit(*uninstall, *initProfile)
		// init prints banner then falls through to start the server.
	}

	// compliance mode is self-contained — reads the policy/audit-db files directly and
	// exits before loading the admission pipeline (same shape as watch/meter).
	if *mode == "compliance" {
		policyPath := *compliancePolicy
		if policyPath == "" {
			policyPath = envutil.GetEnv("PERCHGUARD_POLICY", "./configs/policies.yaml")
		}
		dbPath := *complianceDB
		if dbPath == "" {
			dbPath = store.DefaultDBPath()
		}
		runCompliance(complianceConfig{
			regime:       *complianceRegime,
			doc:          *complianceDoc,
			policyPath:   policyPath,
			dbPath:       dbPath,
			manifestPath: *complianceManifest,
			since:        *complianceSince,
			until:        *complianceUntil,
			format:       *complianceFormat,
			out:          *complianceOut,
		})
		return
	}

	// copilot mode selects copilot-profile.yaml; policy discovery happens below.

	// wrap mode: auto-read .vscode/mcp.json, patch URLs, restore on exit.
	if *mode == "wrap" {
		restore := setupWrap(*mcpConfig)
		// Catch Ctrl-C / kill — restore mcp.json before the process dies.
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			restore()
			os.Exit(0)
		}()
		defer restore() // safety net for clean shutdowns
	}

	// meter mode is self-contained — exits before loading policy/telemetry
	if *mode == "meter" {
		addr := *watchAddr
		if addr == "" {
			addr = envutil.GetEnv("PERCHGUARD_WATCH_ADDR", "http://localhost:8080")
		}
		key := *watchKey
		if key == "" {
			key = os.Getenv("PERCHGUARD_API_KEY")
		}
		runMeter(meterConfig{
			addr:     addr,
			apiKey:   key,
			session:  *meterSession,
			interval: *watchInterval,
			once:     *meterOnce,
		})
		return
	}

	// watch mode is self-contained — exits before loading policy/telemetry
	if *mode == "watch" {
		addr := *watchAddr
		if addr == "" {
			addr = envutil.GetEnv("PERCHGUARD_WATCH_ADDR", "http://localhost:8080")
		}
		key := *watchKey
		if key == "" {
			key = os.Getenv("PERCHGUARD_API_KEY")
		}
		runWatch(watchConfig{
			addr:     addr,
			apiKey:   key,
			interval: *watchInterval,
		})
		return
	}

	policyFile := "policies.yaml"
	if *mode == "copilot" || *mode == "wrap" {
		policyFile = "copilot-profile.yaml"
	}
	policyPath := findPolicyPath(policyFile)
	if policyPath == "" {
		log.Fatalf("[perchguard] no policy file found — run: bash scripts/install.sh\n" +
			"  or set PERCHGUARD_POLICY=/path/to/policy.yaml")
	}
	log.Printf("[perchguard] loading policies from %s", policyPath)
	policyResult, err := policy.LoadWithMeta(policyPath)
	if err != nil {
		log.Fatalf("[perchguard] failed to load policy: %v", err)
	}
	cfg := policyResult.Config
	log.Printf("[perchguard] policy loaded (hash=%s)", policyResult.Hash)

	tp, err := telemetry.InitTracer("perchguard")
	if err != nil {
		log.Printf("[perchguard] telemetry init warning: %v", err)
	} else {
		defer tp.Shutdown(context.Background())
	}

	telemetry.Register()

	// Atomic policy metadata — declared here so the version closure below can capture it.
	// Updated by the watcher on hot-reload; read by GET /api/pipeline and audit stamping.
	var currentPolicy atomic.Pointer[policy.LoadResult]
	currentPolicy.Store(policyResult)

	// Shared session store — extracted to main() so the API server can query it.
	memStore := store.NewMemoryStore()

	// Audit ring buffer — live query surface for GET /api/audit and POST /api/query.
	auditRing := store.NewAuditRingBuffer(1000)
	telemetry.RegisterLiveMetrics(
		func() float64 { return float64(len(memStore.List())) },
		func() float64 { return float64(auditRing.Len()) },
	)
	auditLog := telemetry.NewTeeAuditLogger(
		telemetry.NewAuditLogger(cfg.Policies.Audit),
		auditRing,
		func() string { return currentPolicy.Load().Hash },
	)

	// LLM client — shared between the semantic firewall and POST /api/query.
	llmAPIKey := envutil.GetEnv("PERCHGUARD_LLM_API_KEY", os.Getenv("ANTHROPIC_API_KEY_USED_BY_PERCHGUARD"))
	var llmClient llm.Client
	if llmAPIKey != "" {
		model := envutil.GetEnv("PERCHGUARD_LLM_MODEL", cfg.Policies.SemanticFirewall.LLM.Model)
		if model == "" {
			model = "claude-haiku-4-5"
		}
		llmClient = llm.NewClaudeClient(llmAPIKey,
			llm.WithModel(model),
			llm.WithTimeout(time.Duration(cfg.Policies.SemanticFirewall.LLM.BudgetMs)*time.Millisecond),
		)
		log.Printf("[perchguard] LLM client wired (model=%s)", model)
	}

	// Shared PII/biometric matcher — redacts persisted intent baselines (see
	// EUAIACT-PERCHGUARD-SYNERGY.md §5). Built once; reused by the fleet manager.
	piiMatcher, err := pii.NewMatcher(cfg.Policies.PIIBiometric.Patterns, cfg.Policies.PIIBiometric.BiometricKeySuffixes, cfg.Policies.PIIBiometric.RedactReplacement)
	if err != nil {
		log.Fatalf("[perchguard] invalid piiBiometric patterns: %v", err)
	}

	// Fleet manager — not reconstructed on hot-reload; only policy-driven slices swap.
	fleetMgr := buildFleetManager(cfg, memStore, piiMatcher)

	// Delegation store — tracks parent→child session relationships for scope enforcement.
	delegationStore := agent.NewDelegationStore()

	// Lineage store — tracks per-session data provenance across the session lifetime.
	lineageStore := store.NewLineageStore()

	// Budget checker is created once and survives hot-reloads so session state is preserved.
	// On hot-reload only the policy limits swap via ReloadPolicy; existing sessions continue.
	budgetChecker := quota.NewSessionBudgetChecker(cfg.Policies.SessionBudget, memStore)

	// Build initial admission pipeline slices.
	validators, mutators, quotas, err := buildPipelineSlices(cfg, llmClient, fleetMgr, lineageStore, memStore, budgetChecker)
	if err != nil {
		log.Fatalf("[perchguard] failed to build admission pipeline: %v", err)
	}

	// Manifest store and pluggable context/audit wiring.
	manifestStore := manifest.NewStore()
	var contextProvider audit.ContextProvider = audit.NoOpContextProvider{}
	var auditSink audit.Sink

	fsReader := localfs.NewReader(envutil.GetEnv("PERCHGUARD_CONTEXT_ROOT", ""))
	if fsReader.Root() != "" {
		contextProvider = fsReader
		auditSink = localfs.NewWriter(fsReader.Root())
		log.Printf("[perchguard] local context active (root=%s)", fsReader.Root())
	} else {
		auditSink = audit.NewFileSink("./snapshots/governance.json")
		log.Printf("[perchguard] no context root found — governance records → ./snapshots/governance.json")
	}

	piiOutboundValidator, err := validator.NewPIIBiometricValidator(cfg.Policies.PIIBiometric)
	if err != nil {
		log.Fatalf("[perchguard] failed to build pii_biometric validator: %v", err)
	}

	// Human review dispatcher.
	opts := []admission.InterceptorOption{
		admission.WithOutboundValidators(
			validator.NewOutputValidator(cfg.Policies.OutputValidation),
			piiOutboundValidator,
		),
		admission.WithTokenVerifier(manifestStore),
		admission.WithManifestLookup(func(sessionID string) *admission.MissionContext {
			reg, ok := manifestStore.GetBySession(sessionID)
			if !ok {
				return nil
			}
			return &admission.MissionContext{
				Summary:    reg.Manifest.Mission.Summary,
				Scope:      reg.Manifest.Mission.Scope,
				OutOfScope: reg.Manifest.Mission.OutOfScope,
			}
		}),
	}
	if cfg.Policies.HumanReview.Enabled && cfg.Policies.HumanReview.WebhookURL != "" {
		timeoutSecs := cfg.Policies.HumanReview.TimeoutSeconds
		if timeoutSecs <= 0 {
			timeoutSecs = 60
		}
		dispatcher := humanreview.NewDispatcher(cfg.Policies.HumanReview.WebhookURL, timeoutSecs)
		opts = append(opts, admission.WithReviewDispatcher(dispatcher))
		log.Printf("[perchguard] human review webhook configured (%s, timeout=%ds)",
			cfg.Policies.HumanReview.WebhookURL, timeoutSecs)
	}

	opts = append(opts, admission.WithObserveMode(cfg.IsObserveMode()))
	interceptor := admission.NewInterceptor(validators, mutators, quotas, auditLog, opts...)

	if cfg.IsObserveMode() {
		log.Printf("[perchguard] enforcement mode: OBSERVE — decisions logged but not enforced")
	} else {
		log.Printf("[perchguard] enforcement mode: ENFORCE")
	}

	// Policy watcher — polls every 30 seconds, hot-reloads on hash change.
	watcher := policy.NewWatcher(policyPath, 30*time.Second, func(result *policy.LoadResult) {
		budgetChecker.ReloadPolicy(result.Config.Policies.SessionBudget)
		newValidators, newMutators, newQuotas, err := buildPipelineSlices(result.Config, llmClient, fleetMgr, lineageStore, memStore, budgetChecker)
		if err != nil {
			log.Printf("[perchguard] policy hot-reload failed (keeping previous pipeline): %v", err)
			return
		}
		interceptor.ReloadValidators(newValidators, newMutators, newQuotas)
		interceptor.SetObserveMode(result.Config.IsObserveMode())
		currentPolicy.Store(result)
		telemetry.PolicyReloadTotal.Inc()
		log.Printf("[perchguard] policy hot-reloaded: hash %s (enforcement: %s)", result.Hash, result.Config.Enforcement)
	})
	watcher.Start(policyResult.Hash)
	defer watcher.Stop()

	// Management API key — read from env or auto-generate a pgmk-... key at startup.
	apiKey := os.Getenv("PERCHGUARD_API_KEY")
	if apiKey == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			log.Fatalf("[perchguard] failed to generate API key: %v", err)
		}
		apiKey = "pgmk-" + hex.EncodeToString(b)
		log.Printf("[perchguard] management API key: %s", apiKey)
	}
	keyStore := api.NewKeyStore(apiKey, "default")

	// Production TLS warning — loud and deliberate.
	if os.Getenv("PERCHGUARD_ENV") == "production" {
		certFile := os.Getenv("PERCHGUARD_TLS_CERT")
		keyFile := os.Getenv("PERCHGUARD_TLS_KEY")
		if certFile == "" || keyFile == "" {
			log.Printf("WARNING: PERCHGUARD_ENV=production but TLS is not configured.")
			log.Printf("WARNING: Management API and admission endpoints are unencrypted.")
			log.Printf("WARNING: Set PERCHGUARD_TLS_CERT and PERCHGUARD_TLS_KEY before serving production traffic.")
		}
	}

	// JSONL forensic sink — per-call AuditRecord appended at emit time, survives ring rollover.
	jsonlSink, sinkErr := audit.NewJSONLRecordSink("./snapshots/audit.jsonl")
	if sinkErr != nil {
		log.Printf("[perchguard] JSONL audit sink unavailable: %v", sinkErr)
	} else {
		auditRing.SetPushHook(jsonlSink.Push)
		log.Printf("[perchguard] JSONL audit sink active (./snapshots/audit.jsonl)")
	}

	// Loki log shipping — optional, non-blocking, fails silently if Loki is not running.
	if lokiEndpoint := envutil.GetEnv("PERCHGUARD_LOKI_ENDPOINT", ""); lokiEndpoint != "" {
		lokiSink := store.NewLokiSink(lokiEndpoint)
		existing := auditRing.PushHook()
		auditRing.SetPushHook(func(rec store.AuditRecord) {
			if existing != nil {
				existing(rec)
			}
			lokiSink.Push(rec)
		})
		log.Printf("[perchguard] Loki shipping active (%s)", lokiEndpoint)
	}

	// SQLite durable audit store — persists all decisions across restarts.
	// The hook's in-process mode writes to the same file; server picks them up on start.
	var sqliteSink *store.SQLiteAuditSink
	dbPath := envutil.GetEnv("PERCHGUARD_DB_PATH", store.DefaultDBPath())
	if s, err := store.NewSQLiteAuditSink(dbPath); err != nil {
		log.Printf("[perchguard] SQLite audit store unavailable: %v", err)
	} else {
		sqliteSink = s
		existing := auditRing.PushHook()
		auditRing.SetPushHook(func(rec store.AuditRecord) {
			if existing != nil {
				existing(rec)
			}
			sqliteSink.Push(rec)
		})
		log.Printf("[perchguard] SQLite audit store active (%s)", dbPath)
	}

	// Management API server.
	apiServer := api.NewAPIServer(memStore, auditRing, interceptor, fleetMgr, llmClient, &currentPolicy,
		manifestStore, contextProvider, auditSink, keyStore, delegationStore, budgetChecker, sqliteSink)

	// Session reaper — evicts idle/overdue sessions and emits governance records for them.
	idleLimit := time.Duration(cfg.Policies.SessionBudget.Limits.IdleTimeoutMinutes) * time.Minute
	maxAge := time.Duration(cfg.Policies.SessionBudget.Limits.MaxDurationMinutes) * time.Minute
	if idleLimit > 0 || maxAge > 0 {
		reaper := store.NewReaper(memStore, idleLimit, maxAge, func(ss *store.SessionState, terminatedEarly bool) {
			if ss.ManifestID != "" {
				apiServer.EmitGovernanceRecord(ss, terminatedEarly)
			}
			if fleetMgr != nil {
				fleetMgr.Evict(ss.SessionID)
			}
			lineageStore.Evict(ss.SessionID)
			delegationStore.Evict(ss.SessionID)
		})
		reaper.Start()
		defer reaper.Stop()
		log.Printf("[perchguard] session reaper active (idle=%v maxAge=%v)", idleLimit, maxAge)
	}

	switch *mode {

	// ── COPILOT / LOCAL IDE MODE ─────────────────────────────────────────────
	// mcp-proxy and copilot run as a stdio MCP proxy — no HTTP listener.
	// copilot additionally auto-selects copilot-profile.yaml and opens the
	// browser dashboard. Neither mode uses the autonomous-agent fleet or
	// the full /api/* management surface.
	case "mcp-proxy", "copilot":
		governedModel := envutil.GetEnv("PERCHGUARD_GOVERNED_MODEL", "")

		// Start browser dashboard alongside the MCP proxy.
		dashAddr := *dashboardAddr
		if dashAddr == "" {
			dashAddr = envutil.GetEnv("PERCHGUARD_DASHBOARD_ADDR", "")
		}
		if dashAddr == "" && *mode == "copilot" {
			dashAddr = ":8081"
		}
		if dashAddr != "" {
			dashMux := http.NewServeMux()
			apiServer.RegisterDashboardRoutes(dashMux)
			go func() {
				log.Printf("[perchguard] copilot dashboard → http://localhost%s", dashAddr)
				if err := http.ListenAndServe(dashAddr, dashMux); err != nil {
					log.Printf("[perchguard] dashboard server stopped: %v", err)
				}
			}()
		}

		runMCPProxy(interceptor, governedModel)
	// ── AUTONOMOUS AGENT / SERVER MODE ──────────────────────────────────────
	// Full HTTP server with admission endpoints, management API, Prometheus
	// metrics, optional MCP proxy at /mcp, and policy hot-reload.
	default: // "server" and "wrap" both run the HTTP server
		runServer(interceptor, apiServer)
	}
}

// buildFleetManager creates the Phase 3 session-aware fleet validator.
// Returns nil when agentFleet is disabled in policy.
func buildFleetManager(cfg *policy.Config, st store.SessionStore, piiMatcher *pii.Matcher) *agent.FleetManager {
	if !cfg.Policies.AgentFleet.Enabled {
		return nil
	}
	var behaviorChains [][]agent.Stage
	for _, p := range cfg.Policies.AgentFleet.BehaviorPatterns {
		chain := make([]agent.Stage, len(p.Sequence))
		for i, s := range p.Sequence {
			chain[i] = agent.Stage(s)
		}
		behaviorChains = append(behaviorChains, chain)
	}
	fleetCfg := agent.AgentFleetConfig{
		DriftThreshold:     cfg.Policies.AgentFleet.DriftThreshold,
		BehaviorWindowSize: cfg.Policies.AgentFleet.BehaviorWindowSize,
		AttackChainEnabled: cfg.Policies.AgentFleet.AttackChainEnabled,
		BehaviorChains:     behaviorChains,
		ToolStageMap:       cfg.Policies.AgentFleet.ToolStageMap,
		VelocityAnomaly: agent.VelocityAnomalyConfig{
			WindowMinutes:    cfg.Policies.SessionBudget.Anomaly.VelocityWindowMinutes,
			ThresholdCalls:   cfg.Policies.SessionBudget.Anomaly.VelocityThresholdCalls,
			MaxAvgDrift:      cfg.Policies.SessionBudget.Anomaly.VelocityMaxAvgDrift,
			RiskContribution: cfg.Policies.SessionBudget.Anomaly.VelocityRiskContribution,
		},
		IntentRedactor: piiMatcher,
	}
	fleet := agent.NewFleetManager(fleetCfg, st)
	log.Printf("[perchguard] agent fleet enabled (driftThreshold=%.2f attackChain=%v chains=%d)",
		fleetCfg.DriftThreshold, fleetCfg.AttackChainEnabled, len(behaviorChains))
	return fleet
}

// buildPipelineSlices builds the policy-driven validator/mutator/quota slices.
// Called once at startup and again by the policy watcher on hot-reload.
// budgetChecker is passed in (not created here) so session state survives hot-reloads.
func buildPipelineSlices(cfg *policy.Config, llmClient llm.Client, fleetMgr *agent.FleetManager, lineageStore *store.LineageStore, sessionStore store.SessionStore, budgetChecker *quota.SessionBudgetChecker) (
	[]admission.Validator, []admission.Mutator, []admission.QuotaChecker, error,
) {
	piiValidator, err := validator.NewPIIBiometricValidator(cfg.Policies.PIIBiometric)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("pii_biometric validator: %w", err)
	}

	validators := []admission.Validator{
		validator.NewPromptInjectionValidator(cfg.Policies.PromptInjection),
		piiValidator,
		validator.NewToolAuthorizationValidator(cfg.Policies.ToolAuthorization),
		validator.NewDataExfiltrationValidator(cfg.Policies.DataExfiltration),
		validator.NewLineageValidator(lineageStore),
	}

	if cfg.Policies.SemanticFirewall.Enabled && llmClient != nil {
		validators = append(validators, validator.NewSemanticFirewallValidator(cfg.Policies.SemanticFirewall, llmClient))
		log.Printf("[perchguard] semantic firewall enabled (budget=%dms)", cfg.Policies.SemanticFirewall.LLM.BudgetMs)
	}

	if fleetMgr != nil {
		validators = append(validators, fleetMgr)
	}

	mutators := []admission.Mutator{
		mutator.NewParameterSanitizer(cfg.Policies.ParameterSanitization),
		mutator.NewLeastPrivilegeMutator(cfg.Policies.LeastPrivilege),
	}

	quotas := []admission.QuotaChecker{
		quota.NewDepthLimiter(cfg.Policies.DepthLimiter),
		budgetChecker,
	}

	return validators, mutators, quotas, nil
}

func runServer(interceptor *admission.Interceptor, apiServer *api.APIServer) {
	addr := envutil.GetEnv("PERCHGUARD_ADDR", ":8080")
	certFile := os.Getenv("PERCHGUARD_TLS_CERT")
	keyFile := os.Getenv("PERCHGUARD_TLS_KEY")

	mux := http.NewServeMux()
	mux.Handle("/intercept", interceptor)
	mux.Handle("/validate/output", interceptor)
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","service":"perchguard"}`))
	})
	apiServer.RegisterRoutes(mux)

	// If PERCHGUARD_MCP_UPSTREAM is set, also expose a transparent HTTP MCP proxy at /mcp.
	if upstreamURL := os.Getenv("PERCHGUARD_MCP_UPSTREAM"); upstreamURL != "" {
		httpProxy := mcp.NewProxy(interceptor, mcp.NewHTTPTransport(upstreamURL), nil,
			mcp.WithAgentRole(envutil.GetEnv("PERCHGUARD_AGENT_ROLE", "developer_agent")),
			mcp.WithAgentID(envutil.GetEnv("PERCHGUARD_AGENT_ID", "mcp-proxy")),
		)
		mux.Handle("/mcp", httpProxy)
		log.Printf("[perchguard] HTTP MCP proxy active at /mcp (upstream=%s role=%s)",
			upstreamURL, envutil.GetEnv("PERCHGUARD_AGENT_ROLE", "developer_agent"))
	}

	log.Printf("[perchguard] admission controller listening on %s", addr)
	log.Printf("[perchguard] management API available at %s/api/", addr)
	if certFile != "" && keyFile != "" {
		log.Printf("[perchguard] TLS enabled")
		if err := http.ListenAndServeTLS(addr, certFile, keyFile, mux); err != nil {
			log.Fatalf("[perchguard] TLS server error: %v", err)
		}
	} else {
		log.Printf("[perchguard] WARNING: TLS disabled — local dev only")
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatalf("[perchguard] server error: %v", err)
		}
	}
}

func runMCPProxy(interceptor *admission.Interceptor, governedModel string) {
	// Subprocess-mode: PERCHGUARD_UPSTREAM_CMD is set when wrapping a stdio server.
	// HTTP-mode: PERCHGUARD_MCP_UPSTREAM is set for HTTP-based upstream servers.
	var upstream mcp.UpstreamTransport
	if cmd := os.Getenv("PERCHGUARD_UPSTREAM_CMD"); cmd != "" {
		var args []string
		if raw := os.Getenv("PERCHGUARD_UPSTREAM_ARGS"); raw != "" {
			args = splitArgs(raw)
		}
		sub := mcp.NewSubprocessTransport(cmd, args, nil)
		if err := sub.Start(); err != nil {
			log.Fatalf("[perchguard] failed to start upstream subprocess: %v", err)
		}
		defer sub.Close()
		upstream = sub
		log.Printf("[perchguard] mcp proxy started (upstream=subprocess cmd=%s)", cmd)
	} else {
		upstreamURL := os.Getenv("PERCHGUARD_MCP_UPSTREAM")
		if upstreamURL == "" {
			log.Fatal("[perchguard] --mode=mcp-proxy requires PERCHGUARD_MCP_UPSTREAM (HTTP) or PERCHGUARD_UPSTREAM_CMD (stdio subprocess)")
		}
		upstream = mcp.NewHTTPTransport(upstreamURL)
		log.Printf("[perchguard] mcp proxy started (upstream=%s)", upstreamURL)
	}

	downstream := mcp.NewStdioTransport(os.Stdin, os.Stdout)

	proxyOpts := []mcp.Option{
		mcp.WithSessionID(fmt.Sprintf("pg-%d", time.Now().UnixNano())),
		mcp.WithAgentID(envutil.GetEnv("PERCHGUARD_AGENT_ID", "mcp-proxy")),
		mcp.WithAgentRole(envutil.GetEnv("PERCHGUARD_AGENT_ROLE", "developer_agent")),
	}
	if governedModel != "" {
		proxyOpts = append(proxyOpts, mcp.WithGovernedModel(governedModel))
	}
	proxy := mcp.NewProxy(interceptor, upstream, downstream, proxyOpts...)

	if err := proxy.Run(context.Background()); err != nil {
		log.Fatalf("[perchguard] mcp proxy error: %v", err)
	}
}

// splitArgs parses PERCHGUARD_UPSTREAM_ARGS into a []string.
// The wrap command encodes the original args as a JSON array, e.g. ["-y","server","/path"].
// Legacy comma-separated form is also accepted for manual configurations.
func splitArgs(s string) []string {
	if s == "" {
		return nil
	}
	if strings.HasPrefix(s, "[") {
		var args []string
		if err := json.Unmarshal([]byte(s), &args); err == nil {
			return args
		}
	}
	// Fallback: comma-separated (simple configs without special characters).
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}
