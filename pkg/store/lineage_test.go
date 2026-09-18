package store

import "testing"

func TestLineageStore_SeedInherited_CopiesVerifiedRef(t *testing.T) {
	ls := NewLineageStore()
	ls.AddEdge("parent", "refA", nil, "read_policy")

	ls.SeedInherited("child", ls.Graph("parent"), []string{"refA"})

	child := ls.Graph("child")
	if child == nil || !child.HasAny([]string{"refA"}) {
		t.Fatal("want refA visible in child's graph after SeedInherited")
	}
	tool, origin, ok := child.Producer("refA")
	if !ok {
		t.Fatal("want Producer to find refA in child graph")
	}
	if tool != "read_policy" {
		t.Errorf("want tool=read_policy, got %s", tool)
	}
	if origin != "parent" {
		t.Errorf("want origin=parent (true producer), got %s", origin)
	}
}

func TestLineageStore_SeedInherited_UnknownRef_NotCopied(t *testing.T) {
	ls := NewLineageStore()
	ls.AddEdge("parent", "refA", nil, "read_policy")

	// "refB" was never produced by the parent — SeedInherited must not fabricate it.
	ls.SeedInherited("child", ls.Graph("parent"), []string{"refB"})

	child := ls.Graph("child")
	if child != nil && child.HasAny([]string{"refB"}) {
		t.Error("want refB absent from child graph — parent never produced it")
	}
}

func TestLineageStore_SeedInherited_NoParentGraph_NoOp(t *testing.T) {
	ls := NewLineageStore()
	// Parent has no graph at all (never made a tracked call).
	ls.SeedInherited("child", ls.Graph("parent"), []string{"refA"})
	if g := ls.Graph("child"); g != nil {
		t.Error("want no child graph created when parent has none")
	}
}

func TestLineageStore_SeedInherited_DoesNotOverwriteSelfProduced(t *testing.T) {
	ls := NewLineageStore()
	ls.AddEdge("parent", "sharedRef", nil, "parent_tool")
	ls.AddEdge("child", "sharedRef", nil, "child_tool")

	ls.SeedInherited("child", ls.Graph("parent"), []string{"sharedRef"})

	tool, origin, ok := ls.Graph("child").Producer("sharedRef")
	if !ok {
		t.Fatal("want sharedRef present")
	}
	if tool != "child_tool" || origin != "child" {
		t.Errorf("want child's own record preserved (child_tool/child), got %s/%s", tool, origin)
	}
}

// Multi-hop: a grandchild inherits a ref that the child itself only knows about
// because it was inherited from the grandparent — Producer must report the true
// (grandparent) origin, not the immediate parent, since the child's graph already
// carries the correct origin from its own SeedInherited call.
func TestLineageStore_SeedInherited_TransitiveOriginPreserved(t *testing.T) {
	ls := NewLineageStore()
	ls.AddEdge("grandparent", "refG", nil, "produce_secret")
	ls.SeedInherited("parent", ls.Graph("grandparent"), []string{"refG"})
	ls.SeedInherited("child", ls.Graph("parent"), []string{"refG"})

	tool, origin, ok := ls.Graph("child").Producer("refG")
	if !ok {
		t.Fatal("want refG present in child graph")
	}
	if tool != "produce_secret" || origin != "grandparent" {
		t.Errorf("want true origin (produce_secret/grandparent) preserved through the chain, got %s/%s", tool, origin)
	}
}

func TestLineageGraph_Snapshot_ReportsInRefOrigin(t *testing.T) {
	ls := NewLineageStore()
	ls.AddEdge("parent", "refA", nil, "read_policy")
	ls.SeedInherited("child", ls.Graph("parent"), []string{"refA"})
	// Child consumes the inherited ref in a call of its own.
	ls.AddEdge("child", "refB", []string{"refA"}, "send_email")

	edges := ls.Graph("child").Snapshot()
	if len(edges) != 1 {
		t.Fatalf("want 1 edge, got %d", len(edges))
	}
	e := edges[0]
	if e.OutRef != "refB" || e.Tool != "send_email" || e.InRef != "refA" || e.InRefOrigin != "parent" {
		t.Errorf("unexpected edge: %+v", e)
	}
}
