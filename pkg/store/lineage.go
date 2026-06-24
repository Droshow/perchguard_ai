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
	g.add(outRef, inRefs, toolName)
}

// Evict removes the lineage graph for a session once the session is closed.
func (ls *LineageStore) Evict(sessionID string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	delete(ls.graphs, sessionID)
}

// LineageGraph tracks data provenance within one session.
// edges maps each produced ref to the refs that were consumed to produce it.
// tools maps each ref to the tool that produced it.
type LineageGraph struct {
	mu    sync.Mutex
	edges map[string][]string // outRef → inRefs
	tools map[string]string   // outRef → toolName
}

func newLineageGraph() *LineageGraph {
	return &LineageGraph{
		edges: make(map[string][]string),
		tools: make(map[string]string),
	}
}

func (g *LineageGraph) add(outRef string, inRefs []string, toolName string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.edges[outRef] = inRefs
	g.tools[outRef] = toolName
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
