// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import "testing"

func TestContractShapesLane4Code(t *testing.T) {
	t.Parallel()
	paths := lane4SpecPaths(t)

	t.Run("cypher and visualize name owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: codequery/cypher.go handleCypherQuery writes the raw Neo4j
		// driver rows: keys are the caller query's RETURN aliases, so the
		// rows are a genuine open map, never a fixed key set.
		cypher := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/cypher", "post", "200"), "cypher")
		results := lane4Child(t, cypher, "results", "cypher")
		items, ok := results["items"].(map[string]any)
		if !ok {
			t.Fatal("cypher.results: array schema has no items")
		}
		if items["type"] != "object" {
			t.Errorf("cypher.results[]: has type %v, want object", items["type"])
		}
		if _, ok := items["additionalProperties"]; !ok {
			t.Error("cypher.results[]: caller-keyed rows need additionalProperties")
		}
		if desc, _ := results["description"].(string); desc == "" {
			t.Error("cypher.results: no description naming the caller-alias key semantics")
		}
		lane4RequireDescribed(t, cypher, "cypher", "results", "limit", "truncated")
		// Owner: visualization/packet.go VisualizationPacket (marshaled by
		// handleVisualizeQuery) plus the row-window limit/truncated.
		visualize := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/visualize", "post", "200"), "visualize")
		packet := lane4PropsOf(t, lane4Child(t, visualize, "visualization_packet", "visualize"), "packet")
		lane4RequireKeys(t, packet, "packet",
			"view", "title", "supported", "nodes", "edges", "truth", "limits",
			"truncation", "limitations", "recommended_next_calls")
		lane4RequireDescribed(t, packet, "packet",
			"view", "title", "supported", "nodes", "edges", "truth", "limits",
			"truncation", "limitations", "recommended_next_calls")
		nodes := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, packet, "nodes", "packet"), "nodes"), "nodes[]")
		lane4RequireKeys(t, nodes, "packet.nodes[]", "id", "type", "label")
		lane4RequireScalarTypes(t, nodes, "packet.nodes[]", map[string]string{
			"id": "string", "type": "string", "label": "string",
		})
		edges := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, packet, "edges", "packet"), "edges"), "edges[]")
		lane4RequireKeys(t, edges, "packet.edges[]", "id", "source", "target", "relationship")
		limits := lane4PropsOf(t, lane4Child(t, packet, "limits", "packet"), "packet.limits")
		lane4RequireExactKeys(t, limits, "packet.limits",
			"max_nodes", "max_edges", "ordering", "node_count", "edge_count")
		truncation := lane4PropsOf(t, lane4Child(t, packet, "truncation", "packet"), "packet.truncation")
		lane4RequireExactKeys(t, truncation, "packet.truncation",
			"truncated", "dropped_node_count", "dropped_edge_count", "dropped_node_ids")
	})

	t.Run("topics and bundles name owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: answer/metadata.go AnswerMetadata attached by
		// codequery/topic.go: the normalized additive companion.
		topics := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/topics/investigate", "post", "200"), "topics")
		meta := lane4PropsOf(t, lane4Child(t, topics, "answer_metadata", "topics"), "answer_metadata")
		lane4RequireExactKeys(t, meta, "topics.answer_metadata",
			"schema_version", "evidence_handles", "missing_evidence",
			"limitations", "truncated", "coverage", "partial_reasons",
			"recommended_next_calls")
		lane4RequireDescribed(t, meta, "topics.answer_metadata",
			"schema_version", "evidence_handles", "missing_evidence",
			"limitations", "truncated", "coverage", "partial_reasons",
			"recommended_next_calls")
		lane4RequireDescribed(t, topics, "topics",
			"topic", "intent", "scope", "searched_terms", "matched_files",
			"matched_symbols", "evidence_groups", "call_graph_handles",
			"recommended_next_calls", "count", "limit", "offset", "truncated",
			"source_backend", "coverage", "answer_metadata")
		// Owner: codequery/registry_bundles.go writes bundles, count, limit,
		// truncated for the bounded package-registry page.
		bundles := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/bundles", "post", "200"), "bundles")
		lane4RequireExactKeys(t, bundles, "bundles", "bundles", "count", "limit", "truncated")
		lane4RequireDescribed(t, bundles, "bundles", "bundles", "count", "limit", "truncated")
	})

	t.Run("divergence packets name owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: codequery/divergence.go handleDivergenceFindings,
		// divergence_report.go handleDivergenceReport, and
		// divergence_investigate.go handleDivergenceInvestigate.
		findings := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/divergence/findings", "post", "200"), "divergence-findings")
		lane4RequireExactKeys(t, findings, "divergence-findings",
			"repo_id", "kind", "limit", "offset", "findings", "count",
			"truncated", "next_offset", "suppressions", "source_backend")
		lane4RequireDescribed(t, findings, "divergence-findings",
			"repo_id", "kind", "limit", "offset", "findings", "count",
			"truncated", "next_offset", "suppressions", "source_backend")
		// Owner: codedivergence/results.go ResultMap emits finding_id, kind,
		// fingerprint, score, members, and reasons per finding.
		findingItems := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, findings, "findings", "findings"), "findings"), "findings[]")
		lane4RequireKeys(t, findingItems, "findings[]",
			"finding_id", "kind", "fingerprint", "score", "members", "reasons")
		report := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/divergence/report", "post", "200"), "divergence-report")
		lane4RequireExactKeys(t, report, "divergence-report",
			"repo_id", "counts", "total", "top", "top_per_kind", "truncated",
			"suppressions", "source_backend")
		lane4RequireDescribed(t, report, "divergence-report",
			"repo_id", "counts", "total", "top", "top_per_kind", "truncated",
			"suppressions", "source_backend")
		for _, key := range []string{"counts", "truncated", "suppressions"} {
			node, ok := report[key].(map[string]any)
			if !ok {
				t.Fatalf("divergence-report: missing property %q", key)
			}
			if _, ok := node["additionalProperties"]; !ok {
				t.Errorf("divergence-report: %q needs kind-keyed additionalProperties", key)
			}
			if desc, _ := node["description"].(string); desc == "" {
				t.Errorf("divergence-report: %q has no description naming the kind keys", key)
			}
		}
		investigate := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/divergence/investigate", "post", "200"), "divergence-investigate")
		lane4RequireExactKeys(t, investigate, "divergence-investigate",
			"repo_id", "finding", "next_steps", "truncated", "suppressions", "source_backend")
		lane4RequireDescribed(t, investigate, "divergence-investigate",
			"repo_id", "finding", "next_steps", "truncated", "suppressions", "source_backend")
	})

	t.Run("owners quality secrets name owner keys", func(t *testing.T) {
		t.Parallel()
		// Owner: codeowners/ownership.go emits the bounded CODEOWNERS page.
		owners := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/codeowners/ownership", "get", "200"), "owners")
		lane4RequireExactKeys(t, owners, "owners",
			"ownership", "repository_id", "count", "limit", "truncated",
			"next_cursor", "effective_owner")
		lane4RequireDescribed(t, owners, "owners",
			"ownership", "repository_id", "count", "limit", "truncated",
			"next_cursor", "effective_owner")
		rows := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, owners, "ownership", "owners"), "ownership"), "ownership[]")
		lane4RequireExactKeys(t, rows, "owners.ownership[]",
			"pattern", "source_path", "order_index", "owner_ref")
		lane4RequireScalarTypes(t, rows, "owners.ownership[]", map[string]string{
			"pattern": "string", "source_path": "string",
			"order_index": "integer", "owner_ref": "string",
		})
		cursor := lane4PropsOf(t, lane4Child(t, owners, "next_cursor", "owners"), "next_cursor")
		lane4RequireExactKeys(t, cursor, "owners.next_cursor",
			"after_order_index", "after_pattern", "after_ref")
		effective := lane4PropsOf(t, lane4Child(t, owners, "effective_owner", "owners"), "effective_owner")
		lane4RequireExactKeys(t, effective, "owners.effective_owner", "owner_ref", "source")
		// Owner: codequery/inspection.go echoes the check selectors and the
		// thresholds/results the quality projector returns.
		quality := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/quality/inspect", "post", "200"), "quality")
		lane4RequireDescribed(t, quality, "quality",
			"check", "repo_id", "language", "limit", "offset", "truncated",
			"thresholds", "results", "recommended_next_calls")
		// Owner: codequery/security_secrets.go hardcodedSecretResponse emits
		// scope, finding_kinds, findings, recommended_next_calls, count,
		// limit, offset, truncated, source_backend, and coverage.
		secrets := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/security/secrets/investigate", "post", "200"), "secrets")
		lane4RequireExactKeys(t, secrets, "secrets",
			"scope", "finding_kinds", "findings", "recommended_next_calls",
			"count", "limit", "offset", "truncated", "source_backend", "coverage")
		lane4RequireDescribed(t, secrets, "secrets",
			"scope", "finding_kinds", "findings", "recommended_next_calls",
			"count", "limit", "offset", "truncated", "source_backend", "coverage")
		// Owner: hardcodedSecretFinding emits rank, repo_id, relative_path,
		// language, line_number, finding_kind, confidence, severity,
		// redacted_excerpt, suppressed, suppression_notes, source_handle.
		secretRows := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, secrets, "findings", "secrets"), "findings"), "findings[]")
		lane4RequireKeys(t, secretRows, "secrets.findings[]",
			"rank", "repo_id", "relative_path", "language", "line_number",
			"finding_kind", "confidence", "severity", "redacted_excerpt",
			"suppressed", "suppression_notes", "source_handle")
		lane4RequireDescribed(t, secretRows, "secrets.findings[]",
			"rank", "repo_id", "relative_path", "language", "line_number",
			"finding_kind", "confidence", "severity", "redacted_excerpt",
			"suppressed", "suppression_notes", "source_handle")
		coverage := lane4PropsOf(t, lane4Child(t, secrets, "coverage", "secrets"), "coverage")
		lane4RequireKeys(t, coverage, "secrets.coverage",
			"query_shape", "returned_count", "suppressed_count",
			"include_suppressed", "limit", "offset", "truncated",
			"redaction", "empty", "searched_all_kinds", "requires_repo_scope",
			"read_path")
	})

	t.Run("relationship echoes name owner keys", func(t *testing.T) {
		t.Parallel()
		rels := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/relationships", "post", "200"), "relationships")
		lane4RequireDescribed(t, rels, "relationships",
			"entity_id", "name", "labels", "file_path", "repo_id", "repo_name",
			"language", "start_line", "end_line", "metadata", "outgoing", "incoming")
		story := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/relationships/story", "post", "200"), "story")
		lane4RequireDescribed(t, story, "story",
			"target_resolution", "scope", "relationships", "class_hierarchy",
			"override_story", "summary", "coverage", "source_backend")
		chain := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/call-chain", "post", "200"), "call-chain")
		lane4RequireDescribed(t, chain, "call-chain",
			"start", "end", "start_entity_id", "end_entity_id", "repo_id",
			"cross_repo", "start_repo_id", "end_repo_id", "chains")
		chainRows := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, chain, "chains", "chain"), "chains"), "chains[]")
		lane4RequireExactKeys(t, chainRows, "call-chain.chains[]", "depth", "chain")
		compare := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/call-chain/compare", "post", "200"), "compare")
		lane4RequireDescribed(t, compare, "compare",
			"start", "end", "start_entity_id", "end_entity_id", "repo_id",
			"paths", "truncated", "max_depth", "max_paths", "visited", "source_backend")
		complexity := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/complexity", "post", "200"), "complexity")
		lane4RequireDescribed(t, complexity, "complexity",
			"entity_id", "name", "labels", "file_path", "repo_id", "repo_name",
			"language", "start_line", "end_line", "metadata", "outgoing_count",
			"incoming_count", "total_relationships", "results", "limit",
			"truncated", "docstring_clip_bytes", "docstring_clipped_rows")
		callers := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/routes/callers", "post", "200"), "callers")
		lane4RequireDescribed(t, callers, "callers",
			"status", "partial", "truncated", "unsupported", "truth_source",
			"route", "handler", "callers", "callees", "impact", "max_depth", "limit")
		inventory := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/structure/inventory", "post", "200"), "inventory")
		lane4RequireDescribed(t, inventory, "inventory",
			"repo_id", "language", "inventory_kind", "entity_kind", "results",
			"count", "limit", "offset", "truncated", "next_offset",
			"source_backend", "source_cache_clip_bytes",
			"source_cache_clipped_rows", "docstring_clip_bytes", "docstring_clipped_rows")
		imports := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/imports/investigate", "post", "200"), "imports")
		lane4RequireDescribed(t, imports, "imports",
			"query_type", "scope", "dependencies", "modules", "cycles",
			"cross_module_calls", "count", "limit", "offset", "truncated",
			"has_more", "next_offset", "source_backend", "coverage")
		metrics := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/call-graph/metrics", "post", "200"), "metrics")
		lane4RequireDescribed(t, metrics, "metrics",
			"metric_type", "scope", "functions", "count", "limit", "offset",
			"truncated", "next_offset", "source_backend", "coverage")
	})
}
