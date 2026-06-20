package engine

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// StreamEvent is the handler-layer streaming event emitted by AskStream.
// It mirrors provider.StreamEvent but lives here so the query package can
// consume engine stream events without importing the provider package.
//
// Leak safety: only bounded assistant text deltas and tool identifiers are
// ever included. Provider error bodies, credentials, and raw LLM frames are
// never present.
type StreamEvent struct {
	// Kind identifies the event class.
	Kind StreamEventKind
	// TextDelta is the incremental assistant text for KindToken events.
	TextDelta string
	// ToolCallID is the provider-assigned ID for KindToolCallStarted events.
	ToolCallID string
	// ToolName is the tool name for KindToolCallStarted events.
	ToolName string
	// TraceEntry is set for KindTraceEntry events, carrying the completed
	// tool-call result after dispatching a tool.
	TraceEntry *TraceEntry
}

// StreamEventKind classifies a StreamEvent.
type StreamEventKind string

const (
	// KindToken carries a provider token delta (assistant text).
	KindToken StreamEventKind = "token"
	// KindToolCallStarted signals that the model has requested a tool call.
	KindToolCallStarted StreamEventKind = "tool_call_started"
	// KindTraceEntry signals that a tool call has completed with a result.
	KindTraceEntry StreamEventKind = "trace_entry"
)

// StreamingRunner is a Runner whose adapter supports streaming completions.
// AskStream requires the engine's adapter to implement provider.StreamingAdapter.
// If the adapter does not implement streaming, AskStream returns ErrNoStreaming.
var ErrNoStreaming = fmt.Errorf("ask/engine: adapter does not support streaming; use Ask instead")

// AskStream executes the bounded Tier 1 agent loop for the given question,
// emitting streaming events to emit as they occur:
//
//   - KindToken per provider token delta (assistant prose)
//   - KindToolCallStarted when the model requests a tool call
//   - KindTraceEntry after each tool call completes (with bounded truth metadata)
//
// It returns the same Answer that Ask would return. The narration step uses the
// synchronous Complete call (narration completions are short and not streamed to
// avoid leaking the narration prompt structure as token events).
//
// AskStream returns ErrNoStreaming if the configured adapter does not implement
// provider.StreamingAdapter. In that case callers should fall back to Ask.
//
// AskStream is safe for concurrent use: each call owns its own conversation
// thread.
func (e *Engine) AskStream(ctx context.Context, question string, emit func(StreamEvent)) (Answer, error) {
	sa, ok := e.adapter.(provider.StreamingAdapter)
	if !ok {
		return Answer{}, ErrNoStreaming
	}

	// Resolve the governed narration posture once, up front, and reuse it for
	// both the prose-token gate and the final narration decision so the two
	// agree for this request. Narration is default-closed: streaming raw
	// provider prose token-by-token would bypass that gate, because the JSON
	// path suppresses answer_prose when Narrated is false. So prose token deltas
	// are emitted only when narration is available; tool-lifecycle events stream
	// regardless, and the governed prose is delivered once in the final answer.
	posture := e.resolveNarrationPosture()
	narrationAllowed := posture.State == status.AnswerNarrationAvailable

	messages := []provider.Message{
		{Role: provider.RoleSystem, Text: e.opts.SystemPrompt},
		{Role: provider.RoleUser, Text: question},
	}
	ans := Answer{Question: question}

	for i := 0; i < e.opts.MaxIterations; i++ {
		comp, err := sa.CompleteStream(ctx, messages, e.tools, func(ev provider.StreamEvent) {
			switch ev.Kind {
			case provider.StreamEventToken:
				if narrationAllowed {
					emit(StreamEvent{Kind: KindToken, TextDelta: ev.TextDelta})
				}
			case provider.StreamEventToolCallStarted:
				emit(StreamEvent{
					Kind:       KindToolCallStarted,
					ToolCallID: ev.ToolCallID,
					ToolName:   ev.ToolName,
				})
			}
		})
		if err != nil {
			return Answer{}, fmt.Errorf("ask: provider stream completion: %w", err)
		}

		ans.Usage.InputTokens += comp.Usage.InputTokens
		ans.Usage.OutputTokens += comp.Usage.OutputTokens

		if len(comp.ToolCalls) == 0 {
			// Final turn: model produced prose with no further tool calls.
			ans.Prose = comp.Text
			e.narrate(ctx, &ans, posture)
			return ans, nil
		}

		// Replay: append the assistant message carrying the tool calls.
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
			messages[len(messages)-1].ToolCalls = calls
		}

		for _, call := range calls {
			messages = e.dispatchCallStream(ctx, question, call, messages, &ans, emit)
		}
	}

	// Loop exhausted MaxIterations without a final text turn.
	ans.Partial = true
	ans.Limitations = appendLimitation(ans.Limitations, "reached max reasoning iterations")
	ans.Prose = bestPacketSummary(ans.Packets)
	if ans.Prose == "" {
		ans.Limitations = appendLimitation(ans.Limitations, "no supported evidence assembled")
	}
	e.narrate(ctx, &ans, posture)
	return ans, nil
}

