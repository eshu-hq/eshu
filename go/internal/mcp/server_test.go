// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	assertMCPToolCount(t, tools, 160)
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
	if len(content) != 2 {
		t.Fatalf("expected text summary and resource payload, got %d content entries", len(content))
	}
}

func TestHandleHTTPMessage_ToolCallPlainJSONReturnsStructuredPayload(t *testing.T) {
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
	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("structuredContent = %#v, want API payload object", result["structuredContent"])
	}
	repos, ok := structured["repos"].([]any)
	if !ok || len(repos) != 1 || repos[0] != "test/repo" {
		t.Fatalf("structuredContent.repos = %#v, want [test/repo]", structured["repos"])
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) != 2 {
		t.Fatalf("content = %#v, want text summary plus resource payload", result["content"])
	}
	resourceBlock, ok := content[1].(map[string]any)
	if !ok {
		t.Fatalf("content[1] = %T, want resource block", content[1])
	}
	resource, ok := resourceBlock["resource"].(map[string]any)
	if !ok {
		t.Fatalf("content[1].resource = %#v, want resource payload", resourceBlock["resource"])
	}
	if got, want := resource["uri"], "eshu://tool-result/payload"; got != want {
		t.Fatalf("resource.uri = %#v, want %#v", got, want)
	}
	if got, want := resource["mimeType"], "application/json"; got != want {
		t.Fatalf("resource.mimeType = %#v, want %#v", got, want)
	}
	if !strings.Contains(resource["text"].(string), `"test/repo"`) {
		t.Fatalf("resource.text = %q, want original API payload", resource["text"])
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
	if structured, ok := result["structuredContent"].(map[string]any); !ok {
		t.Fatalf("structuredContent = %#v, want canonical envelope object", result["structuredContent"])
	} else if _, ok := structured["error"].(map[string]any); !ok {
		t.Fatalf("structuredContent.error = %#v, want error object", structured["error"])
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
