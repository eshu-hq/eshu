// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesLaneD12AnswerPackets pins the lane-D12 answer-packet
// completion: both attach paths (ask.AttachPreChangeImpactPacket and
// ask.AttachDeveloperChangePlanPacket) build the packet through
// ask.NewAnswerPacketFromMetadata over answer.NewAnswerPacket, which sets
// Truth from the envelope and never sets Result (EmbedResult stays false)
// or CitationRef (never populated from metadata). The fragments must carry
// truth on both packets and must not name the phantom result/citation_ref
// keys on either.
func TestContractShapesLaneD12AnswerPackets(t *testing.T) {
	t.Parallel()

	fragment := func(t *testing.T, raw, where string) map[string]any {
		t.Helper()
		trimmed := strings.TrimSpace(raw)
		trimmed = strings.TrimSuffix(trimmed, ",")
		var doc map[string]any
		if err := json.Unmarshal([]byte("{"+trimmed+"}"), &doc); err != nil {
			t.Fatalf("%s: fragment is not valid JSON: %v", where, err)
		}
		return doc
	}
	child := func(t *testing.T, node map[string]any, key, where string) map[string]any {
		t.Helper()
		next, ok := node[key].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing or non-object property %q", where, key)
		}
		return next
	}
	propsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		props, ok := node["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: object schema has no properties", where)
		}
		return props
	}
	schemaAt := func(t *testing.T, doc map[string]any, path, method string) map[string]any {
		t.Helper()
		route, ok := doc[path].(map[string]any)
		if !ok {
			t.Fatalf("fragment paths missing %s", path)
		}
		op := child(t, route, method, path)
		responses := child(t, op, "responses", path+" "+method)
		okResp := child(t, responses, "200", path+" "+method)
		content, ok := okResp["content"].(map[string]any)
		if !ok {
			t.Fatalf("%s %s 200 response has no content", path, method)
		}
		body := child(t, content, "application/json", path+" "+method)
		return child(t, body, "schema", path+" "+method)
	}
	requireExactKeys := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		want := map[string]bool{}
		for _, key := range keys {
			want[key] = true
			if _, ok := props[key].(map[string]any); !ok {
				t.Errorf("%s: missing property %q", where, key)
			}
		}
		for key := range props {
			if !want[key] {
				t.Errorf("%s: phantom property %q not emitted by the owner", where, key)
			}
		}
	}
	requireScalarTypes := func(t *testing.T, props map[string]any, where string, want map[string]string) {
		t.Helper()
		for key, typ := range want {
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if got, _ := node["type"].(string); got != typ {
				t.Errorf("%s: property %q has type %q, want %q", where, key, got, typ)
			}
		}
	}

	// Owner: ask.NewAnswerPacketFromMetadata over answer.AnswerPacket sets
	// Truth (a copy of the envelope TruthEnvelope) on both attach paths and
	// never sets Result or CitationRef. Both packets carry exactly the 16
	// emitted keys.
	packetKeys := []string{
		"prompt_family", "question", "primary_tool", "primary_route",
		"truth_class", "summary", "supported", "partial", "result_ref",
		"truth", "limitations", "truncated", "missing_evidence",
		"evidence_handles", "recommended_next_calls", "unsupported_reasons",
	}

	for _, tc := range []struct{ path, method, where string }{
		{"/api/v0/impact/pre-change", "post", "pre-change"},
		{"/api/v0/impact/developer-change-plan", "post", "developer-change-plan"},
	} {
		t.Run("packet keys on "+tc.where, func(t *testing.T) {
			t.Parallel()
			doc := fragment(t, Rest, "impact rest")
			props := propsOf(t, schemaAt(t, doc, tc.path, tc.method), tc.where)
			packet := child(t, props, "answer_packet", tc.where)
			packetProps := propsOf(t, packet, tc.where+".answer_packet")
			requireExactKeys(t, packetProps, tc.where+".answer_packet", packetKeys...)
			requireScalarTypes(t, packetProps, tc.where+".answer_packet", map[string]string{
				"truth_class": "string", "summary": "string",
				"supported": "boolean", "partial": "boolean",
				"result_ref": "string", "truncated": "boolean",
			})
			// Owner: querycontract TruthEnvelope always emits level and
			// freshness; capability, profile, basis, backend, reason,
			// truncated, and omissions join when the envelope carries them.
			truth := child(t, packetProps, "truth", tc.where+".answer_packet")
			requireExactKeys(t, propsOf(t, truth, tc.where+".answer_packet.truth"),
				tc.where+".answer_packet.truth",
				"level", "capability", "profile", "basis", "backend",
				"freshness", "reason", "truncated", "omissions")
			freshness := child(t, propsOf(t, truth, tc.where+".answer_packet.truth"),
				"freshness", tc.where+".answer_packet.truth")
			requireExactKeys(t, propsOf(t, freshness, tc.where+".answer_packet.truth.freshness"),
				tc.where+".answer_packet.truth.freshness",
				"state", "observed_at", "detail", "cause", "next_check")
		})
	}
}
