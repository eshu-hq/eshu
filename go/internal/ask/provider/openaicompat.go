package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// openAICompatAdapter implements Adapter for any OpenAI-compatible
// chat/completions endpoint. A single instance of this adapter covers
// MiniMax, DeepSeek, OpenAI, Azure OpenAI, Gemini-compat, Ollama, and
// internal gateways that expose the same wire contract.
//
// The caller is responsible for supplying the correct baseURL for each
// provider; no default base URL is assumed.
type openAICompatAdapter struct {
	t       *transport
	baseURL string
	apiKey  string
	model   string
}

// newOpenAICompatAdapter returns an openAICompatAdapter configured with the
// given baseURL, apiKey, and model. client is the HTTP doer injected at the
// transport layer; passing nil causes the transport to use a default
// *http.Client with a 60-second timeout. No default baseURL is applied: the
// caller must supply the full base URL for the target provider.
func newOpenAICompatAdapter(baseURL, apiKey, model string, client httpDoer) *openAICompatAdapter {
	return &openAICompatAdapter{
		t:       newTransport(client),
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
	}
}

// ModelID returns the model identifier used by this adapter.
func (a *openAICompatAdapter) ModelID() string {
	return a.model
}

// Complete sends messages and tool definitions to the OpenAI-compatible
// chat/completions endpoint at baseURL/v1/chat/completions and returns a
// provider-neutral Completion.
//
// Role mapping:
//   - RoleSystem   → role "system"
//   - RoleUser     → role "user"
//   - RoleAssistant → role "assistant"
//   - RoleTool     → role "tool" with tool_call_id set from Message.ToolCallID
//
// Tools are only included in the request when len(tools) > 0; tool_choice is
// set to "auto" only when tools are present.
//
// Each tool_call in the response carries arguments as a raw JSON string.
// Complete parses each arguments string into a map[string]any; a malformed
// string causes Complete to return an error rather than panic.
func (a *openAICompatAdapter) Complete(ctx context.Context, messages []Message, tools []Tool) (Completion, error) {
	req := a.buildRequest(messages, tools)

	// Omit the Authorization header when the resolved credential is empty (for
	// example a cloud_workload_identity profile) so ambient identity or sidecar
	// auth can apply instead of an explicit blank bearer token that gateways and
	// Azure/Gemini-compatible endpoints may reject.
	headers := map[string]string{}
	if a.apiKey != "" {
		headers["Authorization"] = "Bearer " + a.apiKey
	}

	var resp openAICompatResponse
	if err := a.t.postJSON(ctx, a.baseURL+"/v1/chat/completions", headers, req, &resp); err != nil {
		return Completion{}, err
	}

	return mapOpenAICompatResponse(resp)
}

// CompleteStream sends messages and tools to the OpenAI-compatible
// chat/completions endpoint with stream=true and calls emit for each token
// delta and tool-call-started event as the provider yields them. It returns
// the fully assembled Completion once the stream ends with [DONE].
//
// Leak safety: emit receives only bounded TextDelta strings and tool
// identifiers. Raw SSE lines, provider error bodies, and credentials are never
// passed to emit.
func (a *openAICompatAdapter) CompleteStream(ctx context.Context, messages []Message, tools []Tool, emit func(StreamEvent)) (Completion, error) {
	req := a.buildRequest(messages, tools)
	req.Stream = true

	headers := map[string]string{}
	if a.apiKey != "" {
		headers["Authorization"] = "Bearer " + a.apiKey
	}

	body, err := a.t.postJSONStream(ctx, a.baseURL+"/v1/chat/completions", headers, req)
	if err != nil {
		return Completion{}, err
	}
	defer drainAndClose(body)

	return parseOpenAICompatStream(body, emit)
}

