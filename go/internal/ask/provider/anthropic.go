package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// anthropicAdapter implements Adapter for the Anthropic Messages API,
// supporting native tool_use blocks.
type anthropicAdapter struct {
	t         *transport
	baseURL   string
	apiKey    string
	model     string
	maxTokens int
}

// newAnthropicAdapter returns an anthropicAdapter configured for the given
// model and API key. When baseURL is empty it defaults to the Anthropic
// production endpoint. client is the HTTP doer injected at the transport
// layer; passing nil causes the transport to use a default *http.Client.
func newAnthropicAdapter(baseURL, apiKey, model string, client httpDoer) *anthropicAdapter {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &anthropicAdapter{
		t:         newTransport(client),
		baseURL:   baseURL,
		apiKey:    apiKey,
		model:     model,
		maxTokens: 4096,
	}
}

// ModelID returns the Anthropic model identifier used by this adapter.
func (a *anthropicAdapter) ModelID() string {
	return a.model
}

// Complete sends messages and tool definitions to the Anthropic Messages API
// and returns a provider-neutral Completion. System-role messages are
// concatenated into the top-level "system" field rather than a message entry.
// Tool-result messages (RoleTool) become user messages containing a
// tool_result content block keyed by ToolCallID.
func (a *anthropicAdapter) Complete(ctx context.Context, messages []Message, tools []Tool) (Completion, error) {
	req := a.buildRequest(messages, tools)

	// Omit x-api-key when the resolved credential is empty (for example a
	// cloud_workload_identity profile) so ambient or sidecar auth can apply
	// instead of an explicit blank key the provider would reject.
	headers := map[string]string{
		"anthropic-version": "2023-06-01",
	}
	if a.apiKey != "" {
		headers["x-api-key"] = a.apiKey
	}

	var resp anthropicResponse
	if err := a.t.postJSON(ctx, a.baseURL+"/v1/messages", headers, req, &resp); err != nil {
		return Completion{}, err
	}

	return mapResponse(resp), nil
}

// CompleteStream sends messages and tools to the Anthropic Messages API with
// stream=true and calls emit for each token delta and tool-call-started event
// as the provider yields them. It returns the fully assembled Completion once
// the stream ends.
//
// The Anthropic streaming protocol uses alternating "event:" and "data:" lines.
// Token deltas arrive as content_block_delta events with delta.type="text_delta".
// Tool calls arrive as content_block_start events with block.type="tool_use",
// followed by content_block_delta events with delta.type="input_json_delta".
//
// Leak safety: emit receives only bounded TextDelta and tool identifier fields.
// Raw event bodies, API keys, and error text are never passed to emit.
func (a *anthropicAdapter) CompleteStream(ctx context.Context, messages []Message, tools []Tool, emit func(StreamEvent)) (Completion, error) {
	req := a.buildStreamRequest(messages, tools)

	headers := map[string]string{
		"anthropic-version": "2023-06-01",
	}
	if a.apiKey != "" {
		headers["x-api-key"] = a.apiKey
	}

	body, err := a.t.postJSONStream(ctx, a.baseURL+"/v1/messages", headers, req)
	if err != nil {
		return Completion{}, err
	}
	defer drainAndClose(body)

	return parseAnthropicStream(body, emit)
}

// buildStreamRequest constructs an Anthropic request with stream=true.
func (a *anthropicAdapter) buildStreamRequest(messages []Message, tools []Tool) anthropicStreamRequest {
	base := a.buildRequest(messages, tools)
	return anthropicStreamRequest{
		anthropicRequest: base,
		Stream:           true,
	}
}

// anthropicStreamRequest extends anthropicRequest with the stream field.
type anthropicStreamRequest struct {
	anthropicRequest
	Stream bool `json:"stream"`
}