// dispatchCallStream is the streaming variant of dispatchCall: it executes a
// tool call, records a TraceEntry, emits a KindTraceEntry event, and appends a
// bounded tool-result message to the conversation. It follows the same four
// outcome branches as dispatchCall.
func (e *Engine) dispatchCallStream(
	ctx context.Context,
	question string,
	call provider.ToolCall,
	messages []provider.Message,
	ans *Answer,
	emit func(StreamEvent),
) []provider.Message {
	res, runErr := e.runner.Run(ctx, call.Name, call.Arguments)
	if runErr != nil {
		te := TraceEntry{
			Tool:       call.Name,
			Args:       call.Arguments,
			Supported:  false,
			TruthClass: query.AnswerTruthUnsupported,
			Err:        runErr.Error(),
		}
		ans.Trace = append(ans.Trace, te)
		emit(StreamEvent{Kind: KindTraceEntry, TraceEntry: &te})
		messages = append(messages, provider.Message{
			Role:       provider.RoleTool,
			ToolCallID: call.ID,
			ToolName:   call.Name,
			Text:       `{"error":"capability call failed"}`,
		})
		return messages
	}

	if res.Envelope != nil {
		pkt, ok := extractEmbeddedPacket(res.Envelope)
		if !ok {
			pkt = query.NewAnswerPacket(query.AnswerPacketInput{
				Question:    question,
				PrimaryTool: call.Name,
				Envelope:    res.Envelope,
			})
		}
		if pkt.Partial {
			ans.Partial = true
		}
		ans.Packets = append(ans.Packets, pkt)
		te := TraceEntry{
			Tool:       call.Name,
			Args:       call.Arguments,
			Supported:  pkt.Supported,
			TruthClass: pkt.TruthClass,
		}
		ans.Trace = append(ans.Trace, te)
		emit(StreamEvent{Kind: KindTraceEntry, TraceEntry: &te})
		messages = append(messages, provider.Message{
			Role:       provider.RoleTool,
			ToolCallID: call.ID,
			ToolName:   call.Name,
			Text:       marshalToolResult(pkt),
		})
		return messages
	}

	if res.Value != nil {
		te := TraceEntry{
			Tool:      call.Name,
			Args:      call.Arguments,
			Supported: true,
		}
		ans.Trace = append(ans.Trace, te)
		emit(StreamEvent{Kind: KindTraceEntry, TraceEntry: &te})
		messages = append(messages, provider.Message{
			Role:       provider.RoleTool,
			ToolCallID: call.ID,
			ToolName:   call.Name,
			Text:       marshalPlainValue(res.Value),
		})
		return messages
	}

	// Both Envelope and Value are nil: empty or failed result.
	te := TraceEntry{
		Tool:       call.Name,
		Args:       call.Arguments,
		Supported:  false,
		TruthClass: query.AnswerTruthUnsupported,
	}
	ans.Trace = append(ans.Trace, te)
	emit(StreamEvent{Kind: KindTraceEntry, TraceEntry: &te})
	messages = append(messages, provider.Message{
		Role:       provider.RoleTool,
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Text:       `{"supported":false,"truth_class":"unsupported"}`,
	})
	return messages
}
