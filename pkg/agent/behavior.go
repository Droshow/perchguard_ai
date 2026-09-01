package agent

import (
	"strings"

	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

// Stage is an attack-chain phase label assigned to each tool call.
type Stage string

const (
	StageRecon      Stage = "recon"      // information gathering
	StageExploit    Stage = "exploit"    // code/command execution
	StageEscalate   Stage = "escalate"   // privilege or access expansion
	StageExfiltrate Stage = "exfiltrate" // data leaving the boundary
	StageUnknown    Stage = "unknown"
)

// defaultChains are the built-in attack progressions used when no behaviorPatterns
// are configured in policy. Matching is subsequence — intervening calls don't break detection.
var defaultChains = [][]Stage{
	{StageRecon, StageExploit, StageExfiltrate},
	{StageRecon, StageEscalate, StageExfiltrate},
	{StageRecon, StageExploit, StageEscalate},
}

// BehaviorAnalyzer classifies tool calls into attack stages and detects sequences.
type BehaviorAnalyzer struct {
	window int       // number of recent events to consider
	chains [][]Stage // attack chains to detect; falls back to defaultChains if nil
}

func NewBehaviorAnalyzer(window int, chains [][]Stage) *BehaviorAnalyzer {
	if window <= 0 {
		window = 10
	}
	if len(chains) == 0 {
		chains = defaultChains
	}
	return &BehaviorAnalyzer{window: window, chains: chains}
}

// ClassifyToolWithMap maps a tool name to an attack stage using a policy-provided
// map (keys: stage names, values: tool name substrings). Falls back to ClassifyTool
// when the map is nil or the tool is not found.
func ClassifyToolWithMap(toolName string, stageMap map[string][]string) Stage {
	if len(stageMap) == 0 {
		return ClassifyTool(toolName)
	}
	name := strings.ToLower(toolName)
	for stageName, tools := range stageMap {
		if matchesAny(name, tools...) {
			return Stage(stageName)
		}
	}
	return StageUnknown
}

// ClassifyTool is the no-map fallback. Returns StageUnknown so that operators
// are forced to configure toolStageMap in policies.yaml rather than relying on
// hardcoded tool names that will not match every deployment's MCP server.
func ClassifyTool(_ string) Stage {
	return StageUnknown
}

// DetectChain reports whether the recent event window matches a known attack chain.
func (b *BehaviorAnalyzer) DetectChain(events []store.ToolEvent) bool {
	recent := events
	if len(events) > b.window {
		recent = events[len(events)-b.window:]
	}
	for _, chain := range b.chains {
		if containsSequence(recent, chain) {
			return true
		}
	}
	return false
}

// containsSequence checks whether seq appears in order within events (subsequence match).
func containsSequence(events []store.ToolEvent, seq []Stage) bool {
	si := 0
	for _, e := range events {
		if si < len(seq) && e.Stage == string(seq[si]) {
			si++
		}
	}
	return si == len(seq)
}

func matchesAny(name string, patterns ...string) bool {
	for _, p := range patterns {
		if strings.Contains(name, p) {
			return true
		}
	}
	return false
}
