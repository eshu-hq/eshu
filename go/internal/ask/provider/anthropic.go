package provider

import (
	"context"
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
