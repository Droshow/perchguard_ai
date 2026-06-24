// Package policy handles loading and validating PerchGuard's YAML policy configuration.
// Mirrors the YAML policy loading pattern from EKS-BankingKube/Dynamic_Pod_Sec.
package policy

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

// Config is the top-level structure parsed from configs/policies.yaml
type Config struct {
	// Enforcement controls whether the pipeline blocks calls or only observes them.
	// "observe" — full pipeline runs but every decision is overridden to ALLOW;
	//             the original decision is preserved in the audit record.
	// "enforce" — normal blocking behaviour (default).
	Enforcement string         `yaml:"enforcement"`
	Admission   AdmissionPolicy `yaml:"admission"`
	Policies    Policies       `yaml:"policies"`
}

// AdmissionPolicy holds the top-level admission thresholds and dual-approval
// configuration that sit above the per-validator policies block (see
// configs/profiles/open-banking.yaml's `admission:` section).
type AdmissionPolicy struct {
	ReviewAbove       float64  `yaml:"reviewAbove"`
	BlockAbove        float64  `yaml:"blockAbove"`
	ReviewTimeoutSecs int      `yaml:"reviewTimeoutSecs"`
	DualApprovalRoles []string `yaml:"dualApprovalRoles"`
}

type Policies struct {
	PromptInjection      PromptInjectionPolicy      `yaml:"promptInjection"`
	SemanticFirewall     SemanticFirewallPolicy     `yaml:"semanticFirewall"`
	ToolAuthorization    ToolAuthorizationPolicy    `yaml:"toolAuthorization"`
	DataExfiltration     DataExfiltrationPolicy     `yaml:"dataExfiltration"`
	ParameterSanitization ParameterSanitizationPolicy `yaml:"parameterSanitization"`
	LeastPrivilege       LeastPrivilegePolicy       `yaml:"leastPrivilege"`
	SessionBudget        SessionBudgetPolicy        `yaml:"sessionBudget"`
	DepthLimiter         DepthLimiterPolicy         `yaml:"depthLimiter"`
	OutputValidation     OutputValidationPolicy     `yaml:"outputValidation"`
	PIIBiometric         PIIBiometricPolicy         `yaml:"piiBiometric"`
	HumanReview          HumanReviewPolicy          `yaml:"humanReview"`
	Audit                AuditPolicy                `yaml:"audit"`
	AgentFleet           AgentFleetPolicy           `yaml:"agentFleet"`
}

// AgentFleetPolicy configures the Phase 3 session-aware agent fleet validator.
type AgentFleetPolicy struct {
	// Enabled gates the entire fleet — set false to run Phase 1/2 only.
	Enabled bool `yaml:"enabled"`

	// DriftThreshold is the cosine drift score above which a tool call contributes
	// to session risk. Range 0.0–1.0. Recommended starting value: 0.4.
	DriftThreshold float64 `yaml:"driftThreshold"`

	// BehaviorWindowSize is the number of recent tool events the attack chain
	// detector scans. Default: 10.
	BehaviorWindowSize int `yaml:"behaviorWindowSize"`

	// AttackChainEnabled toggles the multi-step attack sequence detector.
	AttackChainEnabled bool `yaml:"attackChainEnabled"`

	// BehaviorPatterns defines policy-configurable attack chains to detect.
	// Each sequence is an ordered list of stage names: recon, exploit, escalate, exfiltrate.
	// When empty, the built-in default chains are used.
	BehaviorPatterns []BehaviorPattern `yaml:"behaviorPatterns"`

	// ToolStageMap overrides the built-in tool→stage classification.
	// Keys are stage names (recon, exploit, escalate, exfiltrate).
	// Values are lists of tool name substrings (case-insensitive prefix/contains match).
	// When empty, the built-in defaults in ClassifyTool are used.
	ToolStageMap map[string][]string `yaml:"toolStageMap"`
}

// BehaviorPattern is a named attack-chain sequence loaded from policy config.
type BehaviorPattern struct {
	Name     string   `yaml:"name"`
	Sequence []string `yaml:"sequence"` // ordered stage names: recon | exploit | escalate | exfiltrate
}

// --- Validation Layer ---

type PromptInjectionPolicy struct {
	Enabled            bool     `yaml:"enabled"`
	SuspiciousPatterns []string `yaml:"suspiciousPatterns"`
	ScanFields         []string `yaml:"scanFields"`
	Action             string   `yaml:"action"`
}

