package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// maxToolResultBytes is the size cap for the bounded JSON serialized into a
// tool-result message. When the serialized packet exceeds this threshold the
// engine falls back to a minimal skeleton containing only the fields a model
// needs to reason about the result without re-querying.
const maxToolResultBytes = 4096

// Ask executes the bounded Tier 1 agent loop for the given question.
//
// It builds an initial message thread, drives LLM completions through the
// configured adapter, dispatches tool calls through the Runner, and assembles
// an Answer from the resulting evidence packets. The loop terminates on the
// first completion that carries no tool calls (the model's final turn), or when
// MaxIterations is reached. In the latter case the answer is marked Partial and
// prose is synthesised deterministically from the best supported packet.
//
// Ask is safe for concurrent use: each call owns its own conversation thread.
func (e *Engine) Ask(ctx context.Context, question string) (Answer, error) {
	messages := []provider.Message{
		{Role: provider.RoleSystem, Text: e.opts.SystemPrompt},
		{Role: provider.RoleUser, Text: question},
	}
	ans := Answer{Question: question}

	for i := 0; i < e.opts.MaxIterations; i++ {
		comp, err := e.adapter.Complete(ctx, messages, e.tools)
		if err != nil {
			return Answer{}, fmt.Errorf("ask: provider completion: %w", err)
		}

		ans.Usage.InputTokens += comp.Usage.InputTokens
		ans.Usage.OutputTokens += comp.Usage.OutputTokens

		if len(comp.ToolCalls) == 0 {
			// Final turn: model produced prose with no further tool calls.
			ans.Prose = comp.Text
			return ans, nil
		}

		// Replay: append the assistant message that carries the tool calls so
		// the next completion sees a valid conversation thread.
		messages = append(messages, provider.Message{
			Role:      provider.RoleAssistant,
			Text:      comp.Text,
			ToolCalls: comp.ToolCalls,
		})

		calls := comp.ToolCalls
		if len(calls) > e.opts.MaxToolCallsPerTurn {
			ans.Limitations = appendLimitation(ans.Limitations,
				fmt.Sprintf("tool calls truncated to %d per turn", e.opts.MaxToolCallsPerTurn))
			calls = calls[:e.opts.MaxToolCallsPerTurn]
		}

		for _, call := range calls {
			messages = e.dispatchCall(ctx, question, call, messages, &ans)
		}
	}

	// Loop exhausted MaxIterations without a final text turn.
	ans.Partial = true
	ans.Limitations = appendLimitation(ans.Limitations, "reached max reasoning iterations")
	ans.Prose = bestPacketSummary(ans.Packets)
	if ans.Prose == "" {
		ans.Limitations = appendLimitation(ans.Limitations, "no supported evidence assembled")
	}
	return ans, nil
}

// dispatchCall executes a single tool call, records a TraceEntry, assembles an
// AnswerPacket, and appends a bounded tool-result message to the conversation.
// It returns the updated messages slice.
func (e *Engine) dispatchCall(
	ctx context.Context,
	question string,
	call provider.ToolCall,
	messages []provider.Message,
	ans *Answer,
) []provider.Message {
	env, runErr := e.runner.Run(ctx, call.Name, call.Arguments)
	if runErr != nil {
		ans.Trace = append(ans.Trace, TraceEntry{
			Tool:       call.Name,
			Args:       call.Arguments,
			Supported:  false,
			TruthClass: query.AnswerTruthUnsupported,
			Err:        runErr.Error(),
		})
		messages = append(messages, provider.Message{
			Role:       provider.RoleTool,
			ToolCallID: call.ID,
			ToolName:   call.Name,
			Text:       `{"error":"capability call failed"}`,
		})
		return messages
	}

	pkt := query.NewAnswerPacket(query.AnswerPacketInput{
		Question:    question,
		PrimaryTool: call.Name,
		Envelope:    env,
	})

	ans.Packets = append(ans.Packets, pkt)
	ans.Trace = append(ans.Trace, TraceEntry{
		Tool:       call.Name,
		Args:       call.Arguments,
		Supported:  pkt.Supported,
		TruthClass: pkt.TruthClass,
	})

	messages = append(messages, provider.Message{
		Role:       provider.RoleTool,
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Text:       marshalToolResult(pkt),
	})
	return messages
}

// toolResultSkeleton is the bounded shape serialised into a tool-result message.
// Only prompt-useful fields are included; raw provider or LLM bodies are never
// present.
type toolResultSkeleton struct {
	Summary    string                 `json:"summary,omitempty"`
	TruthClass query.AnswerTruthClass `json:"truth_class"`
	Supported  bool                   `json:"supported"`
	Partial    bool                   `json:"partial"`
}

// marshalToolResult serialises pkt into a bounded JSON string for the
// tool-result message. If the full serialisation exceeds maxToolResultBytes
// only a minimal skeleton is returned.
func marshalToolResult(pkt query.AnswerPacket) string {
	skeleton := toolResultSkeleton{
		Summary:    pkt.Summary,
		TruthClass: pkt.TruthClass,
		Supported:  pkt.Supported,
		Partial:    pkt.Partial,
	}
	b, err := json.Marshal(skeleton)
	if err != nil {
		return `{"supported":false,"truth_class":"unsupported"}`
	}
	if len(b) <= maxToolResultBytes {
		return string(b)
	}
	// Fall back to a minimal skeleton without the (potentially large) summary.
	minimal := toolResultSkeleton{
		TruthClass: pkt.TruthClass,
		Supported:  pkt.Supported,
		Partial:    pkt.Partial,
	}
	mb, err := json.Marshal(minimal)
	if err != nil {
		return `{"supported":false,"truth_class":"unsupported"}`
	}
	return string(mb)
}

// bestPacketSummary returns the Summary of the first supported packet with a
// non-empty Summary. It never fabricates; an empty string means no supported
// evidence was assembled.
func bestPacketSummary(packets []query.AnswerPacket) string {
	for _, p := range packets {
		if p.Supported && p.Summary != "" {
			return p.Summary
		}
	}
	return ""
}

// appendLimitation appends s to limitations when s is not already present.
func appendLimitation(limitations []string, s string) []string {
	for _, existing := range limitations {
		if existing == s {
			return limitations
		}
	}
	return append(limitations, s)
}