// parseOpenAICompatStream reads the OpenAI-compatible SSE stream, emitting
// events for each token delta and tool-call start. It returns the assembled
// Completion when the stream ends with [DONE]. Unexported so tests can inject
// mock bodies directly.
//
// Wire format per line: "data: <json>" or "data: [DONE]".
// Delta shape: {"choices":[{"delta":{"content":"...","tool_calls":[...]}}]}.
func parseOpenAICompatStream(body interface {
	Read([]byte) (int, error)
}, emit func(StreamEvent)) (Completion, error) {
	type deltaToolCallFunction struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	type deltaToolCall struct {
		Index    int                   `json:"index"`
		ID       string                `json:"id"`
		Type     string                `json:"type"`
		Function deltaToolCallFunction `json:"function"`
	}
	type deltaMessage struct {
		Role      string          `json:"role"`
		Content   *string         `json:"content"`
		ToolCalls []deltaToolCall `json:"tool_calls"`
	}
	type deltaChoice struct {
		Delta        deltaMessage `json:"delta"`
		FinishReason string       `json:"finish_reason"`
	}
	type streamChunk struct {
		Choices []deltaChoice      `json:"choices"`
		Usage   *openAICompatUsage `json:"usage"`
	}

	var (
		textBuf      strings.Builder
		toolCallMap  = make(map[int]*openAICompatRequestToolCall)
		toolCallIdx  []int
		finishReason string
		usage        openAICompatUsage
	)

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			// Skip malformed lines; the stream may have keep-alive comments.
			continue
		}

		if chunk.Usage != nil {
			usage = *chunk.Usage
		}

		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.FinishReason != "" {
			finishReason = choice.FinishReason
		}
		delta := choice.Delta

		// Text delta.
		if delta.Content != nil && *delta.Content != "" {
			emit(StreamEvent{Kind: StreamEventToken, TextDelta: *delta.Content})
			textBuf.WriteString(*delta.Content)
		}

		// Tool-call deltas: accumulate by index. The first chunk with a non-empty
		// ID and Name signals the start of a new tool call.
		for _, tc := range delta.ToolCalls {
			if _, seen := toolCallMap[tc.Index]; !seen {
				toolCallMap[tc.Index] = &openAICompatRequestToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: openAICompatToolCallFunction{
						Name:      tc.Function.Name,
						Arguments: "",
					},
				}
				toolCallIdx = append(toolCallIdx, tc.Index)
				if tc.ID != "" && tc.Function.Name != "" {
					emit(StreamEvent{
						Kind:       StreamEventToolCallStarted,
						ToolCallID: tc.ID,
						ToolName:   tc.Function.Name,
					})
				}
			} else {
				// Update fields that arrive in later chunks.
				entry := toolCallMap[tc.Index]
				if tc.ID != "" {
					entry.ID = tc.ID
				}
				if tc.Function.Name != "" {
					entry.Function.Name = tc.Function.Name
				}
				// Arguments arrive as partial JSON strings; concatenate.
				entry.Function.Arguments += tc.Function.Arguments
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Completion{}, fmt.Errorf("ask/provider: openai-compat stream read: %w", err)
	}

	// Assemble the final completion from accumulated state.
	comp := Completion{
		Text:       textBuf.String(),
		StopReason: finishReason,
		Usage: TokenUsage{
			InputTokens:  usage.PromptTokens,
			OutputTokens: usage.CompletionTokens,
		},
	}
	for _, idx := range toolCallIdx {
		tc := toolCallMap[idx]
		var args map[string]any
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				return Completion{}, fmt.Errorf("ask/provider: openai-compat stream: tool call %q has malformed arguments: %w", tc.ID, err)
			}
		}
		comp.ToolCalls = append(comp.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}
	return comp, nil
}

// buildRequest constructs the OpenAI-compatible request body from
// provider-neutral messages and tools.
func (a *openAICompatAdapter) buildRequest(messages []Message, tools []Tool) openAICompatRequest {
	req := openAICompatRequest{
		Model: a.model,
	}

	for _, m := range messages {
		switch m.Role {
		case RoleTool:
			req.Messages = append(req.Messages, openAICompatMessage{
				Role:       "tool",
				Content:    m.Text,
				ToolCallID: m.ToolCallID,
			})
		case RoleSystem:
			req.Messages = append(req.Messages, openAICompatMessage{
				Role:    "system",
				Content: m.Text,
			})
		case RoleUser:
			req.Messages = append(req.Messages, openAICompatMessage{
				Role:    "user",
				Content: m.Text,
			})
		case RoleAssistant:
			msg := openAICompatMessage{
				Role:    "assistant",
				Content: m.Text,
			}
			// Encode prior tool calls into the assistant message so the API can
			// match the subsequent role "tool" result messages by tool_call_id.
			for _, tc := range m.ToolCalls {
				argsJSON, err := json.Marshal(tc.Arguments)
				if err != nil {
					// Arguments came from a prior parsed response; marshalling back to
					// JSON should not fail. If it does, fall back to an empty object.
					argsJSON = []byte("{}")
				}
				msg.ToolCalls = append(msg.ToolCalls, openAICompatRequestToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: openAICompatToolCallFunction{
						Name:      tc.Name,
						Arguments: string(argsJSON),
					},
				})
			}
			req.Messages = append(req.Messages, msg)
		}
	}

	if len(tools) > 0 {
		for _, tool := range tools {
			req.Tools = append(req.Tools, openAICompatToolDef{
				Type: "function",
				Function: openAICompatToolFunctionDef{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.InputSchema,
				},
			})
		}
		req.ToolChoice = "auto"
	}

	return req
}

// mapOpenAICompatResponse converts an OpenAI-compatible response into a
// provider-neutral Completion. It returns an error when choices is empty or
// any tool_call's arguments string is not valid JSON.
func mapOpenAICompatResponse(resp openAICompatResponse) (Completion, error) {
	if len(resp.Choices) == 0 {
		return Completion{}, fmt.Errorf("ask/provider: openai-compat: response contained no choices")
	}

	choice := resp.Choices[0]
	msg := choice.Message

	comp := Completion{
		StopReason: choice.FinishReason,
		Usage: TokenUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		},
	}

	if msg.Content != nil {
		comp.Text = *msg.Content
	}

	for _, tc := range msg.ToolCalls {
		var args map[string]any
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			return Completion{}, fmt.Errorf("ask/provider: openai-compat: tool call %q has malformed arguments: %w", tc.ID, err)
		}
		comp.ToolCalls = append(comp.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	return comp, nil
}
