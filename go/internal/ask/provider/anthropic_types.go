// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package provider

// anthropicRequest is the JSON body sent to the Anthropic Messages API.
// System text is top-level; non-system messages use the messages array.
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
}

// anthropicMessage represents a single turn in the Anthropic conversation.
// Content is a slice of typed blocks (text, tool_use, tool_result).
type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

// anthropicContentBlock is one element of a message's content array.
// The set of populated fields depends on Type:
//   - "text": Text is set.
//   - "tool_use": ID, Name, and Input are set.
//   - "tool_result": ToolUseID and Content are set.
type anthropicContentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
}

// anthropicTool defines one callable tool in the Anthropic request.
// InputSchema is the JSON schema passed verbatim from the provider-neutral Tool.
type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// anthropicResponse is the JSON response from the Anthropic Messages API.
type anthropicResponse struct {
	Content    []anthropicContentBlock `json:"content"`
	StopReason string                  `json:"stop_reason"`
	Usage      anthropicUsage          `json:"usage"`
}

// anthropicUsage holds token counts returned in the Anthropic response.
type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
