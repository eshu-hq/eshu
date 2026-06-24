// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package provider

import "context"

// StreamEventKind classifies a streaming event emitted during CompleteStream.
type StreamEventKind string

const (
	// StreamEventToken carries a partial text delta from the assistant's output.
	StreamEventToken StreamEventKind = "token"
	// StreamEventToolCallStarted signals that the model has begun a tool call.
	// The ToolCallID and ToolName fields identify the call; Arguments accumulate
	// as subsequent events arrive and the final map is populated in the Completion
	// returned by CompleteStream.
	StreamEventToolCallStarted StreamEventKind = "tool_call_started"
)

// StreamEvent is one event emitted by a streaming completion. Only the fields
// relevant to the Kind are populated; all others are zero values.
//
// Leak safety: StreamEvent carries only assistant text deltas and bounded tool
// metadata. Provider error bodies, API keys, and raw HTTP frames are never
// included.
type StreamEvent struct {
	// Kind identifies the event type.
	Kind StreamEventKind
	// TextDelta is the incremental text content for StreamEventToken events.
	TextDelta string
	// ToolCallID is the provider-assigned ID for StreamEventToolCallStarted events.
	ToolCallID string
	// ToolName is the tool name for StreamEventToolCallStarted events.
	ToolName string
}

// StreamingAdapter extends Adapter with a streaming completion seam.
// Implementations must remain safe for concurrent use: a single StreamingAdapter
// may serve multiple concurrent CompleteStream calls.
type StreamingAdapter interface {
	Adapter

	// CompleteStream drives a streaming completion, calling emit for each
	// StreamEvent as the provider yields tokens. It returns the fully assembled
	// Completion (identical to what Complete would return) so callers can feed
	// the conversation without re-parsing the stream. emit is called
	// synchronously on the calling goroutine; implementations must not retain
	// references to the StreamEvent after emit returns.
	//
	// emit must not block for more than a brief moment; a slow consumer will
	// slow the stream read. Implementations that need backpressure should buffer
	// at the caller level.
	CompleteStream(ctx context.Context, messages []Message, tools []Tool, emit func(StreamEvent)) (Completion, error)
}

// Role identifies the participant in a conversation.
type Role string

// Standard roles for conversation participants.
const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is a flat, provider-neutral conversation message.
// Text holds the message content. For tool-result messages, ToolCallID and
// ToolName identify the prior tool call being responded to.
// ToolCalls is set on a RoleAssistant message to carry the tool calls the model
// previously returned, so the agent loop can replay the assistant turn before
// sending tool results. Providers that require an assistant turn with tool_use
// blocks (Anthropic) or tool_calls (OpenAI-compatible) read this field to build
// the correct wire shape.
type Message struct {
	Role       Role
	Text       string
	ToolCallID string
	ToolName   string
	ToolCalls  []ToolCall
}

// Tool describes a callable tool with name, description, and input schema.
// InputSchema is a JSON schema object encoded as map[string]any.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// ToolCall is a single tool call in a completion response.
// ID is the unique identifier for this call; Name is the tool name;
// Arguments contains the parsed input parameters.
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// TokenUsage tracks token consumption for a completion.
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
}

// Total returns the sum of input and output tokens.
func (u TokenUsage) Total() int {
	return u.InputTokens + u.OutputTokens
}

// Completion is the response from a provider completion call.
// Text holds any generated text; ToolCalls holds any tool invocations;
// Usage tracks token consumption; StopReason indicates why generation stopped.
type Completion struct {
	Text       string
	ToolCalls  []ToolCall
	Usage      TokenUsage
	StopReason string
}

// Adapter bridges a specific provider (Anthropic or OpenAI-compatible) to the
// provider-neutral message/tool contract.
type Adapter interface {
	// Complete calls the provider with the given messages and tools,
	// returning a provider-neutral completion or an error.
	Complete(ctx context.Context, messages []Message, tools []Tool) (Completion, error)

	// ModelID returns the model identifier this adapter uses.
	ModelID() string
}
