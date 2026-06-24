// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package provider

// openAICompatRequest is the JSON body sent to an OpenAI-compatible
// chat/completions endpoint (OpenAI, MiniMax, DeepSeek, Azure OpenAI,
// Gemini-compat, Ollama, and internal gateways).
type openAICompatRequest struct {
	Model      string                `json:"model"`
	Messages   []openAICompatMessage `json:"messages"`
	Tools      []openAICompatToolDef `json:"tools,omitempty"`
	ToolChoice string                `json:"tool_choice,omitempty"`
	// Stream, when true, causes the provider to emit SSE data: chunks
	// rather than a single JSON response. CompleteStream sets this to true.
	Stream bool `json:"stream,omitempty"`
}

// openAICompatRequestToolCall represents a tool call in an outgoing assistant
// request message, mirroring the response tool_call wire shape.
// Arguments is a JSON string (not an object) as required by the OpenAI wire format.
type openAICompatRequestToolCall struct {
	ID       string                       `json:"id"`
	Type     string                       `json:"type"`
	Function openAICompatToolCallFunction `json:"function"`
}

// openAICompatMessage is a single turn in an OpenAI-compatible conversation.
// ToolCallID is only set for role "tool" messages.
// ToolCalls is set on role "assistant" messages when the assistant previously
// issued tool calls; it must be present before the corresponding role "tool"
// result messages or the API returns HTTP 400.
type openAICompatMessage struct {
	Role       string                        `json:"role"`
	Content    any                           `json:"content"`
	ToolCallID string                        `json:"tool_call_id,omitempty"`
	ToolCalls  []openAICompatRequestToolCall `json:"tool_calls,omitempty"`
}

// openAICompatToolDef describes a callable function tool in the request.
type openAICompatToolDef struct {
	Type     string                      `json:"type"`
	Function openAICompatToolFunctionDef `json:"function"`
}

// openAICompatToolFunctionDef holds the name, description, and parameter
// schema for a function tool. Parameters is a JSON-schema object carried
// verbatim from the provider-neutral Tool.InputSchema.
type openAICompatToolFunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// openAICompatResponse is the JSON response returned by an OpenAI-compatible
// chat/completions endpoint.
type openAICompatResponse struct {
	Choices []openAICompatChoice `json:"choices"`
	Usage   openAICompatUsage    `json:"usage"`
}

// openAICompatChoice is one candidate completion within the response.
type openAICompatChoice struct {
	Message      openAICompatResponseMessage `json:"message"`
	FinishReason string                      `json:"finish_reason"`
}

// openAICompatResponseMessage holds the model's reply content and any tool
// calls the model requested.
type openAICompatResponseMessage struct {
	// Content may be null in the wire JSON when the model only issues tool
	// calls, so it is decoded as a pointer to distinguish null from "".
	Content   *string                `json:"content"`
	ToolCalls []openAICompatToolCall `json:"tool_calls"`
}

// openAICompatToolCall is a single function invocation within a response.
// Arguments is a JSON string that the caller must parse into a map.
type openAICompatToolCall struct {
	ID       string                       `json:"id"`
	Type     string                       `json:"type"`
	Function openAICompatToolCallFunction `json:"function"`
}

// openAICompatToolCallFunction carries the name and arguments for one tool
// call. Arguments is a raw JSON string (not an object) as specified by the
// OpenAI wire format.
type openAICompatToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// openAICompatUsage holds the token counts returned by the provider.
type openAICompatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
