// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// TestContractShapesLaneD14Keys pins the lane-D14 fixed-key owners: every
// object the mop-up gave properties or additionalProperties keeps exactly
// the key set its Go owner emits, with the scalar types the owner writes.
// Each entry names the emitting owner. The generic mop test
// (TestContractShapesLaneD14Mop) covers descriptions and the no-bare-object
// bar; this test covers exact key sets so a dropped or phantom key fails.
func TestContractShapesLaneD14Keys(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}

	child := func(t *testing.T, node map[string]any, key, where string) map[string]any {
		t.Helper()
		next, ok := node[key].(map[string]any)
		if !ok {
			t.Errorf("%s: missing or non-object %q", where, key)
			return nil
		}
		return next
	}

	propsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		if node == nil {
			t.Errorf("%s: nil node has no properties", where)
			return nil
		}
		props, ok := node["properties"].(map[string]any)
		if !ok {
			t.Errorf("%s: object schema has no properties", where)
			return nil
		}
		return props
	}

	itemsOf := func(t *testing.T, node map[string]any, where string) map[string]any {
		t.Helper()
		if node == nil {
			t.Errorf("%s: nil node has no items", where)
			return nil
		}
		items, ok := node["items"].(map[string]any)
		if !ok {
			t.Errorf("%s: array schema has no items", where)
			return nil
		}
		return items
	}

	nodeOf := func(t *testing.T, props map[string]any, where, key string) map[string]any {
		t.Helper()
		if props == nil {
			t.Errorf("%s: nil props missing %q", where, key)
			return nil
		}
		node, ok := props[key].(map[string]any)
		if !ok {
			t.Errorf("%s: missing property %q", where, key)
			return nil
		}
		return node
	}

	requireExactKeys := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		if props == nil {
			t.Errorf("%s: nil props have no keys", where)
			return
		}
		want := make(map[string]bool, len(keys))
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

	requireKeys := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
		if props == nil {
			t.Errorf("%s: nil props have no keys", where)
			return
		}
		for _, key := range keys {
			if _, ok := props[key].(map[string]any); !ok {
				t.Errorf("%s: missing property %q", where, key)
			}
		}
	}

	schema200 := func(t *testing.T, path, method string) map[string]any {
		t.Helper()
		paths, ok := spec["paths"].(map[string]any)
		if !ok {
			t.Fatal("spec has no paths object")
		}
		route, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("spec paths missing %s", path)
		}
		op := child(t, route, method, path)
		responses := child(t, op, "responses", path+" "+method)
		okResp := child(t, responses, "200", path+" "+method)
		content := child(t, okResp, "content", path+" "+method+" 200")
		body := child(t, content, "application/json", path+" "+method+" 200")
		return child(t, body, "schema", path+" "+method+" 200")
	}

	packetSchemas := func(t *testing.T) map[string]any {
		t.Helper()
		components, ok := spec["components"].(map[string]any)
		if !ok {
			t.Fatal("spec has no components")
		}
		schemas, ok := components["schemas"].(map[string]any)
		if !ok {
			t.Fatal("spec components has no schemas")
		}
		return schemas
	}

	t.Run("story service_identity", func(t *testing.T) {
		t.Parallel()
		top := propsOf(t, schema200(t, "/api/v0/services/{service_name}/story", "get"), "story")
		// Owner: service/story_dossier.go buildServiceIdentity.
		requireExactKeys(t, propsOf(t, nodeOf(t, top, "story", "service_identity"), "story.service_identity"),
			"story.service_identity",
			"service_id", "service_name", "kind", "repo_id", "repo_name",
			"materialization_status", "query_basis", "limitations")
	})

	t.Run("story downstream_consumers", func(t *testing.T) {
		t.Parallel()
		top := propsOf(t, schema200(t, "/api/v0/services/{service_name}/story", "get"), "story")
		// Owner: service/story_dossier.go BuildServiceDownstreamConsumers.
		requireExactKeys(t, propsOf(t, nodeOf(t, top, "story", "downstream_consumers"), "story.downstream_consumers"),
			"story.downstream_consumers",
			"graph_dependent_count", "content_consumer_count",
			"graph_dependents", "content_consumers", "truncated")
	})

	t.Run("story result_limits", func(t *testing.T) {
		t.Parallel()
		top := propsOf(t, schema200(t, "/api/v0/services/{service_name}/story", "get"), "story")
		// Owner: service/story_dossier.go buildServiceResultLimitsWithContext.
		requireExactKeys(t, propsOf(t, nodeOf(t, top, "story", "result_limits"), "story.result_limits"),
			"story.result_limits",
			"limit", "downstream_read_limit", "downstream_render_limit",
			"evidence_file_read_limit", "ordering", "endpoint_count",
			"upstream_count", "downstream_count", "downstream_entries",
			"downstream_repository_count", "lower_bound", "truncated",
			"drilldown_basis", "relationship_tool", "service_context_path",
			"endpoint_list_tool", "endpoint_list_path", "consumer_list_tool",
			"consumer_list_path")
	})

	t.Run("story evidence_graph", func(t *testing.T) {
		t.Parallel()
		top := propsOf(t, schema200(t, "/api/v0/services/{service_name}/story", "get"), "story")
		// Owner: service/story_evidence_graph.go buildServiceEvidenceGraph.
		graph := propsOf(t, nodeOf(t, top, "story", "evidence_graph"), "story.evidence_graph")
		requireExactKeys(t, graph, "story.evidence_graph",
			"nodes", "edges", "edge_count", "truncated")
		requireExactKeys(t, propsOf(t, itemsOf(t, nodeOf(t, graph, "story.evidence_graph", "nodes"), "story.evidence_graph.nodes"), "story.evidence_graph.nodes[]"),
			"story.evidence_graph.nodes[]",
			"id", "label", "kind", "category", "role")
		requireExactKeys(t, propsOf(t, itemsOf(t, nodeOf(t, graph, "story.evidence_graph", "edges"), "story.evidence_graph.edges"), "story.evidence_graph.edges[]"),
			"story.evidence_graph.edges[]",
			"id", "source", "target", "relationship_type",
			"confidence", "evidence_count", "rationale", "resolved_id")
	})

	t.Run("story code_to_runtime_trace", func(t *testing.T) {
		t.Parallel()
		top := propsOf(t, schema200(t, "/api/v0/services/{service_name}/story", "get"), "story")
		// Owner: service/story_trace_path.go buildServiceCodeToRuntimeTrace.
		trace := propsOf(t, nodeOf(t, top, "story", "code_to_runtime_trace"), "story.code_to_runtime_trace")
		requireExactKeys(t, trace, "story.code_to_runtime_trace",
			"status", "segments", "missing_segments", "limit", "ordering")
		// Owner: service/story_trace_path.go serviceTraceSegment.
		requireExactKeys(t, propsOf(t, itemsOf(t, nodeOf(t, trace, "story.code_to_runtime_trace", "segments"), "story.code_to_runtime_trace.segments"), "story.code_to_runtime_trace.segments[]"),
			"story.code_to_runtime_trace.segments[]",
			"name", "status", "basis", "evidence_count", "evidence", "truncated")
	})

	t.Run("story sections", func(t *testing.T) {
		t.Parallel()
		top := propsOf(t, schema200(t, "/api/v0/services/{service_name}/story", "get"), "story")
		// Owner: service/story_overview.go buildServiceStorySectionsWithContext.
		requireExactKeys(t, propsOf(t, itemsOf(t, nodeOf(t, top, "story", "story_sections"), "story.story_sections"), "story.story_sections[]"),
			"story.story_sections[]", "title", "summary")
	})

	t.Run("story answer_metadata", func(t *testing.T) {
		t.Parallel()
		top := propsOf(t, schema200(t, "/api/v0/services/{service_name}/story", "get"), "story")
		// Owner: answer.AttachAnswerMetadata via entity.BuildServiceStoryEnvelope.
		requireExactKeys(t, propsOf(t, nodeOf(t, top, "story", "answer_metadata"), "story.answer_metadata"),
			"story.answer_metadata",
			"schema_version", "evidence_handles", "missing_evidence",
			"limitations", "truncated", "coverage", "partial_reasons",
			"recommended_next_calls")
	})

	t.Run("story investigation", func(t *testing.T) {
		t.Parallel()
		top := propsOf(t, schema200(t, "/api/v0/services/{service_name}/story", "get"), "story")
		// Owner: service/investigation_packet.go buildServiceInvestigationPacketWithContext.
		// environment/intent/question are sparse, so presence is asserted
		// only for the always-emitted keys.
		packet := propsOf(t, nodeOf(t, top, "story", "investigation"), "story.investigation")
		requireKeys(t, packet, "story.investigation",
			"service_name", "repositories_considered", "repositories_with_evidence",
			"evidence_families_found", "coverage_summary", "investigation_findings",
			"recommended_next_calls", "service_story_path", "service_context_path")
	})

	t.Run("packet truth", func(t *testing.T) {
		t.Parallel()
		schemas := packetSchemas(t)
		packet := child(t, schemas, "InvestigationEvidencePacket", "components")
		truth := propsOf(t, nodeOf(t, propsOf(t, packet, "packet"), "packet", "truth"), "packet.truth")
		// Owner: querycontract/truth.go TruthEnvelope via BuildTruthEnvelope.
		// level and freshness are always set; reason/truncated/omissions are
		// sparse, and capability/profile/basis/backend ride the envelope.
		requireKeys(t, truth, "packet.truth",
			"level", "capability", "profile", "basis", "backend", "freshness")
		fresh := propsOf(t, nodeOf(t, truth, "packet.truth", "freshness"), "packet.truth.freshness")
		requireKeys(t, fresh, "packet.truth.freshness", "state")
	})
}
