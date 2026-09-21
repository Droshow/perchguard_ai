package store

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// LineageRef returns a deterministic 12-char hex identifier for one tool call's output.
// Computed from session_id and request_uid so caller and PerchGuard derive the same ref
// without coordination — callers include it in DataRefsIn of subsequent calls.
func LineageRef(sessionID, requestUID string) string {
	h := sha256.Sum256([]byte(sessionID + ":" + requestUID))
	return hex.EncodeToString(h[:6])
}

// LineageStore holds one LineageGraph per active session.
type LineageStore struct {
	mu     sync.RWMutex
	graphs map[string]*LineageGraph
}

func NewLineageStore() *LineageStore {
	return &LineageStore{graphs: make(map[string]*LineageGraph)}
}

// Graph returns the LineageGraph for a session, or nil if none exists yet.
func (ls *LineageStore) Graph(sessionID string) *LineageGraph {
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	return ls.graphs[sessionID]
}

// AddEdge records that outRef was produced by a call to toolName consuming inRefs.
// Creates the session graph on first use.
func (ls *LineageStore) AddEdge(sessionID, outRef string, inRefs []string, toolName string) {
	ls.mu.Lock()
	g, ok := ls.graphs[sessionID]
	if !ok {
		g = newLineageGraph()
		ls.graphs[sessionID] = g
	}
	ls.mu.Unlock()
	g.add(sessionID, outRef, inRefs, toolName)
}

// SeedInherited copies refs a child session claims to have received from its parent
// into the child's own lineage graph — but only refs parentGraph actually produced.
// A claim that doesn't check out against parentGraph is silently dropped rather than
// trusted; callers that need to reject the registration outright should check
// parentGraph themselves first (see pkg/api/agents.go delegation handling, which
// verifies ParentSessionID the same way before ever reaching here).
//
// Callers must pass the same *LineageGraph they already verified refs against
// (typically via LineageStore.Graph(parentSessionID) earlier in the same request),
// not re-resolve it by ID here. Re-resolving would open a TOCTOU window: if the
// parent session is evicted between verification and seeding, a fresh Graph()
// lookup returns nil and the child silently gets no lineage at all, even for
// already-verified refs. Taking the pointer directly means eviction after
// verification can't affect this call.
//
// The seeded refs keep their true origin session and producing tool, so a later
// exfiltration check on the child session (LineageGraph.HasAny) and the swarm graph
// endpoint both see the real provenance, not the child session as a false origin.
func (ls *LineageStore) SeedInherited(childSessionID string, parentGraph *LineageGraph, refs []string) {
	if parentGraph == nil {
		return
	}

	ls.mu.Lock()
	child, ok := ls.graphs[childSessionID]
	if !ok {
		child = newLineageGraph()
		ls.graphs[childSessionID] = child
	}
	ls.mu.Unlock()

	for _, ref := range refs {
		tool, originSession, ok := parentGraph.Producer(ref)
		if !ok {
			continue
		}
		child.seedRef(ref, tool, originSession)
	}
}

// Evict removes the lineage graph for a session once the session is closed.
func (ls *LineageStore) Evict(sessionID string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	delete(ls.graphs, sessionID)
}

// LineageGraph tracks data provenance for one session.
// edges maps each produced ref to the refs that were consumed to produce it.
// tools maps each ref to the tool that produced it.
// origin maps each ref to the session that actually produced it — normally this
// session's own ID, but for a ref seeded via SeedInherited, the session further up
// the delegation chain where the data really originated.
type LineageGraph struct {
	mu     sync.Mutex
	edges  map[string][]string // outRef → inRefs
	tools  map[string]string   // outRef → toolName
	origin map[string]string   // outRef → session that produced it
}

func newLineageGraph() *LineageGraph {
	return &LineageGraph{
		edges:  make(map[string][]string),
		tools:  make(map[string]string),
		origin: make(map[string]string),
	}
}

func (g *LineageGraph) add(sessionID, outRef string, inRefs []string, toolName string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.edges[outRef] = inRefs
	g.tools[outRef] = toolName
	g.origin[outRef] = sessionID
}

// seedRef records ref as already known to this graph, produced by toolName in
// originSession. Used only by SeedInherited. A ref this graph already knows about
// (self-produced or previously seeded) is left untouched.
func (g *LineageGraph) seedRef(ref, toolName, originSession string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, exists := g.tools[ref]; exists {
		return
	}
	g.tools[ref] = toolName
	g.origin[ref] = originSession
}

// HasAny reports whether any of the given refs was produced by a prior governed call
// in this session — i.e., it exists in the graph as a known output.
func (g *LineageGraph) HasAny(refs []string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, ref := range refs {
		if _, ok := g.tools[ref]; ok {
			return true
		}
	}
	return false
}

// Producer returns the tool that produced ref and the session where it was actually
// produced (which may differ from this graph's own session if ref was itself
// inherited from further up the delegation chain). ok is false if ref is not a
// known output in this graph.
func (g *LineageGraph) Producer(ref string) (tool, originSession string, ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	tool, ok = g.tools[ref]
	if !ok {
		return "", "", false
	}
	return tool, g.origin[ref], true
}

// LineageEdge is one outRef → inRef data-flow edge in a session's graph, annotated
// with the tool that produced outRef and the session that actually produced inRef
// (see LineageGraph.origin). Used to render the swarm graph's data-flow edges.
type LineageEdge struct {
	OutRef      string
	Tool        string
	InRef       string
	InRefOrigin string
}

// Snapshot returns every edge currently recorded in this graph.
func (g *LineageGraph) Snapshot() []LineageEdge {
	g.mu.Lock()
	defer g.mu.Unlock()
	edges := make([]LineageEdge, 0, len(g.edges))
	for outRef, inRefs := range g.edges {
		tool := g.tools[outRef]
		for _, inRef := range inRefs {
			edges = append(edges, LineageEdge{
				OutRef:      outRef,
				Tool:        tool,
				InRef:       inRef,
				InRefOrigin: g.origin[inRef],
			})
		}
	}
	return edges
}
