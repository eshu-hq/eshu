// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesLaneC14Impact pins the lane-C14 shape completion for the
// impact leaf leftovers: the support_overview target_support map names the
// keys buildStoryTargetSupport emits, the answer_metadata coverage companions
// mirror the route coverage they carry, and the answer_packet truth/result
// shapes follow the AnswerPacket owner.
func TestContractShapesLaneC14Impact(t *testing.T) {
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
	itemsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		items, ok := node["items"].(map[string]any)
		if !ok {
			t.Fatalf("%s: array schema has no items", where)
		}
		return items
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
	requireKeys := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		for _, key := range keys {
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if node["type"] == "object" {
				_, hasProps := node["properties"]
				_, hasAdditional := node["additionalProperties"]
				if !hasProps && !hasAdditional {
					t.Errorf("%s: property %q is a bare object with no properties", where, key)
				}
			}
			if node["type"] == "array" {
				items, ok := node["items"].(map[string]any)
				if !ok {
					t.Errorf("%s: property %q is an array with no items", where, key)
					continue
				}
				if items["type"] == "object" {
					_, hasProps := items["properties"]
					_, hasAdditional := items["additionalProperties"]
					if !hasProps && !hasAdditional {
						t.Errorf("%s: property %q has bare object items", where, key)
					}
				}
			}
		}
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

	t.Run("target support names buildStoryTargetSupport keys", func(t *testing.T) {
		t.Parallel()
		doc := fragment(t, Routes, "impact routes")
		schema := schemaAt(t, doc, "/api/v0/impact/trace-deployment-chain", "post")
		props := propsOf(t, schema, "trace")
		overview := child(t, props, "support_overview", "trace")
		overviewProps := propsOf(t, overview, "trace.support_overview")
		// Owner: query.buildStoryTargetSupport always emits evidence,
		// counts, ambiguous pair, coverage, missing_evidence, limit, and
		// source.
		target := child(t, overviewProps, "target_support", "trace.support_overview")
		targetProps := propsOf(t, target, "trace.support_overview.target_support")
		requireExactKeys(t, targetProps, "trace.support_overview.target_support",
			"evidence", "evidence_count", "work_item_count", "incident_routing_count",
			"ambiguous_evidence", "ambiguous_count", "coverage", "missing_evidence", "limit", "source")
		// Owner: the coverage block always carries target, target_fact_count,
		// and truncated; repository_workload_count joins for service targets.
		coverage := child(t, targetProps, "coverage", "trace.support_overview.target_support")
		requireExactKeys(t, propsOf(t, coverage, "trace.support_overview.target_support.coverage"),
			"trace.support_overview.target_support.coverage",
			"target", "target_fact_count", "truncated", "repository_workload_count")
		// Owner: query.serviceStorySupportEvidenceRow always emits
		// link_basis, fact_id, fact_kind, scope_id, generation_id,
		// source_system, and observed_at; source_record_id,
		// linked_via_fact_id, and payload join conditionally.
		evidence := child(t, targetProps, "evidence", "trace.support_overview.target_support")
		evidenceProps := propsOf(t, itemsOf(t, evidence, "trace.support_overview.target_support.evidence"), "trace.support_overview.target_support.evidence[]")
		requireKeys(t, evidenceProps, "trace.support_overview.target_support.evidence[]",
			"link_basis", "fact_id", "fact_kind", "scope_id", "generation_id",
			"source_system", "observed_at", "source_record_id", "linked_via_fact_id", "payload")
		// Owner: query.serviceStorySupportMissingEvidence emits reason/detail
		// rows (support_correlation_ambiguous or support_target_facts_absent).
		missing := child(t, targetProps, "missing_evidence", "trace.support_overview.target_support")
		missingProps := propsOf(t, itemsOf(t, missing, "trace.support_overview.target_support.missing_evidence"), "trace.support_overview.target_support.missing_evidence[]")
		requireExactKeys(t, missingProps, "trace.support_overview.target_support.missing_evidence[]",
			"reason", "detail")
	})

	t.Run("investigate answer_metadata coverage mirrors route coverage", func(t *testing.T) {
		t.Parallel()
		doc := fragment(t, Routes, "impact routes")
		schema := schemaAt(t, doc, "/api/v0/impact/change-surface/investigate", "post")
		props := propsOf(t, schema, "investigate")
		routeCoverage := child(t, props, "coverage", "investigate")
		routeCoverageProps := propsOf(t, routeCoverage, "investigate.coverage")
		meta := child(t, props, "answer_metadata", "investigate")
		metaCoverage := child(t, propsOf(t, meta, "investigate.answer_metadata"), "coverage", "investigate.answer_metadata")
		metaCoverageProps := propsOf(t, metaCoverage, "investigate.answer_metadata.coverage")
		// Owner: answer.answerMetadataCoverage copies data["coverage"], so
		// the companion carries the same keys as the route coverage above.
		for key := range routeCoverageProps {
			if _, ok := metaCoverageProps[key].(map[string]any); !ok {
				t.Errorf("investigate.answer_metadata.coverage: missing route coverage key %q", key)
			}
		}
		for key := range metaCoverageProps {
			if _, ok := routeCoverageProps[key].(map[string]any); !ok {
				t.Errorf("investigate.answer_metadata.coverage: phantom key %q not in route coverage", key)
			}
		}
		// The companion rows keep their owner shapes: handles mirror the
		// route evidence rows, limitations stay bounded caveats, next calls
		// name the follow-up tool.
		metaProps := propsOf(t, meta, "investigate.answer_metadata")
		requireKeys(t, metaProps, "investigate.answer_metadata",
			"schema_version", "evidence_handles", "missing_evidence", "limitations",
			"truncated", "coverage", "partial_reasons", "recommended_next_calls")
	})

	t.Run("pre-change answer_metadata coverage mirrors route coverage", func(t *testing.T) {
		t.Parallel()
		doc := fragment(t, Rest, "impact rest")
		schema := schemaAt(t, doc, "/api/v0/impact/pre-change", "post")
		props := propsOf(t, schema, "pre-change")
		routeCoverageProps := propsOf(t, child(t, props, "coverage", "pre-change"), "pre-change.coverage")
		meta := child(t, props, "answer_metadata", "pre-change")
		metaCoverageProps := propsOf(t, child(t, propsOf(t, meta, "pre-change.answer_metadata"), "coverage", "pre-change.answer_metadata"), "pre-change.answer_metadata.coverage")
		for key := range routeCoverageProps {
			if _, ok := metaCoverageProps[key].(map[string]any); !ok {
				t.Errorf("pre-change.answer_metadata.coverage: missing route coverage key %q", key)
			}
		}
		for key := range metaCoverageProps {
			if _, ok := routeCoverageProps[key].(map[string]any); !ok {
				t.Errorf("pre-change.answer_metadata.coverage: phantom key %q not in route coverage", key)
			}
		}
	})

	t.Run("developer plan answer_metadata coverage mirrors route coverage", func(t *testing.T) {
		t.Parallel()
		doc := fragment(t, Rest, "impact rest")
		schema := schemaAt(t, doc, "/api/v0/impact/developer-change-plan", "post")
		props := propsOf(t, schema, "developer-change-plan")
		routeCoverageProps := propsOf(t, child(t, props, "coverage", "developer-change-plan"), "developer-change-plan.coverage")
		meta := child(t, props, "answer_metadata", "developer-change-plan")
		metaCoverageProps := propsOf(t, child(t, propsOf(t, meta, "developer-change-plan.answer_metadata"), "coverage", "developer-change-plan.answer_metadata"), "developer-change-plan.answer_metadata.coverage")
		for key := range routeCoverageProps {
			if _, ok := metaCoverageProps[key].(map[string]any); !ok {
				t.Errorf("developer-change-plan.answer_metadata.coverage: missing route coverage key %q", key)
			}
		}
		for key := range metaCoverageProps {
			if _, ok := routeCoverageProps[key].(map[string]any); !ok {
				t.Errorf("developer-change-plan.answer_metadata.coverage: phantom key %q not in route coverage", key)
			}
		}
	})

	t.Run("answer packets carry truth envelope and bounded result", func(t *testing.T) {
		t.Parallel()
		doc := fragment(t, Rest, "impact rest")
		schema := schemaAt(t, doc, "/api/v0/impact/pre-change", "post")
		props := propsOf(t, schema, "/api/v0/impact/pre-change")
		packet := child(t, props, "answer_packet", "/api/v0/impact/pre-change")
		packetProps := propsOf(t, packet, "/api/v0/impact/pre-change.answer_packet")
		// Owner: ask.NewAnswerPacketFromMetadata over answer.AnswerPacket:
		// truth is a copy of the envelope TruthEnvelope (querycontract),
		// result a compact embedded copy of the envelope data.
		truth := child(t, packetProps, "truth", "/api/v0/impact/pre-change.answer_packet")
		truthProps := propsOf(t, truth, "/api/v0/impact/pre-change.answer_packet.truth")
		requireKeys(t, truthProps, "/api/v0/impact/pre-change.answer_packet.truth",
			"level", "capability", "profile", "basis", "backend", "freshness", "reason", "truncated", "omissions")
		result := child(t, packetProps, "result", "/api/v0/impact/pre-change.answer_packet")
		if _, ok := result["additionalProperties"]; !ok {
			t.Error("/api/v0/impact/pre-change.answer_packet.result: embedded envelope copy needs additionalProperties")
		}
	})

	t.Run("pre-change next-call arguments stay tool-conditional", func(t *testing.T) {
		t.Parallel()
		doc := fragment(t, Rest, "impact rest")
		schema := schemaAt(t, doc, "/api/v0/impact/pre-change", "post")
		props := propsOf(t, schema, "pre-change")
		next := child(t, props, "recommended_next_calls", "pre-change")
		nextProps := propsOf(t, itemsOf(t, next, "pre-change.recommended_next_calls"), "pre-change.recommended_next_calls[]")
		// Owner: preChangeNextCalls emits surface-call rows with args and
		// resource-call rows with arguments; argument keys depend on the
		// tool, so arguments stays an open map with key semantics.
		args := child(t, nextProps, "arguments", "pre-change.recommended_next_calls[]")
		if _, ok := args["additionalProperties"]; !ok {
			t.Error("pre-change.recommended_next_calls[].arguments: tool-conditional args need additionalProperties")
		}
		argsDesc, _ := args["description"].(string)
		if !strings.Contains(argsDesc, "tool") {
			t.Error("pre-change.recommended_next_calls[].arguments: description must name the tool-conditional keys")
		}
	})
}
