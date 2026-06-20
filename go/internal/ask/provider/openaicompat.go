package provider

import (
	"context"
	"encoding/json"
	"fmt"
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

	headers := map[string]string{
		"Authorization": "Bearer " + a.apiKey,
	}

	var resp openAICompatResponse
	if err := a.t.postJSON(ctx, a.baseURL+"/v1/chat/completions", headers, req, &resp); err != nil {
		return Completion{}, err
	}

	return mapOpenAICompatResponse(resp)
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
			req.Messages = append(req.Messages, openAICompatMessage{
				Role:    "assistant",
				Content: m.Text,
			})
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
