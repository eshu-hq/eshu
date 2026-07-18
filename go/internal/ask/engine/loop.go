// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/answerguardrail"
	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// extractEmbeddedPacket checks whether env.Data is a JSON object containing an
// "answer_packet" key and, if so, decodes that sub-value into a query.AnswerPacket.
//
// Route handlers like service-story and code-topic embed a pre-built AnswerPacket
// directly in the response Data rather than leaving packet construction to the engine.
// This helper surfaces that embedded packet so its Summary, EvidenceHandles,
// CitationRef, RecommendedNextCalls, and truth classification are preserved rather
// than replaced by a bare NewAnswerPacket call.
//
// Returns (packet, true) when the embedded packet was successfully decoded.
// Returns (zero, false) when env is nil, Data is not an object, the key is absent,
// or the decode fails — in all fallback cases NewAnswerPacket should be used instead.
func extractEmbeddedPacket(env *query.ResponseEnvelope) (query.AnswerPacket, bool) {
	if env == nil {
		return query.AnswerPacket{}, false
	}
	// Marshal Data to JSON so we can inspect it as a raw map.
	dataBytes, err := json.Marshal(env.Data)
	if err != nil {
		return query.AnswerPacket{}, false
	}
	var dataMap map[string]json.RawMessage
	if err := json.Unmarshal(dataBytes, &dataMap); err != nil {
		return query.AnswerPacket{}, false
	}
	raw, ok := dataMap["answer_packet"]
	if !ok {
		return query.AnswerPacket{}, false
	}
	var pkt query.AnswerPacket
	if err := json.Unmarshal(raw, &pkt); err != nil {
		return query.AnswerPacket{}, false
	}
	return pkt, true
}

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
	var progress evidenceProgress

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
			// FINDING 3: the assistant message must carry only the dispatched
			// (truncated) tool calls. Replaying all comp.ToolCalls while only
			// providing RoleTool responses for the truncated subset causes the
			// provider to reject the next request due to unmatched IDs.
			messages[len(messages)-1].ToolCalls = calls
		}

		for _, call := range calls {
			messages = e.dispatchCall(ctx, question, call, messages, &ans)
		}

		// Evidence-sufficiency stop: once answer evidence is held and the latest
		// turn added no new distinct supported evidence, continuing would only
		// spin on redundant or oversized calls, so terminate instead of running
		// to the iteration bound.
		if made, have := progress.observe(ans.Packets); have && !made {
			if finalizeIndexedRepositoryCountAnswer(question, &ans) {
				ans.TerminationReason = terminationDeterministicRoute
				return ans, nil
			}
			e.log().Info("ask: evidence sufficiency stop",
				"iteration", i+1,
				"max_iterations", e.opts.MaxIterations,
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
//     packet. FINDING 1: prefer an embedded answer_packet from envelope.Data
//     over a bare NewAnswerPacket when the route handler attached one.
//     FINDING 4: when the packet is Partial, set ans.Partial = true.
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
		// FINDING 1: prefer an embedded answer_packet carried by the route handler.
		pkt := answerPacketForToolResult(question, call.Name, res.Envelope)
		// FINDING 4: propagate partial state upward to the aggregate answer.
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
		// FINDING 2: plain-JSON result. Feed the model the bounded data but do
		// not fabricate an AnswerPacket — there is no truth envelope to score.
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

func answerPacketForToolResult(
	question string,
	toolName string,
	envelope *query.ResponseEnvelope,
) query.AnswerPacket {
	var packet query.AnswerPacket
	if packet, ok := extractEmbeddedPacket(envelope); ok {
		if toolName == indexedRepositoryInventoryTool && asksForIndexedRepositoryCount(question) {
			applyIndexedRepositoryCountResult(&packet, envelope)
		}
		return packet
	}
	packet = query.NewAnswerPacket(query.AnswerPacketInput{
		Question:    question,
		PrimaryTool: toolName,
		Envelope:    envelope,
		Summary:     envelopeDataSummary(envelope),
	})
	if toolName == indexedRepositoryInventoryTool && asksForIndexedRepositoryCount(question) {
		applyIndexedRepositoryCountResult(&packet, envelope)
	}
	return packet
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

// marshalPlainValue serialises a plain-JSON tool result into a bounded string
// for the tool-result message. This is used when the tool returned a plain JSON
// payload rather than a canonical ResponseEnvelope. The same maxToolResultBytes
// cap applies; oversized payloads are replaced with a bounded note so the
// conversation thread stays within the provider's context window.
func marshalPlainValue(value any) string {
	b, err := json.Marshal(value)
	if err != nil {
		return `{"note":"tool result could not be serialised"}`
	}
	if len(b) <= maxToolResultBytes {
		return string(b)
	}
	return fmt.Sprintf(`{"note":"tool result truncated","bytes":%d}`, len(b))
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

// toolResultWrapperOverhead is the byte budget reserved for the JSON fields
// that marshalToolResult wraps around the summary string. It covers the
// worst-case skeleton without summary:
//
//	{"truth_class":"semantic_observation","supported":true,"partial":true}
//
// plus the summary key, quotes, comma, and JSON-escaping headroom. 128 bytes
// is a conservative bound that keeps the final marshalToolResult output within
// maxToolResultBytes even when the summary itself is at the inner cap.
const toolResultWrapperOverhead = 128

// envelopeDataSummary returns a bounded JSON string of the envelope Data for
// use as an AnswerPacket Summary. It gives the model actual content to reason
// about in the tool-result message and lets bestPacketSummary return prose
// when MaxIterations is reached without a final text turn.
//
// The summary is capped at maxToolResultBytes - toolResultWrapperOverhead so
// that when marshalToolResult wraps it in a JSON skeleton (adding truth_class,
// supported, partial, and JSON-escaping overhead) the final output stays within
// maxToolResultBytes. Capping at the full maxToolResultBytes would cause
// marshalToolResult to silently drop the summary and return only a minimal
// skeleton, giving the model no data — the same broken state as before #3437
// for large payloads.
//
// An empty string is returned when Data is nil or cannot be marshalled —
// callers treat an empty Summary as "no summary available", the safe fallback.
func envelopeDataSummary(env *query.ResponseEnvelope) string {
	if env == nil || env.Data == nil {
		return ""
	}
	b, err := json.Marshal(env.Data)
	if err != nil {
		return ""
	}
	limit := maxToolResultBytes - toolResultWrapperOverhead
	if len(b) > limit {
		b = b[:limit]
	}
	return string(b)
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
