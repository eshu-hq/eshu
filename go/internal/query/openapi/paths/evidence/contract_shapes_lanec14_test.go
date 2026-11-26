// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContractShapesLaneC14Evidence pins the lane-C14 shape completion for
// the evidence leaf leftovers: the visualization packet inner shapes name
// the keys querycontract/visualization emits, the documentation aggregate
// scopes name the filter echo keys, the documentation facts payload and
// findings readback name their owner-backed shapes, and the live evidence
// bundle layers name the keys evidencebundle emits.
func TestContractShapesLaneC14Evidence(t *testing.T) {
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
	requireDescribed := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		for _, key := range keys {
			prop, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if desc, _ := prop["description"].(string); desc == "" {
				t.Errorf("%s: property %q has no description", where, key)
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

	t.Run("visualization packet inner shapes", func(t *testing.T) {
		t.Parallel()
		doc := fragment(t, VisualizationPackets, "visualization packets")
		schema := schemaAt(t, doc, "/api/v0/visualizations/derive", "post")
		packet := child(t, propsOf(t, schema, "derive"), "visualization_packet", "derive")

		// Owner: querycontract/visualization VisualizationNode emits id,
		// type, label always; category/role/roles/canonical_key/scope_key/
		// scope_keys/truth_label/evidence_handle/evidence_handles join
		// conditionally, so the pin requires the always keys and describes
		// the conditional ones.
		packetProps := propsOf(t, packet, "derive.visualization_packet")
		requireKeys(t, packetProps, "derive.visualization_packet",
			"view", "title", "supported", "nodes", "edges", "truth", "limits", "truncation")
		nodes := child(t, packetProps, "nodes", "derive.visualization_packet")
		nodeProps := propsOf(t, itemsOf(t, nodes, "derive.visualization_packet.nodes"), "derive.visualization_packet.nodes[]")
		requireKeys(t, nodeProps, "derive.visualization_packet.nodes[]",
			"id", "type", "label", "category", "role", "roles", "canonical_key",
			"scope_key", "scope_keys", "truth_label", "evidence_handle", "evidence_handles")
		requireDescribed(t, nodeProps, "derive.visualization_packet.nodes[]",
			"id", "type", "label", "category", "role", "evidence_handle")

		// Owner: querycontract/visualization VisualizationEdge emits id,
		// source, target, relationship always; truth_label and the handle
		// pair join conditionally.
		edges := child(t, packetProps, "edges", "derive.visualization_packet")
		edgeProps := propsOf(t, itemsOf(t, edges, "derive.visualization_packet.edges"), "derive.visualization_packet.edges[]")
		requireKeys(t, edgeProps, "derive.visualization_packet.edges[]",
			"id", "source", "target", "relationship", "truth_label", "evidence_handle", "evidence_handles")
		requireDescribed(t, edgeProps, "derive.visualization_packet.edges[]",
			"id", "source", "target", "relationship")

		// Owner: querycontract TruthEnvelope emits level and freshness
		// always; capability/profile/basis/backend/reason/truncated/
		// omissions join conditionally.
		truth := child(t, packetProps, "truth", "derive.visualization_packet")
		truthProps := propsOf(t, truth, "derive.visualization_packet.truth")
		requireKeys(t, truthProps, "derive.visualization_packet.truth",
			"level", "capability", "profile", "basis", "backend", "freshness", "reason", "truncated", "omissions")

		// Owner: querycontract/visualization VisualizationLimits always
		// emits all five keys; VisualizationTruncation always emits
		// truncated/dropped counts with dropped_node_ids conditional.
		limits := child(t, packetProps, "limits", "derive.visualization_packet")
		requireExactKeys(t, propsOf(t, limits, "derive.visualization_packet.limits"), "derive.visualization_packet.limits",
			"max_nodes", "max_edges", "ordering", "node_count", "edge_count")
		truncation := child(t, packetProps, "truncation", "derive.visualization_packet")
		requireExactKeys(t, propsOf(t, truncation, "derive.visualization_packet.truncation"), "derive.visualization_packet.truncation",
			"truncated", "dropped_node_count", "dropped_edge_count", "dropped_node_ids")

		// Owner: visualization evidenceCitationVisualizationNextCalls and
		// the incident/service-story builders emit tool/reason rows (plus
		// tool-conditional source-response keys on passthrough rows), so
		// the rows stay an open map with key semantics documented.
		next := child(t, packetProps, "recommended_next_calls", "derive.visualization_packet")
		nextItems := itemsOf(t, next, "derive.visualization_packet.recommended_next_calls")
		if _, ok := nextItems["additionalProperties"]; !ok {
			t.Error("derive.visualization_packet.recommended_next_calls[]: next-call rows need additionalProperties")
		}
		nextDesc, _ := nextItems["description"].(string)
		for _, key := range []string{"tool", "reason"} {
			if !strings.Contains(nextDesc, key) {
				t.Errorf("derive.visualization_packet.recommended_next_calls[]: description must name key %q", key)
			}
		}
	})

	t.Run("documentation aggregate scopes", func(t *testing.T) {
		t.Parallel()
		doc := fragment(t, DocumentationFindingAggregate, "documentation finding aggregate")
		for _, path := range []string{"/api/v0/documentation/findings/count", "/api/v0/documentation/findings/inventory"} {
			schema := schemaAt(t, doc, path, "get")
			props := propsOf(t, schema, path)
			// Owner: query.documentationFindingAggregateScope echoes the
			// non-empty filter fields as string pairs; only filters the
			// caller set appear, so the map stays open with key semantics.
			scope := child(t, props, "scope", path)
			if addl, ok := scope["additionalProperties"].(map[string]any); !ok || addl["type"] != "string" {
				t.Errorf("%s: scope must be an open string map echoing the request filters", path)
			}
			scopeDesc, _ := scope["description"].(string)
			for _, key := range []string{"scope_id", "finding_type", "source_id", "document_id", "status", "truth_level", "freshness_state"} {
				if !strings.Contains(scopeDesc, key) {
					t.Errorf("%s: scope description must name filter key %q", path, key)
				}
			}
		}
	})

	t.Run("documentation facts payload and findings readback", func(t *testing.T) {
		t.Parallel()
		doc := fragment(t, Documentation, "documentation")
		factsSchema := schemaAt(t, doc, "/api/v0/documentation/facts", "get")
		factsProps := propsOf(t, factsSchema, "facts")
		facts := child(t, factsProps, "facts", "facts")
		factProps := propsOf(t, itemsOf(t, facts, "facts.facts"), "facts.facts[]")
		// Owner: the Postgres documentation read model surfaces the raw
		// fact_records.payload verbatim; keys dispatch on fact_kind (16
		// collected/documented kinds), so the payload stays an open map
		// with the dispatch documented.
		payload := child(t, factProps, "payload", "facts.facts[]")
		if _, ok := payload["additionalProperties"]; !ok {
			t.Error("facts.facts[].payload: fact-kind-dispatched payload needs additionalProperties")
		}
		payloadDesc, _ := payload["description"].(string)
		if !strings.Contains(payloadDesc, "fact_kind") {
			t.Error("facts.facts[].payload: description must name the fact_kind dispatch")
		}

		findingsSchema := schemaAt(t, doc, "/api/v0/documentation/findings", "get")
		findingsProps := propsOf(t, findingsSchema, "findings")
		// Owner: query.documentationFindingsResponse carries the stored
		// finding-fact payloads plus the computed access_disposition
		// family; rows vary by finding fact, so items stay open with the
		// always keys named.
		findings := child(t, findingsProps, "findings", "findings")
		findingItems := itemsOf(t, findings, "findings.findings")
		if _, ok := findingItems["additionalProperties"]; !ok {
			t.Error("findings.findings[]: finding rows need additionalProperties")
		}
		findingDesc, _ := findingItems["description"].(string)
		for _, key := range []string{"finding_id", "access_disposition"} {
			if !strings.Contains(findingDesc, key) {
				t.Errorf("findings.findings[]: description must name key %q", key)
			}
		}

		// Owner: querycontract DocumentationTargetCoverage always emits
		// findings_returned/target_fact_count/truncated; target and the
		// kind buckets join conditionally.
		coverage := child(t, findingsProps, "coverage", "findings")
		coverageProps := propsOf(t, coverage, "findings.coverage")
		requireKeys(t, coverageProps, "findings.coverage",
			"target", "findings_returned", "target_fact_count", "target_fact_kinds",
			"source_only_count", "source_only_fact_kinds", "truncated")

		// Owner: querycontract DocumentationMissingEvidence emits reason
		// always and detail conditionally.
		missing := child(t, findingsProps, "missing_evidence", "findings")
		missingProps := propsOf(t, itemsOf(t, missing, "findings.missing_evidence"), "findings.missing_evidence[]")
		requireExactKeys(t, missingProps, "findings.missing_evidence[]", "reason", "detail")

		// Owner: related_facts previews raw documentation facts, the same
		// rows as the facts list, including the kind-dispatched payload.
		// Rows vary by fact kind, so items stay open with the row keys named.
		related := child(t, findingsProps, "related_facts", "findings")
		relatedItems := itemsOf(t, related, "findings.related_facts")
		if _, ok := relatedItems["additionalProperties"]; !ok {
			t.Error("findings.related_facts[]: fact rows need additionalProperties")
		}
		relatedDesc, _ := relatedItems["description"].(string)
		for _, key := range []string{"fact_id", "fact_kind", "payload"} {
			if !strings.Contains(relatedDesc, key) {
				t.Errorf("findings.related_facts[]: description must name key %q", key)
			}
		}
	})

	t.Run("documentation evidence packet layers", func(t *testing.T) {
		t.Parallel()
		doc := fragment(t, Documentation, "documentation")
		schema := schemaAt(t, doc, "/api/v0/documentation/findings/{finding_id}/evidence-packet", "get")
		props := propsOf(t, schema, "evidence-packet")
		// Owner: the stored documentation evidence packet (packet_version)
		// carries versioned layers; keys vary by packet, so each layer is
		// an open map with its contents named in the description.
		for _, key := range []string{"finding", "document", "section", "bounded_excerpt", "current_truth", "truth", "permissions", "states"} {
			layer := child(t, props, key, "evidence-packet")
			if _, ok := layer["additionalProperties"]; !ok {
				t.Errorf("evidence-packet.%s: stored packet layer needs additionalProperties", key)
			}
			if desc, _ := layer["description"].(string); desc == "" {
				t.Errorf("evidence-packet.%s: stored packet layer needs a description", key)
			}
		}
		for _, key := range []string{"linked_entities", "evidence_refs"} {
			arr := child(t, props, key, "evidence-packet")
			items := itemsOf(t, arr, "evidence-packet."+key)
			if _, ok := items["additionalProperties"]; !ok {
				t.Errorf("evidence-packet.%s[]: stored packet rows need additionalProperties", key)
			}
		}
	})

	t.Run("live evidence bundle layers", func(t *testing.T) {
		t.Parallel()
		doc := fragment(t, Bundle, "evidence bundle")
		schema := schemaAt(t, doc, "/api/v0/evidence/bundle", "get")
		props := propsOf(t, schema, "bundle")

		// Owner: evidencebundle.BuildLiveBundle stamps
		// SourceIdentity{Repository: "live:stack"}; deployment is absent on
		// the live route.
		source := child(t, props, "source", "bundle")
		requireExactKeys(t, propsOf(t, source, "bundle.source"), "bundle.source", "repository", "deployment")

		// Owner: evidencebundle RedactionProfile stamps profile
		// share_safe_v1 with the four screen rules.
		redaction := child(t, props, "redaction", "bundle")
		requireExactKeys(t, propsOf(t, redaction, "bundle.redaction"), "bundle.redaction", "profile", "rules")

		// Owner: BuildLiveBundle Contents carries only operator_state,
		// pipeline_state, and semantic_provider_state (no answer/investigation
		// packets on the live route).
		contents := child(t, props, "contents", "bundle")
		requireExactKeys(t, propsOf(t, contents, "bundle.contents"), "bundle.contents",
			"operator_state", "pipeline_state", "semantic_provider_state")
		operator := child(t, propsOf(t, contents, "bundle.contents"), "operator_state", "bundle.contents")
		operatorProps := propsOf(t, itemsOf(t, operator, "bundle.contents.operator_state"), "bundle.contents.operator_state[]")
		requireExactKeys(t, operatorProps, "bundle.contents.operator_state[]", "kind", "state", "reason")

		// Owner: evidencebundle PipelineStateSnapshot always emits the
		// counts/queue/activity/history keys; stage/domain/collector rows
		// and health_reasons join conditionally.
		pipeline := child(t, propsOf(t, contents, "bundle.contents"), "pipeline_state", "bundle.contents")
		pipelineProps := propsOf(t, pipeline, "bundle.contents.pipeline_state")
		requireKeys(t, pipelineProps, "bundle.contents.pipeline_state",
			"repository_count", "health_state", "health_reasons", "queue",
			"queue_blocked_count", "scope_activity", "generation_history",
			"stage_summaries", "domain_backlogs", "collectors")

		// Owner: evidencebundle SemanticProviderStateSnapshot.
		semantic := child(t, propsOf(t, contents, "bundle.contents"), "semantic_provider_state", "bundle.contents")
		requireExactKeys(t, propsOf(t, semantic, "bundle.contents.semantic_provider_state"), "bundle.contents.semantic_provider_state",
			"state", "reason", "provider_configured", "provider_profiles")

		// Owner: evidencebundle MissingEvidence/ReproduceCall/Bounds emit
		// fixed keys; reproduce args and truncated_layers join conditionally.
		missing := child(t, props, "missing_evidence", "bundle")
		requireExactKeys(t, propsOf(t, itemsOf(t, missing, "bundle.missing_evidence"), "bundle.missing_evidence[]"), "bundle.missing_evidence[]",
			"family", "reason")
		reproduce := child(t, props, "reproduce", "bundle")
		reproduceProps := propsOf(t, itemsOf(t, reproduce, "bundle.reproduce"), "bundle.reproduce[]")
		requireExactKeys(t, reproduceProps, "bundle.reproduce[]", "kind", "target", "args")
		bounds := child(t, props, "bounds", "bundle")
		requireExactKeys(t, propsOf(t, bounds, "bundle.bounds"), "bundle.bounds",
			"max_answer_packets", "max_investigation_packets", "max_handles", "truncated", "truncated_layers")
	})
}
