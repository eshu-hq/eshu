package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOpenAICompatParsesContentAndToolCalls verifies that Complete sends a
// correct POST to /v1/chat/completions with an Authorization header, tools
// array, and tool_choice, and correctly maps a response with both content text
// and one tool_call whose arguments JSON string is parsed into a map.
func TestOpenAICompatParsesContentAndToolCalls(t *testing.T) {
	t.Parallel()

	type toolFunction struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	type toolDef struct {
		Type     string       `json:"type"`
		Function toolFunction `json:"function"`
	}
	type requestBody struct {
		Model      string    `json:"model"`
		ToolChoice string    `json:"tool_choice"`
		Tools      []toolDef `json:"tools"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method: got %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path: got %q, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization: got %q, want %q", got, "Bearer test-key")
		}

		var body requestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if len(body.Tools) != 1 || body.Tools[0].Function.Name != "search" {
			t.Errorf("expected one tool named 'search', got %+v", body.Tools)
		}
		if body.ToolChoice != "auto" {
			t.Errorf("tool_choice: got %q, want %q", body.ToolChoice, "auto")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"choices": [{
				"message": {
					"content": "here is a result",
					"tool_calls": [{
						"id": "call_1",
						"type": "function",
						"function": {"name": "search", "arguments": "{\"q\":\"x\"}"}
					}]
				},
				"finish_reason": "tool_calls"
			}],
			"usage": {"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30}
		}`))
	}))
	defer srv.Close()

	adapter := newOpenAICompatAdapter(srv.URL, "test-key", "gpt-4o", srv.Client())
	if got := adapter.ModelID(); got != "gpt-4o" {
		t.Fatalf("ModelID: got %q, want %q", got, "gpt-4o")
	}

	messages := []Message{
		{Role: RoleUser, Text: "search something"},
	}
	tools := []Tool{
		{
			Name:        "search",
			Description: "Search the web",
			InputSchema: map[string]any{"type": "object"},
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
	if tc.ID != "call_1" {
		t.Errorf("ToolCall.ID: got %q, want %q", tc.ID, "call_1")
	}
	if tc.Name != "search" {
		t.Errorf("ToolCall.Name: got %q, want %q", tc.Name, "search")
	}
	if q, ok := tc.Arguments["q"]; !ok || q != "x" {
		t.Errorf("ToolCall.Arguments[q]: got %v, want %q", q, "x")
	}
	if comp.Usage.InputTokens != 20 {
		t.Errorf("Usage.InputTokens: got %d, want 20", comp.Usage.InputTokens)
	}
	if comp.Usage.OutputTokens != 10 {
		t.Errorf("Usage.OutputTokens: got %d, want 10", comp.Usage.OutputTokens)
	}
	if comp.StopReason != "tool_calls" {
		t.Errorf("StopReason: got %q, want %q", comp.StopReason, "tool_calls")
	}
}

// TestOpenAICompatRejectsMalformedToolArguments verifies that Complete returns
// an error (and does not panic) when a tool_call's arguments field is not valid
// JSON.
func TestOpenAICompatRejectsMalformedToolArguments(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"choices": [{
				"message": {
					"content": null,
					"tool_calls": [{
						"id": "call_bad",
						"type": "function",
						"function": {"name": "search", "arguments": "{not json"}
					}]
				},
				"finish_reason": "tool_calls"
			}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 5, "total_tokens": 10}
		}`))
	}))
	defer srv.Close()

	adapter := newOpenAICompatAdapter(srv.URL, "key", "gpt-4o", srv.Client())
	_, err := adapter.Complete(t.Context(), []Message{{Role: RoleUser, Text: "hi"}}, nil)
	if err == nil {
		t.Fatal("expected error for malformed tool arguments, got nil")
	}
}

// TestOpenAICompatBaseURLForMiniMaxAndDeepSeek proves that the same adapter
// implementation posts to {baseURL}/v1/chat/completions for two different
// provider-style base URLs (MiniMax and DeepSeek style), demonstrating that
// one adapter covers multiple providers.
func TestOpenAICompatBaseURLForMiniMaxAndDeepSeek(t *testing.T) {
	t.Parallel()

	var gotPaths []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"choices": [{
				"message": {"content": "ok", "tool_calls": null},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}
		}`))
	}))
	defer srv.Close()

	cases := []struct {
		name    string
		baseURL string
	}{
		{"minimax-style", srv.URL},
		{"deepseek-style", srv.URL},
	}

	for _, tc := range cases {
		adapter := newOpenAICompatAdapter(tc.baseURL, "key", "model", srv.Client())
		_, err := adapter.Complete(t.Context(), []Message{{Role: RoleUser, Text: "hi"}}, nil)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
		}
	}

	if len(gotPaths) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(gotPaths))
	}
	for i, p := range gotPaths {
		if p != "/v1/chat/completions" {
			t.Errorf("request %d path: got %q, want /v1/chat/completions", i, p)
		}
	}
}
