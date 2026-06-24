package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sync"
	"time"
)

// DownstreamTransport is the connection to the MCP client (e.g. Claude Desktop).
// In stdio mode the proxy reads requests from stdin and writes responses to stdout.
type DownstreamTransport interface {
	ReadRequest(ctx context.Context) (*JSONRPCRequest, error)
	WriteResponse(ctx context.Context, resp JSONRPCResponse) error
	Close() error
}

// UpstreamTransport is the connection to the real MCP server.
// Forward sends a request and returns the server's response synchronously.
type UpstreamTransport interface {
	Forward(ctx context.Context, req JSONRPCRequest) (*JSONRPCResponse, error)
	Close() error
}

// --- StdioTransport (DownstreamTransport) ---

// StdioTransport reads newline-delimited JSON requests from r and writes
// responses to w. Default downstream for --mode=mcp-proxy.
type StdioTransport struct {
	dec *json.Decoder
	enc *json.Encoder
	mu  sync.Mutex
}

func NewStdioTransport(r io.Reader, w io.Writer) *StdioTransport {
	return &StdioTransport{
		dec: json.NewDecoder(r),
		enc: json.NewEncoder(w),
	}
}

func (t *StdioTransport) ReadRequest(_ context.Context) (*JSONRPCRequest, error) {
	var req JSONRPCRequest
	if err := t.dec.Decode(&req); err != nil {
		return nil, err
	}
	return &req, nil
}

func (t *StdioTransport) WriteResponse(_ context.Context, resp JSONRPCResponse) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.enc.Encode(resp)
}

func (t *StdioTransport) Close() error { return nil }

// --- HTTPTransport (UpstreamTransport) ---

// HTTPTransport forwards JSON-RPC requests to an HTTP MCP server via POST.
// Suitable for MCP servers exposed over HTTP/SSE (not stdio subprocess).
type HTTPTransport struct {
	url  string
	http *http.Client
}

func NewHTTPTransport(url string) *HTTPTransport {
	return &HTTPTransport{
		url:  url,
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

func (t *HTTPTransport) Forward(ctx context.Context, req JSONRPCRequest) (*JSONRPCResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := t.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	var rpcResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &rpcResp, nil
}

func (t *HTTPTransport) Close() error { return nil }

// --- MemTransport (testing only) ---

// MemTransport is an in-memory transport for tests.
// Implements both DownstreamTransport and UpstreamTransport.
type MemTransport struct {
	requests  chan JSONRPCRequest
	responses chan JSONRPCResponse
}

func NewMemTransport(buf int) *MemTransport {
	return &MemTransport{
		requests:  make(chan JSONRPCRequest, buf),
		responses: make(chan JSONRPCResponse, buf),
	}
}

// Push queues a request to be read by ReadRequest.
func (m *MemTransport) Push(req JSONRPCRequest) { m.requests <- req }

// Pop retrieves a response written by WriteResponse or Forward.
func (m *MemTransport) Pop() JSONRPCResponse { return <-m.responses }

func (m *MemTransport) ReadRequest(ctx context.Context) (*JSONRPCRequest, error) {
	select {
	case req := <-m.requests:
		return &req, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *MemTransport) WriteResponse(_ context.Context, resp JSONRPCResponse) error {
	m.responses <- resp
	return nil
}

// Forward implements UpstreamTransport for MemTransport.
// It echoes back a success response with the request's ID.
func (m *MemTransport) Forward(_ context.Context, req JSONRPCRequest) (*JSONRPCResponse, error) {
	result, _ := json.Marshal(map[string]string{"status": "ok"})
	return &JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: result}, nil
}

func (m *MemTransport) Close() error { return nil }

// --- SubprocessTransport (UpstreamTransport) ---

// SubprocessTransport launches a command as a subprocess and forwards JSON-RPC
// requests over its stdin/stdout. This is the correct transport for stdio-type
// MCP servers (e.g. "npx -y @modelcontextprotocol/server-filesystem /path").
//
// PerchGuard sits between VS Code and the real stdio server:
//
//	VS Code ──stdio──▶ perchguard (mcp-proxy) ──stdin/stdout──▶ real MCP server
type SubprocessTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	enc    *json.Encoder
	dec    *json.Decoder
	mu     sync.Mutex
}

// NewSubprocessTransport creates a SubprocessTransport but does not start the
// subprocess yet. Call Start() before use.
func NewSubprocessTransport(command string, args []string, env []string) *SubprocessTransport {
	cmd := exec.Command(command, args...)
	if len(env) > 0 {
		cmd.Env = env
	}
	return &SubprocessTransport{cmd: cmd}
}

// Start launches the subprocess. Must be called before Forward.
func (t *SubprocessTransport) Start() error {
	stdin, err := t.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("subprocess stdin pipe: %w", err)
	}
	stdout, err := t.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("subprocess stdout pipe: %w", err)
	}
	if err := t.cmd.Start(); err != nil {
		return fmt.Errorf("subprocess start: %w", err)
	}
	t.stdin = stdin
	t.stdout = stdout
	t.enc = json.NewEncoder(stdin)
	t.dec = json.NewDecoder(stdout)
	return nil
}

// Forward writes req to the subprocess stdin and reads the response from stdout.
// JSON-RPC over stdio is inherently sequential per connection, so requests are
// serialised by a mutex.
func (t *SubprocessTransport) Forward(_ context.Context, req JSONRPCRequest) (*JSONRPCResponse, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.enc.Encode(req); err != nil {
		return nil, fmt.Errorf("subprocess write: %w", err)
	}
	var resp JSONRPCResponse
	if err := t.dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("subprocess read: %w", err)
	}
	return &resp, nil
}

// Close terminates the subprocess gracefully.
func (t *SubprocessTransport) Close() error {
	if t.stdin != nil {
		_ = t.stdin.Close()
	}
	if t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
		_ = t.cmd.Wait()
	}
	return nil
}