// LLMConfig holds the LLM budget and model settings for the SemanticFirewall.
type LLMConfig struct {
	Model          string  `yaml:"model"`
	MaxTokens      int     `yaml:"maxTokens"`
	Temperature    float64 `yaml:"temperature"`
	BudgetMs       int     `yaml:"budgetMs"`
	SkipIfNoIntent bool    `yaml:"skipIfNoIntent"`
}

type SemanticFirewallPolicy struct {
	Enabled                  bool      `yaml:"enabled"`
	IntentAlignmentThreshold float64   `yaml:"intentAlignmentThreshold"`
	Action                   string    `yaml:"action"`
	LLM                      LLMConfig `yaml:"llm"`
}

type ToolAuthorizationPolicy struct {
	Enabled    bool        `yaml:"enabled"`
	AgentRoles []AgentRole `yaml:"agentRoles"`
	Action     string      `yaml:"action"`
}

type AgentRole struct {
	Role         string   `yaml:"role"`
	AllowedTools []string `yaml:"allowedTools"`
	DeniedTools  []string `yaml:"deniedTools"`
}

type DataExfiltrationPolicy struct {
	Enabled                  bool     `yaml:"enabled"`
	AllowedDestinations      []string `yaml:"allowedDestinations"`
	BlockedDestinations      []string `yaml:"blockedDestinations"`
	BlockUnknownDestinations bool     `yaml:"blockUnknownDestinations"`
	Action                   string   `yaml:"action"`
}

// --- Mutation Layer ---

type ParameterSanitizationPolicy struct {
	Enabled bool               `yaml:"enabled"`
	Rules   []SanitizationRule `yaml:"rules"`
}

type SanitizationRule struct {
	Match          string   `yaml:"match"`
	ContainsAny    []string `yaml:"containsAny"`
	Inject         string   `yaml:"inject"`
	Unless         string   `yaml:"unless"`
	StripFlags     []string `yaml:"stripFlags"`
	EnforceReadOnly bool    `yaml:"enforceReadOnly"`
}

type LeastPrivilegePolicy struct {
	Enabled        bool           `yaml:"enabled"`
	SQLInjection   SQLPrivPolicy  `yaml:"sqlInjection"`
	FileOperations FilePrivPolicy `yaml:"fileOperations"`
}

type SQLPrivPolicy struct {
	AutoAppendWhereClause bool   `yaml:"autoAppendWhereClause"`
	UserContextField      string `yaml:"userContextField"`
}

type FilePrivPolicy struct {
	AllowedBasePaths    []string `yaml:"allowedBasePaths"`
	BlockAbsolutePaths  bool     `yaml:"blockAbsolutePaths"`
	BlockPathTraversal  bool     `yaml:"blockPathTraversal"`
}

// --- Quota Layer ---

type SessionBudgetPolicy struct {
	Enabled            bool               `yaml:"enabled"`
	Limits             BudgetLimits       `yaml:"limits"`
	Action             string             `yaml:"action"`
	Anomaly            BudgetAnomalyConfig `yaml:"anomaly"`
	// DelegationFraction is the fraction of the parent's MaxToolCallsPerSession
	// granted to a child session at registration. 0.5 = child gets 50% of parent's limit.
	// Zero disables budget inheritance — child uses the policy default.
	DelegationFraction float64 `yaml:"delegationFraction"`
}

// BudgetAnomalyConfig configures the velocity-based behavioral anomaly signal.
// When an agent makes many semantically repetitive calls in a short window, it
// is treated as a probe pattern and contributes to session risk.
type BudgetAnomalyConfig struct {
	VelocityWindowMinutes    int     `yaml:"velocityWindowMinutes"`    // lookback window (default 5)
	VelocityThresholdCalls   int     `yaml:"velocityThresholdCalls"`   // min calls in window to trigger (default 20)
	VelocityMaxAvgDrift      float64 `yaml:"velocityMaxAvgDrift"`      // avg drift below this = low diversity (default 0.3)
	VelocityRiskContribution float64 `yaml:"velocityRiskContribution"` // risk added when triggered (default 0.15)
}

type BudgetLimits struct {
	MaxTokensPerSession    int     `yaml:"maxTokensPerSession"    json:"maxTokensPerSession"`
	MaxCostPerSessionUSD   float64 `yaml:"maxCostPerSessionUSD"   json:"maxCostPerSessionUSD"`
	MaxToolCallsPerSession int     `yaml:"maxToolCallsPerSession" json:"maxToolCallsPerSession"`
	MaxToolCallsPerMinute  int     `yaml:"maxToolCallsPerMinute"  json:"maxToolCallsPerMinute"`
	IdleTimeoutMinutes     int     `yaml:"idleTimeoutMinutes"     json:"idleTimeoutMinutes"`
	MaxDurationMinutes     int     `yaml:"maxDurationMinutes"     json:"maxDurationMinutes"`
}

