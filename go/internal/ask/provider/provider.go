package provider

import "context"

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
