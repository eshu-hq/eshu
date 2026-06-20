package engine

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// StreamEvent is the handler-layer streaming event emitted by AskStream.
// It mirrors provider.StreamEvent but lives here so the query package can
// consume engine stream events without importing the provider package.
//
// Leak safety: only validated narration text and bounded tool identifiers are
// ever included. Provider error bodies, credentials, prompts, and raw LLM
// frames are never present.
type StreamEvent struct {
	// Kind identifies the event class.
	Kind StreamEventKind
	// TextDelta is validated narration prose for KindToken events.
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
	// KindToken carries validated narration prose.
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
//   - KindToken once with validated narration prose when narration succeeds
//   - KindToolCallStarted when the model requests a tool call
//   - KindTraceEntry after each tool call completes (with bounded truth metadata)
//
// It returns the same Answer that Ask would return. Provider token deltas are
// never emitted directly because they precede citation and publish-safety
// validation. The narration step uses the synchronous Complete call; when that
// validated narration succeeds, AskStream emits the validated prose as one token
// event before returning the final Answer.
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
	// the final narration decision so the stream and answer agree for this
	// request. Narration is default-closed, and raw provider prose is never
	// emitted as token events because it has not passed citation or publish-safety
	// validation yet. Tool-lifecycle events stream regardless.
	posture := e.resolveNarrationPosture()

	messages := []provider.Message{
		{Role: provider.RoleSystem, Text: e.opts.SystemPrompt},
		{Role: provider.RoleUser, Text: question},
	}
	ans := Answer{Question: question}

	for i := 0; i < e.opts.MaxIterations; i++ {
		comp, err := sa.CompleteStream(ctx, messages, e.tools, func(ev provider.StreamEvent) {
			switch ev.Kind {
			case provider.StreamEventToken:
				// Drop raw provider deltas. They are pre-validation and may
				// contain uncited claims or publish-unsafe material.
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
			emitValidatedNarration(ans, emit)
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
	emitValidatedNarration(ans, emit)
	return ans, nil
}

// emitValidatedNarration streams only prose that has passed the governed
// narration validator. It intentionally emits a single delta rather than raw
// provider chunks so SSE clients never see pre-validation LLM output.
func emitValidatedNarration(ans Answer, emit func(StreamEvent)) {
	if ans.Narrated && ans.Prose != "" {
		emit(StreamEvent{Kind: KindToken, TextDelta: ans.Prose})
	}
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
