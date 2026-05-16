package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
)

func testServer() *Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/repositories", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"repos": []string{"test/repo"}})
	})
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	return NewServer(mux, logger)
}

func TestHandleHTTPMessage_Initialize(t *testing.T) {
	s := testServer()

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	req := httptest.NewRequest("POST", "/mcp/message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.handleHTTPMessage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp jsonrpcResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != float64(1) {
		t.Errorf("expected id=1, got %v", resp.ID)
	}
	if resp.Error != nil {
		t.Errorf("unexpected error: %v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("initialize result = %#v, want object", resp.Result)
	}
	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("initialize serverInfo = %#v, want object", result["serverInfo"])
	}
	if got, want := serverInfo["version"], buildinfo.AppVersion(); got != want {
		t.Fatalf("initialize server version = %#v, want %#v", got, want)
	}
}

func TestHandleHTTPMessage_ToolsList(t *testing.T) {
	s := testServer()

	body := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	req := httptest.NewRequest("POST", "/mcp/message", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.handleHTTPMessage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatal("missing result")
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatal("missing tools array")
	}
	assertMCPToolCount(t, tools, 66)
}

func TestHandleHTTPMessage_Ping(t *testing.T) {
	s := testServer()

	body := `{"jsonrpc":"2.0","id":3,"method":"ping"}`
	req := httptest.NewRequest("POST", "/mcp/message", strings.NewReader(body))
	rec := httptest.NewRecorder()

	s.handleHTTPMessage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandleHTTPMessage_Notification(t *testing.T) {
	s := testServer()

	body := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	req := httptest.NewRequest("POST", "/mcp/message", strings.NewReader(body))
	rec := httptest.NewRecorder()

	s.handleHTTPMessage(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for notification, got %d", rec.Code)
	}
}

func TestHandleHTTPMessage_InvalidJSON(t *testing.T) {
	s := testServer()

	req := httptest.NewRequest("POST", "/mcp/message", strings.NewReader("{bad json"))
	rec := httptest.NewRecorder()

	s.handleHTTPMessage(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleHTTPMessage_UnknownMethod(t *testing.T) {
	s := testServer()

	body := `{"jsonrpc":"2.0","id":4,"method":"unknown/method"}`
	req := httptest.NewRequest("POST", "/mcp/message", strings.NewReader(body))
	rec := httptest.NewRecorder()

	s.handleHTTPMessage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp jsonrpcResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected code -32601, got %d", resp.Error.Code)
	}
}

func TestHandleHTTPMessage_ToolCall(t *testing.T) {
	s := testServer()

	body := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"list_indexed_repositories","arguments":{}}}`
	req := httptest.NewRequest("POST", "/mcp/message", strings.NewReader(body))
	rec := httptest.NewRecorder()

	s.handleHTTPMessage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatal("missing result")
	}
	content, ok := result["content"].([]any)
	if !ok {
		t.Fatal("missing content array")
	}
	if len(content) == 0 {
		t.Fatal("expected at least one content entry")
	}
	if len(content) != 1 {
		t.Fatalf("expected 1 content entry, got %d", len(content))
	}
}

func TestHandleHTTPMessage_ToolCallStructuredEnvelopeError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v0/code/call-chain", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":  nil,
			"truth": nil,
			"error": map[string]any{
				"code":       "unsupported_capability",
				"message":    "call-chain analysis requires authoritative graph mode",
				"capability": "call_graph.call_chain_path",
				"profiles": map[string]any{
					"current":  "local_lightweight",
					"required": "local_full_stack",
				},
			},
		})
	})
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	s := NewServer(mux, logger)

	body := `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"find_function_call_chain","arguments":{"start":"a","end":"b"}}}`
	req := httptest.NewRequest("POST", "/mcp/message", strings.NewReader(body))
	rec := httptest.NewRecorder()

	s.handleHTTPMessage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatal("missing result")
	}
	if isError, _ := result["isError"].(bool); !isError {
		t.Fatalf("result.isError = %#v, want true", result["isError"])
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) != 2 {
		t.Fatalf("content = %#v, want 2 entries", result["content"])
	}
	resource, ok := content[1].(map[string]any)
	if !ok {
		t.Fatalf("content[1] type = %T, want map[string]any", content[1])
	}
	resourcePayload, ok := resource["resource"].(map[string]any)
	if !ok {
		t.Fatalf("resource payload = %#v, want map[string]any", resource["resource"])
	}
	if !strings.Contains(resourcePayload["text"].(string), `"unsupported_capability"`) {
		t.Fatalf("resource text = %q, want canonical unsupported_capability envelope", resourcePayload["text"])
	}
}

func TestHandleSSE_EndpointEvent(t *testing.T) {
	s := testServer()

	// Start test HTTP server
	ts := httptest.NewServer(http.HandlerFunc(s.handleSSE))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET /sse: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %s", ct)
	}

	// Read the first event (endpoint).
	scanner := bufio.NewScanner(resp.Body)
	var lines []string
	deadline := time.After(2 * time.Second)

	for {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for endpoint event")
		default:
		}

		if !scanner.Scan() {
			break
		}
		line := scanner.Text()
		lines = append(lines, line)
		// An empty line marks end of an SSE event.
		if line == "" && len(lines) > 1 {
			break
		}
	}

	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d: %v", len(lines), lines)
	}

	foundEndpoint := false
	for _, l := range lines {
		if strings.HasPrefix(l, "event: endpoint") {
			foundEndpoint = true
		}
	}
	if !foundEndpoint {
		t.Errorf("expected 'event: endpoint' line, got: %v", lines)
	}

	foundData := false
	for _, l := range lines {
		if strings.HasPrefix(l, "data: /mcp/message?sessionId=") {
			foundData = true
		}
	}
	if !foundData {
		t.Errorf("expected 'data: /mcp/message?sessionId=...' line, got: %v", lines)
	}
}

func TestHandleHTTPMessage_SSESession(t *testing.T) {
	s := testServer()

	// Manually create a session.
	sess := &sseSession{ch: make(chan []byte, 16)}
	s.sessMu.Lock()
	s.sessions["test-session"] = sess
	s.sessMu.Unlock()

	body := `{"jsonrpc":"2.0","id":10,"method":"ping"}`
	req := httptest.NewRequest("POST", "/mcp/message?sessionId=test-session", strings.NewReader(body))
	rec := httptest.NewRecorder()

	s.handleHTTPMessage(rec, req)

	// SSE-linked request returns 202.
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for SSE session, got %d", rec.Code)
	}

	// The response should be in the session channel.
	select {
	case msg := <-sess.ch:
		var resp jsonrpcResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			t.Fatalf("decode SSE message: %v", err)
		}
		if resp.ID != float64(10) {
			t.Errorf("expected id=10, got %v", resp.ID)
		}
	default:
		t.Fatal("expected message in SSE session channel")
	}
}

func TestNewServer_NilLogger(t *testing.T) {
	s := NewServer(http.NewServeMux(), nil)
	if s.logger == nil {
		t.Fatal("expected non-nil logger")
	}
	if s.sessions == nil {
		t.Fatal("expected non-nil sessions map")
	}
}

func TestHandleHTTPMessage_ToolCallError(t *testing.T) {
	s := testServer()

	// Call a tool that doesn't exist in the dispatch table.
	body := `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"nonexistent_tool","arguments":{}}}`
	req := httptest.NewRequest("POST", "/mcp/message", strings.NewReader(body))
	rec := httptest.NewRecorder()

	s.handleHTTPMessage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatal("missing result")
	}
	isError, _ := result["isError"].(bool)
	if !isError {
		t.Error("expected isError=true for unknown tool")
	}
}

// Verify Health endpoint works through RunHTTP mux setup.
func TestHealth_ViaHTTPMux(t *testing.T) {
	s := testServer()

	// Replicate the mux setup from RunHTTP.
	httpMux := http.NewServeMux()
	httpMux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	httpMux.HandleFunc("GET /sse", s.handleSSE)
	httpMux.HandleFunc("POST /mcp/message", s.handleHTTPMessage)
	httpMux.Handle("/api/", s.handler)

	ts := httptest.NewServer(httpMux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["status"] != "ok" {
		t.Errorf("expected status=ok, got %s", result["status"])
	}
}

// Verify API passthrough works.
func TestAPI_Passthrough(t *testing.T) {
	s := testServer()

	httpMux := http.NewServeMux()
	httpMux.HandleFunc("POST /mcp/message", s.handleHTTPMessage)
	httpMux.Handle("/api/", s.handler)

	ts := httptest.NewServer(httpMux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v0/repositories")
	if err != nil {
		t.Fatalf("GET /api/v0/repositories: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body bytes.Buffer
	_, _ = io.Copy(&body, resp.Body)
	if !strings.Contains(body.String(), "test/repo") {
		t.Errorf("expected test/repo in response, got: %s", body.String())
	}
}

func TestHTTPMux_ComposesSharedAdminAndMCPRoutes(t *testing.T) {
	s := testServer()

	adminMux := http.NewServeMux()
	adminMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("admin-ok"))
	})

	httpMux := s.httpMux(adminMux)

	healthzReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthzRec := httptest.NewRecorder()
	httpMux.ServeHTTP(healthzRec, healthzReq)
	if got, want := healthzRec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /healthz status = %d, want %d", got, want)
	}
	if got := strings.TrimSpace(healthzRec.Body.String()); got != "admin-ok" {
		t.Fatalf("GET /healthz body = %q, want %q", got, "admin-ok")
	}

	healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthRec := httptest.NewRecorder()
	httpMux.ServeHTTP(healthRec, healthReq)
	if got, want := healthRec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /health status = %d, want %d", got, want)
	}

	apiReq := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
	apiRec := httptest.NewRecorder()
	httpMux.ServeHTTP(apiRec, apiReq)
	if got, want := apiRec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /api/v0/repositories status = %d, want %d", got, want)
	}
	if got := apiRec.Body.String(); !strings.Contains(got, "test/repo") {
		t.Fatalf("GET /api/v0/repositories body = %q, want test/repo", got)
	}
}
