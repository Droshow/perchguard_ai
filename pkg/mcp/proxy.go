package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Droshow/PerchGuard/perchguard/pkg/admission"
)

// mcpToolResult is the structured result returned by an MCP tools/call response.
// Content is a list of typed blocks; "text" blocks carry the tool output string.
type mcpToolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"content"`
	IsError bool `json:"isError,omitempty"`
}

// Proxy intercepts MCP JSON-RPC messages, runs tools/call requests through
// the PerchGuard admission pipeline, then forwards or blocks.
type Proxy struct {
	interceptor  *admission.Interceptor
	upstream     UpstreamTransport
	downstream   DownstreamTransport
	sessionID    string
	agentID      string
	agentRole    string
	userIntent   string // set per-request from X-Perchguard-User-Intent header
	governedModel string // model being governed, used for cost estimation
}

type Option func(*Proxy)

func WithSessionID(id string) Option      { return func(p *Proxy) { p.sessionID = id } }
func WithAgentID(id string) Option        { return func(p *Proxy) { p.agentID = id } }
func WithAgentRole(role string) Option    { return func(p *Proxy) { p.agentRole = role } }
func WithGovernedModel(m string) Option   { return func(p *Proxy) { p.governedModel = m } }

func NewProxy(i *admission.Interceptor, up UpstreamTransport, down DownstreamTransport, opts ...Option) *Proxy {
	p := &Proxy{
		interceptor: i,
		upstream:    up,
		downstream:  down,
		sessionID:   "mcp-session",
		agentID:     "mcp-proxy",
		agentRole:   "developer_agent",
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Run processes MCP messages sequentially until ctx is cancelled or the
// downstream transport returns EOF (client disconnected).
func (p *Proxy) Run(ctx context.Context) error {
	for {
		req, err := p.downstream.ReadRequest(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("downstream read: %w", err)
		}

		resp, err := p.handle(ctx, req)
		if err != nil {
			resp = errorResponse(req.ID, ErrCodeInternal, err.Error())
		}

		if err := p.downstream.WriteResponse(ctx, resp); err != nil {
			return fmt.Errorf("downstream write: %w", err)
		}
	}
}

// ServeHTTP implements http.Handler so the Proxy can be mounted as an HTTP
// endpoint in server mode. Each request is a single MCP JSON-RPC message;
// governance runs synchronously and the decision is returned inline.
// This is the HTTP equivalent of stdio MCP proxy mode — the downstream agent
// does not need to know PerchGuard exists.
//
// Per-request role override: if the caller sets X-Perchguard-Role, that role
// is used for this request instead of the proxy's configured default. This
// allows multi-role lab scenarios without restarting PerchGuard.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Allow per-request role, session, and user-intent overrides via headers.
	// This lets multi-role / multi-session lab scenarios run without restarting PerchGuard.
	role := p.agentRole
	if h := r.Header.Get("X-Perchguard-Role"); h != "" {
		role = h
	}
	sid := p.sessionID
	if h := r.Header.Get("X-Perchguard-Session-ID"); h != "" {
		sid = h
	}
	userIntent := ""
	if h := r.Header.Get("X-Perchguard-User-Intent"); h != "" {
		userIntent = h
	}
	// Shallow-copy the proxy to apply per-request overrides without mutating shared state.
	scoped := *p
	scoped.agentRole = role
	scoped.sessionID = sid
	scoped.userIntent = userIntent

	resp, err := scoped.handle(r.Context(), &req)
	if err != nil {
		resp = errorResponse(req.ID, ErrCodeInternal, err.Error())
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}

func (p *Proxy) handle(ctx context.Context, req *JSONRPCRequest) (JSONRPCResponse, error) {
	if req.Method != "tools/call" {
		return p.forward(ctx, req)
	}
	return p.handleToolCall(ctx, req)
}

func (p *Proxy) handleToolCall(ctx context.Context, req *JSONRPCRequest) (JSONRPCResponse, error) {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errorResponse(req.ID, ErrCodeInvalidRequest, "invalid tool call params"), nil
	}

	admReq := &admission.ToolCallAdmissionRequest{
		UID:        fmt.Sprintf("%v", req.ID),
		SessionID:  p.sessionID,
		AgentID:    p.agentID,
		AgentRole:  p.agentRole,
		UserIntent: p.userIntent,
		ToolCall: admission.ToolCall{
			Name:       params.Name,
			Parameters: params.Arguments,
		},
	}

	// Estimate request token count and inject for quota cost accounting.
	// byte-length / 4 is the standard GPT tokenization approximation.
	rawArgs, _ := json.Marshal(params.Arguments)
	estimated := len(rawArgs) / 4
	if estimated < 1 {
		estimated = 1
	}
	if admReq.Metadata == nil {
		admReq.Metadata = make(map[string]string)
	}
	admReq.Metadata["tokens_used"] = strconv.Itoa(estimated)
	if p.governedModel != "" {
		admReq.Metadata["model"] = p.governedModel
	}

	// Inbound admission — block or mutate before sending to upstream.
	admResp := p.interceptor.Intercept(ctx, admReq)
	switch admResp.Decision {
	case admission.DecisionDeny, admission.DecisionTerminate:
		return errorResponse(req.ID, ErrCodeToolBlocked, admResp.Reason), nil
	case admission.DecisionMutate:
		if admResp.MutatedCall != nil {
			params.Arguments = admResp.MutatedCall.Parameters
			mutated, err := json.Marshal(params)
			if err != nil {
				return errorResponse(req.ID, ErrCodeInternal, "marshal mutated params"), nil
			}
			req.Params = mutated
		}
	}

	// Forward to upstream MCP server.
	upstreamResp, err := p.forward(ctx, req)
	if err != nil {
		return JSONRPCResponse{}, err
	}

	// Outbound admission — scan the tool result before handing it to the client.
	// This is what makes transparent MCP proxy governance equivalent to explicit HTTP mode:
	// indirect prompt injections hidden in tool outputs are caught and sanitized here.
	return p.scanOutput(ctx, admReq, upstreamResp)
}

// scanOutput runs the outbound admission pipeline on an upstream tool result.
// It parses the MCP content blocks, concatenates all text, and passes it to
// InterceptOutput. On DENY the call is blocked; on MUTATE the sanitized text
// replaces the original content before the response reaches the client.
func (p *Proxy) scanOutput(ctx context.Context, admReq *admission.ToolCallAdmissionRequest, resp JSONRPCResponse) (JSONRPCResponse, error) {
	if resp.Error != nil || resp.Result == nil {
		return resp, nil
	}

	var result mcpToolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil || len(result.Content) == 0 {
		return resp, nil
	}

	// Concatenate all text blocks into one string for the scanner.
	var sb strings.Builder
	for _, c := range result.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	output := sb.String()
	if output == "" {
		return resp, nil
	}

	admReq.ToolOutput = &output
	outboundResp := p.interceptor.InterceptOutput(ctx, admReq)

	switch outboundResp.Decision {
	case admission.DecisionDeny, admission.DecisionTerminate:
		return errorResponse(resp.ID, ErrCodeToolBlocked, outboundResp.Reason), nil
	case admission.DecisionMutate:
		if outboundResp.SanitizedOutput == nil {
			return resp, nil
		}
		// Replace content with a single sanitized text block.
		result.Content = []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		}{{Type: "text", Text: *outboundResp.SanitizedOutput}}
		sanitized, err := json.Marshal(result)
		if err != nil {
			return resp, nil // marshal error — return original (content is safe, scan already ran)
		}
		resp.Result = sanitized
	}
	return resp, nil
}

func (p *Proxy) forward(ctx context.Context, req *JSONRPCRequest) (JSONRPCResponse, error) {
	resp, err := p.upstream.Forward(ctx, *req)
	if err != nil {
		return JSONRPCResponse{}, err
	}
	resp.ID = req.ID // ensure correlation
	return *resp, nil
}
