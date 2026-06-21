package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/ask/provider"
	"github.com/eshu-hq/eshu/go/internal/query"
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
			posture := e.resolveNarrationPosture()
			e.narrate(ctx, &ans, posture)
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
	}

	// Loop exhausted MaxIterations without a final text turn.
	ans.Partial = true
	ans.Limitations = appendLimitation(ans.Limitations, "reached max reasoning iterations")
	ans.Prose = bestPacketSummary(ans.Packets)
	if ans.Prose == "" {
		ans.Limitations = appendLimitation(ans.Limitations, "no supported evidence assembled")
	}
	e.log().Warn("ask: reached max reasoning iterations",
		"max_iterations", e.opts.MaxIterations,
		"max_tool_calls_per_turn", e.opts.MaxToolCallsPerTurn,
		"packets", len(ans.Packets),
		"has_supported_evidence", ans.Prose != "")
	posture := e.resolveNarrationPosture()
	e.narrate(ctx, &ans, posture)
	return ans, nil
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
		// FINDING 1: prefer an embedded answer_packet carried by the route handler.
		pkt, ok := extractEmbeddedPacket(res.Envelope)
		if !ok {
			// FINDING 5 (issue #3437): pass a bounded JSON summary of the
			// envelope Data so the model receives actual content in the
			// tool-result message and bestPacketSummary can return prose at
			// max-iterations. Without this, dispatchCall produced packets with
			// Summary=="" causing "no supported evidence assembled" and the
			// model kept calling tools until MaxIterations because the
			// tool-result message carried no data for it to reason about.
			pkt = query.NewAnswerPacket(query.AnswerPacketInput{
				Question:    question,
				PrimaryTool: call.Name,
				Envelope:    res.Envelope,
				Summary:     envelopeDataSummary(res.Envelope),
			})
		}
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
