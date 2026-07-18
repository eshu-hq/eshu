// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package engine

import (
	"encoding/json"
	"fmt"

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
