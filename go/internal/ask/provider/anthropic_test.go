package provider

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAnthropicCompleteParsesTextAndToolUse verifies that Complete sends a
// correct POST to /v1/messages with required headers and a tool definition,
// and correctly maps a response with one text block and one tool_use block.
func TestAnthropicCompleteParsesTextAndToolUse(t *testing.T) {
	t.Parallel()

	type requestBody struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Tools     []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"input_schema"`
		} `json:"tools"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: got %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path: got %q, want /v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("x-api-key: got %q, want %q", got, "test-key")
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Errorf("anthropic-version: got %q, want %q", got, "2023-06-01")
		}

		var body requestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if len(body.Tools) != 1 || body.Tools[0].Name != "search" {
			t.Errorf("expected tool named 'search', got %+v", body.Tools)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"content": [
				{"type": "text", "text": "here is a result"},
				{"type": "tool_use", "id": "tu_1", "name": "search", "input": {"query": "go test"}}
			],
			"stop_reason": "tool_use",
			"usage": {"input_tokens": 42, "output_tokens": 17}
		}`))
	}))
	defer srv.Close()

	adapter := newAnthropicAdapter(srv.URL, "test-key", "claude-3-5-sonnet-20241022", srv.Client())
	if got := adapter.ModelID(); got != "claude-3-5-sonnet-20241022" {
		t.Fatalf("ModelID: got %q, want %q", got, "claude-3-5-sonnet-20241022")
	}

	messages := []Message{
		{Role: RoleUser, Text: "Search for something"},
	}
	tools := []Tool{
		{
			Name:        "search",
			Description: "Search the web",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}},
		},
	}

	comp, err := adapter.Complete(t.Context(), messages, tools)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comp.Text != "here is a result" {
		t.Errorf("Text: got %q, want %q", comp.Text, "here is a result")
	}
	if len(comp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls: got %d, want 1", len(comp.ToolCalls))
	}
	tc := comp.ToolCalls[0]
	if tc.ID != "tu_1" {
		t.Errorf("ToolCall.ID: got %q, want %q", tc.ID, "tu_1")
	}
	if tc.Name != "search" {
		t.Errorf("ToolCall.Name: got %q, want %q", tc.Name, "search")
	}
	if q, ok := tc.Arguments["query"]; !ok || q != "go test" {
		t.Errorf("ToolCall.Arguments[query]: got %v, want %q", q, "go test")
	}
	if comp.Usage.InputTokens != 42 {
		t.Errorf("Usage.InputTokens: got %d, want 42", comp.Usage.InputTokens)
	}
	if comp.Usage.OutputTokens != 17 {
		t.Errorf("Usage.OutputTokens: got %d, want 17", comp.Usage.OutputTokens)
	}
	if comp.StopReason != "tool_use" {
		t.Errorf("StopReason: got %q, want %q", comp.StopReason, "tool_use")
	}
}

// TestAnthropicCompletePropagatesProviderError verifies that a 500 response
// returns a *ProviderError and that the Completion is zero-valued.
func TestAnthropicCompletePropagatesProviderError(t *testing.T) {
	t.Parallel()

	const secretBody = "INTERNAL_SERVER_SECRET"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(secretBody))
	}))
	defer srv.Close()

	// Use a transport with zero retries to keep the test fast.
	adapter := newAnthropicAdapter(srv.URL, "key", "model", srv.Client())
	adapter.t.maxRetries = 0

	comp, err := adapter.Complete(t.Context(), []Message{{Role: RoleUser, Text: "hello"}}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var provErr *ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("expected *ProviderError, got %T: %v", err, err)
	}
	if provErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode: got %d, want 500", provErr.StatusCode)
	}
	if strings.Contains(err.Error(), secretBody) {
		t.Errorf("error must not contain response body text; got: %q", err.Error())
	}
	// Completion should be zero-valued.
	if comp.Text != "" || comp.ToolCalls != nil || comp.StopReason != "" {
		t.Errorf("expected zero Completion, got %+v", comp)
	}
}

// TestAnthropicMapsToolResultMessage verifies that a Message with Role RoleTool
// is encoded as an Anthropic user message containing a tool_result block keyed
// by ToolCallID.
func TestAnthropicMapsToolResultMessage(t *testing.T) {
	t.Parallel()

	type contentBlock struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id"`
		Content   string `json:"content"`
	}
	type message struct {
		Role    string         `json:"role"`
		Content []contentBlock `json:"content"`
	}
	type requestBody struct {
		Messages []message `json:"messages"`
	}

	var captured requestBody

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"content": [{"type": "text", "text": "ok"}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 1, "output_tokens": 1}
		}`))
	}))
	defer srv.Close()

	adapter := newAnthropicAdapter(srv.URL, "key", "model", srv.Client())
	messages := []Message{
		{Role: RoleTool, ToolCallID: "tu_1", Text: `{"x":1}`},
	}

	_, err := adapter.Complete(t.Context(), messages, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(captured.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(captured.Messages))
	}
	msg := captured.Messages[0]
	if msg.Role != "user" {
		t.Errorf("role: got %q, want %q", msg.Role, "user")
	}
	if len(msg.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(msg.Content))
	}
	block := msg.Content[0]
	if block.Type != "tool_result" {
		t.Errorf("block.type: got %q, want %q", block.Type, "tool_result")
	}
	if block.ToolUseID != "tu_1" {
		t.Errorf("tool_use_id: got %q, want %q", block.ToolUseID, "tu_1")
	}
	if block.Content != `{"x":1}` {
		t.Errorf("content: got %q, want %q", block.Content, `{"x":1}`)
	}
}