type DepthLimiterPolicy struct {
	Enabled               bool   `yaml:"enabled"`
	MaxAgentNestingDepth  int    `yaml:"maxAgentNestingDepth"`
	MaxParallelAgents     int    `yaml:"maxParallelAgents"`
	MaxSubtaskChainLength int    `yaml:"maxSubtaskChainLength"`
	Action                string `yaml:"action"`
}

// --- Outbound Admission ---

type OutputValidationPolicy struct {
	Enabled                  bool     `yaml:"enabled"`
	ScanToolOutputs          bool     `yaml:"scanToolOutputs"`
	SuspiciousOutputPatterns []string `yaml:"suspiciousOutputPatterns"`
	MaxOutputSizeBytes       int      `yaml:"maxOutputSizeBytes"`
	Action                   string   `yaml:"action"`
	SanitizeMode             string   `yaml:"sanitizeMode"`      // "strip" | "redact" | "block"
	RedactReplacement        string   `yaml:"redactReplacement"` // used when sanitizeMode=redact
}

// PIIBiometricPolicy configures content-based detection and redaction of PII and
// biometric-template-shaped data, independent of policies.audit.redactedFields
// (which matches by parameter name and is profile-specific). See pkg/pii.
type PIIBiometricPolicy struct {
	Enabled bool `yaml:"enabled"`
	// Patterns are regexes matched against tool call parameter values and outputs
	// (e.g. SSN-shaped strings).
	Patterns []string `yaml:"patterns"`
	// BiometricKeySuffixes are case-insensitive substrings matched against parameter
	// names (e.g. "_embedding", "_template", "voiceprint").
	BiometricKeySuffixes []string `yaml:"biometricKeySuffixes"`
	// RedactReplacement is substituted for matched content; defaults to "[REDACTED:PII]".
	RedactReplacement string `yaml:"redactReplacement"`
}

// HumanReviewPolicy configures the human-in-loop webhook dispatcher.
type HumanReviewPolicy struct {
	Enabled           bool   `yaml:"enabled"`
	WebhookURL        string `yaml:"webhookURL"`
	TimeoutSeconds    int    `yaml:"timeoutSeconds"`
	AutoDenyOnTimeout bool   `yaml:"autoDenyOnTimeout"`
	StatusPollURL     string `yaml:"statusPollURL,omitempty"`
	StatusPollIntervalMs int `yaml:"statusPollIntervalMs,omitempty"`
}

// --- Audit ---

type AuditPolicy struct {
	Enabled           bool            `yaml:"enabled"`
	LogLevel          string          `yaml:"logLevel"`
	LogAllDecisions   bool            `yaml:"logAllDecisions"`
	LogToolParameters bool            `yaml:"logToolParameters"`
	RedactedFields    []string        `yaml:"redactedFields"`
	Exporters         []AuditExporter `yaml:"exporters"`
}

type AuditExporter struct {
	Type     string `yaml:"type"`
	Endpoint string `yaml:"endpoint,omitempty"`
}

// Load reads and parses the PerchGuard policy file.
// Equivalent to how Dynamic_Pod_Sec loads its security-policies.yaml.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read policy file %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse policy file %q: %w", path, err)
	}

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("policy validation error: %w", err)
	}

	return &cfg, nil
}

// IsObserveMode returns true when the policy is configured for observe-only enforcement.
func (c *Config) IsObserveMode() bool {
	return c.Enforcement == "observe"
}

// validate performs basic sanity checks on the loaded config.
func validate(cfg *Config) error {
	if cfg.Enforcement != "" && cfg.Enforcement != "observe" && cfg.Enforcement != "enforce" {
		return fmt.Errorf("enforcement must be \"observe\" or \"enforce\", got %q", cfg.Enforcement)
	}
	if cfg.Policies.SessionBudget.Enabled {
		if cfg.Policies.SessionBudget.Limits.MaxTokensPerSession <= 0 {
			return fmt.Errorf("sessionBudget.limits.maxTokensPerSession must be > 0")
		}
	}
	if cfg.Policies.DepthLimiter.Enabled {
		if cfg.Policies.DepthLimiter.MaxAgentNestingDepth <= 0 {
			return fmt.Errorf("depthLimiter.maxAgentNestingDepth must be > 0")
		}
	}
	return nil
}
