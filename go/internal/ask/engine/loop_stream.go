// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
	var progress evidenceProgress
	noProgressStreak := 0

	for i := 0; i < e.opts.MaxIterations; i++ {
		comp, err := sa.CompleteStream(ctx, messages, e.tools, func(ev provider.StreamEvent) {
			switch ev.Kind {
			case provider.StreamEventToken:
				// Drop raw provider deltas. They are pre-validation and may
				// contain uncited claims or publish-unsafe material.
			case provider.StreamEventToolCallStarted:
				// The executed call may be deterministically routed after the
				// completion returns. Emit the executed identity below instead.
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
			if finalizeIndexedRepositoryCountAnswer(question, &ans) {
				ans.TerminationReason = terminationDeterministicRoute
				return ans, nil
			}
			e.finalizeAnswerWithPosture(ctx, question, &ans, terminationFinalTurn, posture)
			emitValidatedNarration(ans, emit)
			return ans, nil
		}

		calls := routeIndexedRepositoryCountCalls(question, comp.ToolCalls)

		// Replay: append the assistant message carrying the tool calls.
		messages = append(messages, provider.Message{
			Role:      provider.RoleAssistant,
			Text:      comp.Text,
			ToolCalls: calls,
		})

		if len(calls) > e.opts.MaxToolCallsPerTurn {
			ans.Limitations = appendLimitation(ans.Limitations,
				fmt.Sprintf("tool calls truncated to %d per turn", e.opts.MaxToolCallsPerTurn))
			calls = calls[:e.opts.MaxToolCallsPerTurn]
			messages[len(messages)-1].ToolCalls = calls
		}

		for _, call := range calls {
			emit(StreamEvent{
				Kind:       KindToolCallStarted,
				ToolCallID: call.ID,
				ToolName:   call.Name,
			})
			messages = e.dispatchCallStream(ctx, question, call, messages, &ans, emit)
		}

		// Evidence-sufficiency stop: mirror the synchronous loop so the SSE
		// surface terminates on sufficiency rather than running to the bound.
		made, haveEvidence := progress.observe(ans.Packets)
		switch {
		case !haveEvidence:
			// No answer evidence yet; keep gathering.
		case made:
			noProgressStreak = 0
		default:
			noProgressStreak++
		}
		if haveEvidence && noProgressStreak >= sufficiencyNoProgressTurns {
			if finalizeIndexedRepositoryCountAnswer(question, &ans) {
				ans.TerminationReason = terminationDeterministicRoute
				return ans, nil
			}
			e.log().Info("ask: evidence sufficiency stop",
				"iteration", i+1,
				"max_iterations", e.opts.MaxIterations,
				"no_progress_turns", noProgressStreak,
				"packets", len(ans.Packets))
			e.finalizeAnswerWithPosture(ctx, question, &ans, terminationEvidenceSufficient, posture)
			emitValidatedNarration(ans, emit)
			return ans, nil
		}
	}

	// Loop exhausted MaxIterations without a final text turn.
	ans.Partial = true
	ans.Limitations = appendLimitation(ans.Limitations, "reached max reasoning iterations")
	e.log().Warn("ask: reached max reasoning iterations",
		"max_iterations", e.opts.MaxIterations,
		"max_tool_calls_per_turn", e.opts.MaxToolCallsPerTurn,
		"packets", len(ans.Packets),
		"has_supported_evidence", bestPacketSummary(ans.Packets) != "")
	if finalizeIndexedRepositoryCountAnswer(question, &ans) {
		ans.TerminationReason = terminationDeterministicRoute
		return ans, nil
	}
	e.finalizeAnswerWithPosture(ctx, question, &ans, terminationMaxIterations, posture)
	if ans.Prose == "" {
		ans.Limitations = appendLimitation(ans.Limitations, "no supported evidence assembled")
	}
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
	// Pre-dispatch bounding: refuse an unbounded broad list/search call before it
	// can spend the dispatch deadline or the response budget.
	if hint, refused := boundToolCall(call.Name, call.Arguments); refused {
		ans.Limitations = appendLimitation(ans.Limitations, hint)
		te := TraceEntry{
			Tool:       call.Name,
			Args:       call.Arguments,
			Supported:  false,
			TruthClass: query.AnswerTruthUnsupported,
			Err:        "refused: unbounded list/search call",
		}
		ans.Trace = append(ans.Trace, te)
		emit(StreamEvent{Kind: KindTraceEntry, TraceEntry: &te})
		e.log().Warn("ask: refused unbounded list/search call before dispatch",
			"tool", call.Name)
		return append(messages, provider.Message{
			Role:       provider.RoleTool,
			ToolCallID: call.ID,
			ToolName:   call.Name,
			Text:       refusalToolResult(hint),
		})
	}

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
		// Runaway result (over-budget or dispatch-timeout): preserve a bounded
		// continuation packet instead of an opaque unsupported outcome.
		if cont, ok := oversizedContinuationPacket(question, call.Name, res.Envelope); ok {
			ans.Partial = true
			ans.Packets = append(ans.Packets, cont)
			te := TraceEntry{
				Tool:       call.Name,
				Args:       call.Arguments,
				Supported:  false,
				TruthClass: query.AnswerTruthUnsupported,
			}
			ans.Trace = append(ans.Trace, te)
			emit(StreamEvent{Kind: KindTraceEntry, TraceEntry: &te})
			e.log().Warn("ask: tool result runaway; bounded continuation offered",
				"tool", call.Name,
				"code", envelopeErrorCode(res.Envelope))
			return append(messages, provider.Message{
				Role:       provider.RoleTool,
				ToolCallID: call.ID,
				ToolName:   call.Name,
				Text:       continuationToolResult(cont),
			})
		}
		pkt := answerPacketForToolResult(question, call.Name, res.Envelope)
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
