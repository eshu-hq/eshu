package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// openAIStreamBody builds a minimal OpenAI-compatible SSE stream body.
// Each token in tokens becomes one data: chunk; [DONE] is appended at the end.
// When toolName/toolID are non-empty a tool_call chunk is injected after all tokens.
func openAIStreamBody(tokens []string, toolID, toolName, toolArgsJSON string) string {
	var b strings.Builder
	for i, tok := range tokens {
		// Escape the token for JSON string embedding.
		escaped := strings.ReplaceAll(tok, `"`, `\"`)
		fmt.Fprintf(&b, "data: {\"choices\":[{\"delta\":{\"content\":\"%s\"},\"finish_reason\":null}],\"usage\":null}\n\n", escaped)
		_ = i
	}
	if toolID != "" && toolName != "" {
		// First chunk: tool call start with id and name.
		fmt.Fprintf(
			&b,
			"data: {\"choices\":[{\"delta\":{\"content\":null,\"tool_calls\":[{\"index\":0,\"id\":%q,\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":\"\"}}]},\"finish_reason\":null}],\"usage\":null}\n\n",
			toolID, toolName,
		)
		// Second chunk: arguments delta.
		escaped := strings.ReplaceAll(toolArgsJSON, `"`, `\"`)
		fmt.Fprintf(
			&b,
			"data: {\"choices\":[{\"delta\":{\"content\":null,\"tool_calls\":[{\"index\":0,\"id\":\"\",\"type\":\"function\",\"function\":{\"name\":\"\",\"arguments\":\"%s\"}}]},\"finish_reason\":null}],\"usage\":null}\n\n",
			escaped,
		)
	}
	// Final usage chunk.
	b.WriteString("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":3,\"total_tokens\":8}}\n\n")
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

// TestOpenAICompatCompleteStream_TokenDeltas verifies that CompleteStream calls
// emit exactly once per token chunk, assembles the full text in the returned
// Completion, and reports correct usage.
func TestOpenAICompatCompleteStream_TokenDeltas(t *testing.T) {
	t.Parallel()

	tokens := []string{"Hello", ", ", "world!"}
	streamBody := openAIStreamBody(tokens, "", "", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(streamBody))
	}))
	defer srv.Close()

	adapter := newOpenAICompatAdapter(srv.URL, "key", "gpt-4o", srv.Client())

	var deltas []string
	comp, err := adapter.CompleteStream(t.Context(), []Message{{Role: RoleUser, Text: "hi"}}, nil, func(ev StreamEvent) {
		if ev.Kind == StreamEventToken {
			deltas = append(deltas, ev.TextDelta)
		}
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	if len(deltas) != 3 {
		t.Errorf("emit calls = %d, want 3; deltas = %v", len(deltas), deltas)
	}
	want := "Hello, world!"
	if comp.Text != want {
		t.Errorf("Completion.Text = %q, want %q", comp.Text, want)
	}
	concatenated := strings.Join(deltas, "")
	if concatenated != want {
		t.Errorf("concatenated deltas = %q, want %q", concatenated, want)
	}
	if comp.Usage.InputTokens != 5 {
		t.Errorf("InputTokens = %d, want 5", comp.Usage.InputTokens)
	}
	if comp.Usage.OutputTokens != 3 {
		t.Errorf("OutputTokens = %d, want 3", comp.Usage.OutputTokens)
	}
}

// TestOpenAICompatCompleteStream_ToolCall verifies that CompleteStream emits a
// StreamEventToolCallStarted event and assembles the ToolCall in the returned
// Completion with parsed arguments.
func TestOpenAICompatCompleteStream_ToolCall(t *testing.T) {
	t.Parallel()

	streamBody := openAIStreamBody(nil, "call_abc", "list_repos", `{"limit":10}`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(streamBody))
	}))
	defer srv.Close()

	adapter := newOpenAICompatAdapter(srv.URL, "key", "model", srv.Client())

	var toolEvents []StreamEvent
	comp, err := adapter.CompleteStream(t.Context(), []Message{{Role: RoleUser, Text: "hi"}}, nil, func(ev StreamEvent) {
		if ev.Kind == StreamEventToolCallStarted {
			toolEvents = append(toolEvents, ev)
		}
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	if len(toolEvents) != 1 {
		t.Fatalf("tool_call_started events = %d, want 1", len(toolEvents))
	}
	if toolEvents[0].ToolCallID != "call_abc" {
		t.Errorf("ToolCallID = %q, want %q", toolEvents[0].ToolCallID, "call_abc")
	}
	if toolEvents[0].ToolName != "list_repos" {
		t.Errorf("ToolName = %q, want %q", toolEvents[0].ToolName, "list_repos")
	}

	if len(comp.ToolCalls) != 1 {
		t.Fatalf("Completion.ToolCalls = %d, want 1", len(comp.ToolCalls))
	}
	tc := comp.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("ToolCall.ID = %q, want %q", tc.ID, "call_abc")
	}
	if tc.Name != "list_repos" {
		t.Errorf("ToolCall.Name = %q, want %q", tc.Name, "list_repos")
	}
	if v, ok := tc.Arguments["limit"]; !ok || v != float64(10) {
		t.Errorf("ToolCall.Arguments[limit] = %v, want 10", v)
	}
}

