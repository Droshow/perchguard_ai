package humanreview

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
)

func reviewReq() admission.ReviewRequest {
	return admission.ReviewRequest{
		RequestUID: "uid-1",
		SessionID:  "sess-1",
		AgentID:    "agent-1",
		UserIntent: "test",
	}
}

func TestDispatcher_SynchronousApprove(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(admission.ReviewResponse{RequestUID: "uid-1", Approved: true})
	}))
	defer srv.Close()

	d := NewDispatcher(srv.URL, 5)
	approved, err := d.Dispatch(context.Background(), reviewReq())
	if err != nil || !approved {
		t.Fatalf("expected approve, got approved=%v err=%v", approved, err)
	}
}

func TestDispatcher_SynchronousDeny(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(admission.ReviewResponse{RequestUID: "uid-1", Approved: false})
	}))
	defer srv.Close()

	d := NewDispatcher(srv.URL, 5)
	approved, err := d.Dispatch(context.Background(), reviewReq())
	if err != nil || approved {
		t.Fatalf("expected deny, got approved=%v err=%v", approved, err)
	}
}

func TestDispatcher_HTTP500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	d := NewDispatcher(srv.URL, 5)
	_, err := d.Dispatch(context.Background(), reviewReq())
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}

func TestDispatcher_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		json.NewEncoder(w).Encode(admission.ReviewResponse{Approved: true})
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	d := NewDispatcher(srv.URL, 5)
	approved, err := d.Dispatch(ctx, reviewReq())
	if err == nil {
		t.Fatalf("expected timeout error, got approved=%v", approved)
	}
}

func TestDispatcher_PollApprove(t *testing.T) {
	callCount := 0
	// Webhook returns 202; poll endpoint returns pending then approved.
	pollSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 3 {
			w.WriteHeader(http.StatusAccepted) // pending
			return
		}
		json.NewEncoder(w).Encode(admission.ReviewResponse{RequestUID: "uid-1", Approved: true})
	}))
	defer pollSrv.Close()

	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer webhookSrv.Close()

	d := NewDispatcher(webhookSrv.URL, 10,
		WithPollConfig(pollSrv.URL, 10*time.Millisecond),
	)
	approved, err := d.Dispatch(context.Background(), reviewReq())
	if err != nil || !approved {
		t.Fatalf("expected poll approve, got approved=%v err=%v", approved, err)
	}
}

func TestDispatcher_PollTimeout(t *testing.T) {
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer webhookSrv.Close()

	pollSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted) // always pending
	}))
	defer pollSrv.Close()

	d := NewDispatcher(webhookSrv.URL, 1, // 1s timeout
		WithPollConfig(pollSrv.URL, 50*time.Millisecond),
	)
	_, err := d.Dispatch(context.Background(), reviewReq())
	if err != ErrTimeout {
		t.Fatalf("expected ErrTimeout, got %v", err)
	}
}