// parseAnthropicStream reads the Anthropic SSE stream, emitting events for
// each text delta and tool-call start. It returns the assembled Completion
// when the stream ends with a message_stop event.
//
// The Anthropic streaming format pairs "event: <type>" with "data: <json>":
//   - event: content_block_start → new block (text or tool_use)
//   - event: content_block_delta → incremental delta for current block
//   - event: message_delta       → stop_reason and usage
//   - event: message_stop        → end of stream
func parseAnthropicStream(body interface {
	Read([]byte) (int, error)
}, emit func(StreamEvent),
) (Completion, error) {
	type streamDelta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
	}
	type streamBlock struct {
		Type  string         `json:"type"`
		Index int            `json:"index"`
		ID    string         `json:"id"`
		Name  string         `json:"name"`
		Input map[string]any `json:"input"`
		Text  string         `json:"text"`
	}
	type deltaEvent struct {
		Type         string      `json:"type"`
		Delta        streamDelta `json:"delta"`
		Index        int         `json:"index"`
		ContentBlock streamBlock `json:"content_block"`
	}
	type messageDeltaUsage struct {
		OutputTokens int `json:"output_tokens"`
	}
	type messageDeltaData struct {
		Delta struct {
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
		Usage messageDeltaUsage `json:"usage"`
	}
	type messageStartData struct {
		Message struct {
			Usage anthropicUsage `json:"usage"`
		} `json:"message"`
	}

	var (
		textBuf      strings.Builder
		stopReason   string
		inputTokens  int
		outputTokens int
		// blockIndex → accumulated tool-call metadata.
		toolByIndex = make(map[int]*ToolCall)
		// blockIndex → accumulated partial input JSON string.
		toolInputBuf = make(map[int]*strings.Builder)
		// preserve insertion order.
		toolOrder []int
	)

	scanner := bufio.NewScanner(body)
	var currentEventType string

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			currentEventType = strings.TrimPrefix(line, "event: ")
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")

		switch currentEventType {
		case "message_start":
			var ms messageStartData
			if err := json.Unmarshal([]byte(payload), &ms); err == nil {
				inputTokens = ms.Message.Usage.InputTokens
				outputTokens = ms.Message.Usage.OutputTokens
			}

		case "content_block_start":
			var ev deltaEvent
			if err := json.Unmarshal([]byte(payload), &ev); err != nil {
				continue
			}
			if ev.ContentBlock.Type == "tool_use" {
				tc := &ToolCall{
					ID:   ev.ContentBlock.ID,
					Name: ev.ContentBlock.Name,
				}
				toolByIndex[ev.Index] = tc
				toolInputBuf[ev.Index] = &strings.Builder{}
				toolOrder = append(toolOrder, ev.Index)
				if tc.ID != "" && tc.Name != "" {
					emit(StreamEvent{
						Kind:       StreamEventToolCallStarted,
						ToolCallID: tc.ID,
						ToolName:   tc.Name,
					})
				}
			}

		case "content_block_delta":
			var ev deltaEvent
			if err := json.Unmarshal([]byte(payload), &ev); err != nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				if ev.Delta.Text != "" {
					emit(StreamEvent{Kind: StreamEventToken, TextDelta: ev.Delta.Text})
					textBuf.WriteString(ev.Delta.Text)
				}
			case "input_json_delta":
				// Accumulate partial tool input JSON; do not emit (internal detail).
				if buf, ok := toolInputBuf[ev.Index]; ok {
					buf.WriteString(ev.Delta.PartialJSON)
				}
			}

		case "message_delta":
			var md messageDeltaData
			if err := json.Unmarshal([]byte(payload), &md); err == nil {
				if md.Delta.StopReason != "" {
					stopReason = md.Delta.StopReason
				}
				if md.Usage.OutputTokens > 0 {
					outputTokens = md.Usage.OutputTokens
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Completion{}, fmt.Errorf("ask/provider: anthropic stream read: %w", err)
	}

	comp := Completion{
		Text:       textBuf.String(),
		StopReason: stopReason,
		Usage: TokenUsage{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
		},
	}
	for _, idx := range toolOrder {
		tc := toolByIndex[idx]
		if buf, ok := toolInputBuf[idx]; ok && buf.Len() > 0 {
			var args map[string]any
			if err := json.Unmarshal([]byte(buf.String()), &args); err != nil {
				return Completion{}, fmt.Errorf("ask/provider: anthropic stream: tool call %q has malformed input: %w", tc.ID, err)
			}
			tc.Arguments = args
		}
		comp.ToolCalls = append(comp.ToolCalls, *tc)
	}
	return comp, nil
}

// buildRequest constructs the Anthropic API request body from provider-neutral
// messages and tools.
func (a *anthropicAdapter) buildRequest(messages []Message, tools []Tool) anthropicRequest {
	req := anthropicRequest{
		Model:     a.model,
		MaxTokens: a.maxTokens,
	}

	// Collect system messages into the top-level system field.
	var systemParts []string
	for _, m := range messages {
		if m.Role == RoleSystem {
			systemParts = append(systemParts, m.Text)
		}
	}
	if len(systemParts) > 0 {
		req.System = strings.Join(systemParts, "\n")
	}

	// Map non-system messages.
	for _, m := range messages {
		switch m.Role {
		case RoleSystem:
			// Already handled above.
		case RoleTool:
			// Tool results become a user message with a tool_result block.
			req.Messages = append(req.Messages, anthropicMessage{
				Role: "user",
				Content: []anthropicContentBlock{
					{Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Text},
				},
			})
		case RoleUser:
			req.Messages = append(req.Messages, anthropicMessage{
				Role:    "user",
				Content: []anthropicContentBlock{{Type: "text", Text: m.Text}},
			})
		case RoleAssistant:
			var blocks []anthropicContentBlock
			// Include a text block first when the assistant produced text.
			if m.Text != "" {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: m.Text})
			}
			// Encode each prior tool call as a tool_use block so the Anthropic API
			// can match the subsequent tool_result blocks by id.
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, anthropicContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: tc.Arguments,
				})
			}
			// Anthropic requires at least one content block; fall back to an empty
			// text block when the assistant turn carries neither text nor tool calls.
			if len(blocks) == 0 {
				blocks = append(blocks, anthropicContentBlock{Type: "text", Text: ""})
			}
			req.Messages = append(req.Messages, anthropicMessage{
				Role:    "assistant",
				Content: blocks,
			})
		}
	}

	// Map tools to the Anthropic tool definition shape.
	for _, tool := range tools {
		req.Tools = append(req.Tools, anthropicTool(tool))
	}

	return req
}

// mapResponse converts an Anthropic response to a provider-neutral Completion.
// Text blocks are concatenated; tool_use blocks become ToolCall entries.
func mapResponse(resp anthropicResponse) Completion {
	comp := Completion{
		StopReason: resp.StopReason,
		Usage: TokenUsage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
		},
	}

	var textParts []string
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			comp.ToolCalls = append(comp.ToolCalls, ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: block.Input,
			})
		}
	}
	comp.Text = strings.Join(textParts, "")
	return comp
}