// TestOpenAICompatCompleteStream_RejectsStreamWithoutDONE verifies that a stream
// which delivers content and then ends (EOF) without the "data: [DONE]"
// terminator — a mid-stream disconnect after HTTP 200 — is treated as an error
// rather than a successful (but truncated) completion.
func TestOpenAICompatCompleteStream_RejectsStreamWithoutDONE(t *testing.T) {
	t.Parallel()

	body := `data: {"choices":[{"delta":{"content":"partial answer"},"finish_reason":null}],"usage":null}` + "\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body)) // no "data: [DONE]" — connection just ends
	}))
	defer srv.Close()

	adapter := newOpenAICompatAdapter(srv.URL, "key", "model", srv.Client())
	_, err := adapter.CompleteStream(t.Context(), []Message{{Role: RoleUser, Text: "hi"}}, nil, func(StreamEvent) {})
	if err == nil {
		t.Fatal("expected error for stream ending before [DONE], got nil")
	}
	if !strings.Contains(err.Error(), "[DONE]") {
		t.Errorf("error = %v, want a missing-[DONE] terminator error", err)
	}
}

// TestOpenAICompatCompleteStream_PreservesFirstToolArgDelta verifies that when
// the first streamed tool-call chunk carries argument bytes alongside the id and
// name, those bytes are not dropped when the entry is created.
func TestOpenAICompatCompleteStream_PreservesFirstToolArgDelta(t *testing.T) {
	t.Parallel()

	// First chunk: id + name + the opening argument bytes together.
	// Second chunk: the remaining argument bytes. Both must survive to parse.
	body := `data: {"choices":[{"delta":{"content":null,"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"list_repos","arguments":"{\"limit\":"}}]},"finish_reason":null}],"usage":null}` + "\n\n" +
		`data: {"choices":[{"delta":{"content":null,"tool_calls":[{"index":0,"id":"","type":"function","function":{"name":"","arguments":"10}"}}]},"finish_reason":null}],"usage":null}` + "\n\n" +
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}` + "\n\n" +
		`data: [DONE]` + "\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	adapter := newOpenAICompatAdapter(srv.URL, "key", "model", srv.Client())
	comp, err := adapter.CompleteStream(t.Context(), []Message{{Role: RoleUser, Text: "hi"}}, nil, func(StreamEvent) {})
	if err != nil {
		// Without the fix, the first chunk's `{"limit":` is dropped, leaving the
		// unparseable `10}`, which surfaces here as a malformed-arguments error.
		t.Fatalf("CompleteStream error: %v", err)
	}
	if len(comp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %d, want 1", len(comp.ToolCalls))
	}
	if v, ok := comp.ToolCalls[0].Arguments["limit"]; !ok || v != float64(10) {
		t.Errorf("Arguments[limit] = %v (ok=%v), want 10 — first arg delta was dropped", v, ok)
	}
}

// TestOpenAICompatCompleteStream_ProviderError verifies that a non-2xx HTTP
// status from the provider returns an error and the error message contains only
// the status code (never the raw response body).
func TestOpenAICompatCompleteStream_ProviderError(t *testing.T) {
	t.Parallel()

	const secretBody = "SECRET_PROVIDER_TOKEN_XYZ"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(secretBody))
	}))
	defer srv.Close()

	adapter := newOpenAICompatAdapter(srv.URL, "bad-key", "model", srv.Client())
	_, err := adapter.CompleteStream(t.Context(), []Message{{Role: RoleUser, Text: "hi"}}, nil, func(StreamEvent) {})
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	if strings.Contains(err.Error(), secretBody) {
		t.Errorf("error leaked secret body: %v", err)
	}
}

// anthropicStreamBody builds a minimal Anthropic SSE stream body.
// Each token in tokens becomes a content_block_delta event.
func anthropicStreamBody(tokens []string, toolID, toolName, toolArgsJSON string) string {
	var b strings.Builder

	// message_start
	b.WriteString("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n")

	if len(tokens) > 0 {
		// text block start
		b.WriteString("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		for _, tok := range tokens {
			escaped := strings.ReplaceAll(tok, `"`, `\"`)
			fmt.Fprintf(
				&b,
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"%s\"}}\n\n",
				escaped,
			)
		}
		b.WriteString("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
	}

	if toolID != "" && toolName != "" {
		// tool_use block start
		fmt.Fprintf(
			&b,
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":%q,\"name\":%q,\"input\":{}}}\n\n",
			toolID, toolName,
		)
		// input_json_delta
		escaped := strings.ReplaceAll(toolArgsJSON, `"`, `\"`)
		fmt.Fprintf(
			&b,
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"%s\"}}\n\n",
			escaped,
		)
		b.WriteString("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n")
	}

	// message_delta with stop_reason and usage
	b.WriteString("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n")
	b.WriteString("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")

	return b.String()
}

