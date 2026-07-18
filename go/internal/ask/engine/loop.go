// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/answerguardrail"
	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/status"
)

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
	var progress evidenceProgress
	noProgressStreak := 0

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
			if finalizeIndexedRepositoryCountAnswer(question, &ans) {
				ans.TerminationReason = terminationDeterministicRoute
				return ans, nil
			}
			e.finalizeAnswer(ctx, question, &ans, terminationFinalTurn)
			return ans, nil
		}

		calls := routeIndexedRepositoryCountCalls(question, comp.ToolCalls)

		// Replay: append the assistant message that carries the tool calls so
		// the next completion sees a valid conversation thread.
		messages = append(messages, provider.Message{
			Role:      provider.RoleAssistant,
			Text:      comp.Text,
			ToolCalls: calls,
		})

		if len(calls) > e.opts.MaxToolCallsPerTurn {
			ans.Limitations = appendLimitation(ans.Limitations,
				fmt.Sprintf("tool calls truncated to %d per turn", e.opts.MaxToolCallsPerTurn))
			e.log().Warn("ask: tool calls truncated",
				"requested", len(calls),
				"max_tool_calls_per_turn", e.opts.MaxToolCallsPerTurn,
				"iteration", i)
			calls = calls[:e.opts.MaxToolCallsPerTurn]
			// The assistant message must carry only the dispatched (truncated)
			// tool calls. Replaying all comp.ToolCalls while only providing
			// RoleTool responses for the truncated subset causes the provider to
			// reject the next request due to unmatched IDs.
			messages[len(messages)-1].ToolCalls = calls
		}

		for _, call := range calls {
			messages = e.dispatchCall(ctx, question, call, messages, &ans)
		}

		// Evidence-sufficiency stop: once answer evidence is held and the loop has
		// spun for sufficiencyNoProgressTurns consecutive turns without adding any
		// new distinct supported evidence, continuing would only repeat redundant
		// or oversized calls, so terminate instead of running to the iteration
		// bound.
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
			e.finalizeAnswer(ctx, question, &ans, terminationEvidenceSufficient)
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
	e.finalizeAnswer(ctx, question, &ans, terminationMaxIterations)
	if ans.Prose == "" {
		ans.Limitations = appendLimitation(ans.Limitations, "no supported evidence assembled")
	}
	return ans, nil
}

// finalizeAnswer performs the shared answer-completion steps for every non-error
// loop exit: it records the termination reason, selects the relevance-ranked
// primary packet (unless a deterministic route already bound one), fills
// deterministic prose from that packet when the model produced none, and runs the
// governed narration gate. It is called by both the synchronous and streaming
// loops so their answer assembly stays identical.
func (e *Engine) finalizeAnswer(ctx context.Context, question string, ans *Answer, reason string) {
	e.finalizeAnswerWithPosture(ctx, question, ans, reason, e.resolveNarrationPosture())
}

// finalizeAnswerWithPosture is finalizeAnswer with an explicitly supplied
// narration posture. The streaming loop resolves the posture once up front and
// threads it here so the stream and the returned answer make the same narration
// decision for a request.
func (e *Engine) finalizeAnswerWithPosture(ctx context.Context, question string, ans *Answer, reason string, posture status.AnswerNarrationStatus) {
	ans.TerminationReason = reason
	if ans.PrimaryPacketIndex == nil {
		if idx, selReason := selectPrimaryPacketIndex(question, ans.Packets); idx >= 0 {
			// Only override the historical first-supported fallback when the
			// ranking disagrees, so the handler's own selection is unchanged for
			// the common case and only corrected when relevance demands it.
			if idx != firstSupportedIndex(ans.Packets) {
				bound := idx
				ans.PrimaryPacketIndex = &bound
			}
			e.log().Info("ask: primary packet selected",
				"index", idx,
				"reason", selReason,
				"termination", reason)
		}
	}
	if ans.Prose == "" {
		ans.Prose = selectedPacketSummary(ans)
	}
	e.narrate(ctx, ans, posture)
	// Usefulness verdict telemetry: record whether the published prose is a
	// circular, identity-only restatement of the question so an operator can see
	// the answer-quality outcome without re-running the session. The handler
	// enforces the withholding; this is the observable verdict.
	if strings.TrimSpace(ans.Prose) != "" {
		e.log().Info("ask: usefulness verdict",
			"circular", answerguardrail.IsCircularAnswer(question, ans.Prose),
			"termination", reason)
	}
}