// TestAnthropicCompleteStream_TokenDeltas verifies that CompleteStream emits
// StreamEventToken events for each text_delta chunk and assembles the full text.
func TestAnthropicCompleteStream_TokenDeltas(t *testing.T) {
	t.Parallel()

	tokens := []string{"The", " answer", " is 42."}
	streamBody := anthropicStreamBody(tokens, "", "", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(streamBody))
	}))
	defer srv.Close()

	adapter := newAnthropicAdapter(srv.URL, "key", "claude-3-5-sonnet-20241022", srv.Client())

	var deltas []string
	comp, err := adapter.CompleteStream(t.Context(), []Message{{Role: RoleUser, Text: "hi"}}, nil, func(ev StreamEvent) {
		if ev.Kind == StreamEventToken {
			deltas = append(deltas, ev.TextDelta)
		}
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	if len(deltas) != 3 {
		t.Errorf("emit calls = %d, want 3; deltas = %v", len(deltas), deltas)
	}
	want := "The answer is 42."
	if comp.Text != want {
		t.Errorf("Completion.Text = %q, want %q", comp.Text, want)
	}
	if strings.Join(deltas, "") != want {
		t.Errorf("concatenated deltas = %q, want %q", strings.Join(deltas, ""), want)
	}
	if comp.Usage.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", comp.Usage.InputTokens)
	}
	if comp.Usage.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", comp.Usage.OutputTokens)
	}
	if comp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want end_turn", comp.StopReason)
	}
}

// TestAnthropicCompleteStream_ToolCall verifies that CompleteStream emits a
// StreamEventToolCallStarted event and assembles the ToolCall in the returned
// Completion with parsed input arguments.
func TestAnthropicCompleteStream_ToolCall(t *testing.T) {
	t.Parallel()

	streamBody := anthropicStreamBody(nil, "toolu_01", "list_services", `{"limit":5}`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(streamBody))
	}))
	defer srv.Close()

	adapter := newAnthropicAdapter(srv.URL, "key", "claude-3-5-sonnet-20241022", srv.Client())

	var toolEvents []StreamEvent
	comp, err := adapter.CompleteStream(t.Context(), []Message{{Role: RoleUser, Text: "hi"}}, nil, func(ev StreamEvent) {
		if ev.Kind == StreamEventToolCallStarted {
			toolEvents = append(toolEvents, ev)
		}
	})
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	if len(toolEvents) != 1 {
		t.Fatalf("tool_call_started events = %d, want 1", len(toolEvents))
	}
	if toolEvents[0].ToolCallID != "toolu_01" {
		t.Errorf("ToolCallID = %q, want %q", toolEvents[0].ToolCallID, "toolu_01")
	}
	if toolEvents[0].ToolName != "list_services" {
		t.Errorf("ToolName = %q, want %q", toolEvents[0].ToolName, "list_services")
	}

	if len(comp.ToolCalls) != 1 {
		t.Fatalf("Completion.ToolCalls = %d, want 1", len(comp.ToolCalls))
	}
	tc := comp.ToolCalls[0]
	if tc.ID != "toolu_01" {
		t.Errorf("ToolCall.ID = %q, want %q", tc.ID, "toolu_01")
	}
	if tc.Name != "list_services" {
		t.Errorf("ToolCall.Name = %q, want %q", tc.Name, "list_services")
	}
	if v, ok := tc.Arguments["limit"]; !ok || v != float64(5) {
		t.Errorf("ToolCall.Arguments[limit] = %v, want 5", v)
	}
}