// dispatchCall executes a single tool call, records a TraceEntry, and appends
// a bounded tool-result message to the conversation. It returns the updated
// messages slice.
//
// There are four outcome branches based on RunResult:
//  1. Run error: record a failed trace entry and a bounded error tool-result.
//  2. Envelope non-nil: extract or build an AnswerPacket, append it to
//     ans.Packets, propagate Partial, and feed the model a bounded JSON of the
//     packet. An embedded answer_packet from envelope.Data is preferred over a
//     bare NewAnswerPacket when the route handler attached one, and a Partial
//     packet sets ans.Partial = true.
//  3. Envelope nil, Value non-nil: the tool returned plain JSON. Feed the model
//     a bounded JSON of the value; do NOT append an AnswerPacket (no truth
//     envelope). Record the trace entry as Supported=true.
//  4. Envelope nil, Value nil: empty result; record as unsupported.
func (e *Engine) dispatchCall(
	ctx context.Context,
	question string,
	call provider.ToolCall,
	messages []provider.Message,
	ans *Answer,
) []provider.Message {
	// Pre-dispatch bounding: refuse an unbounded broad list/search call before it
	// can spend the dispatch deadline or the response budget, feeding the model an
	// executable narrowing hint instead.
	if hint, refused := boundToolCall(call.Name, call.Arguments); refused {
		ans.Limitations = appendLimitation(ans.Limitations, hint)
		ans.Trace = append(ans.Trace, TraceEntry{
			Tool:       call.Name,
			Args:       call.Arguments,
			Supported:  false,
			TruthClass: query.AnswerTruthUnsupported,
			Err:        "refused: unbounded list/search call",
		})
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

	if res.Envelope != nil {
		// Runaway result (over-budget or dispatch-timeout): preserve a bounded
		// continuation packet with a useful next action instead of collapsing rich
		// evidence into an opaque unsupported outcome.
		if cont, ok := oversizedContinuationPacket(question, call.Name, res.Envelope); ok {
			ans.Partial = true
			ans.Packets = append(ans.Packets, cont)
			ans.Trace = append(ans.Trace, TraceEntry{
				Tool:       call.Name,
				Args:       call.Arguments,
				Supported:  false,
				TruthClass: query.AnswerTruthUnsupported,
			})
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
		// Prefer an embedded answer_packet carried by the route handler.
		pkt := answerPacketForToolResult(question, call.Name, res.Envelope)
		// Propagate partial state upward to the aggregate answer.
		if pkt.Partial {
			ans.Partial = true
		}
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

	if res.Value != nil {
		// Plain-JSON result. Feed the model the bounded data but do not fabricate
		// an AnswerPacket — there is no truth envelope to score.
		ans.Trace = append(ans.Trace, TraceEntry{
			Tool:      call.Name,
			Args:      call.Arguments,
			Supported: true,
		})
		messages = append(messages, provider.Message{
			Role:       provider.RoleTool,
			ToolCallID: call.ID,
			ToolName:   call.Name,
			Text:       marshalPlainValue(res.Value),
		})
		return messages
	}

	// Both Envelope and Value are nil: empty or failed result.
	ans.Trace = append(ans.Trace, TraceEntry{
		Tool:       call.Name,
		Args:       call.Arguments,
		Supported:  false,
		TruthClass: query.AnswerTruthUnsupported,
	})
	messages = append(messages, provider.Message{
		Role:       provider.RoleTool,
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Text:       `{"supported":false,"truth_class":"unsupported"}`,
	})
	return messages
}

// answerPacketForToolResult, the tool-result serialisation helpers, and
// appendLimitation live in tool_result.go to keep this file focused on the loop.
