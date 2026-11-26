// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// TestContractShapesLaneB8Search pins the lane-B8 shape-completeness fixes
// for the search path fragments (ask, content, semantic, semantic_evidence)
// and the non-story entity routes (resolve, entity-context, workload-story,
// api-surface endpoints, downstream consumers). Every 200-response property
// must carry a description naming its emitting owner, no bare
// {"type": "object"} may stand without properties, and fixed key sets are
// pinned exactly. Each entry names the Go owner that emits the keys.
//
// Owners:
//   - ask: go/internal/query/ask/handler.go (askResponse, askArtifact,
//     traceEntry, askUnavailableResponse)
//   - content files/php: go/internal/query/contentread/content_handler.go
//     (contentSearchResponse), unscoped_search.go, php_config.go
//     (writePHPConfigSearch)
//   - content dockerfile: go/internal/query/contentread/
//     dockerfile_install_search.go (writeDockerfileInstallSearch)
//   - semantic: go/internal/query/semanticsearch/semantic_search.go and
//     semantic_search_rerank.go
//   - resolve: go/internal/query/entity/resolve_page.go
//     (resolvedEntityResponse) via Handler.ResolveEntity
//   - entity context: go/internal/query/entity/context_handler.go
//     (GetEntityContext) plus entitysemantics.AttachSemanticSummary
//   - workload story: go/internal/query/entity/workload_handlers.go
//     (GetWorkloadStory)
//   - api endpoints: go/internal/query/service/api_surface_list.go
//     (ListServiceAPISurfaceEndpoints) over
//     go/internal/query/repository/api_surface.go
//     (QueryRepoAPISurfacePaged/buildGraphAPISurface)
//   - downstream consumers: go/internal/query/entity/
//     service_downstream_consumers.go (GetServiceDownstreamConsumers) over
//     go/internal/query/service/downstream_consumers_list.go
func TestContractShapesLaneB8Search(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("spec has no paths object")
	}

	child := func(t *testing.T, node map[string]any, key, where string) map[string]any {
		t.Helper()
		next, ok := node[key].(map[string]any)
		if !ok {
			t.Fatalf("%s: missing or non-object property %q", where, key)
		}
		return next
	}

	schemaAt := func(t *testing.T, path, method string) map[string]any {
		t.Helper()
		route, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("spec paths missing %s", path)
		}
		op := child(t, route, method, path)
		responses := child(t, op, "responses", path+" "+method)
		okResp := child(t, responses, "200", path+" "+method)
		content, ok := okResp["content"].(map[string]any)
		if !ok {
			t.Fatalf("%s %s 200 response has no content (only a description)", path, method)
		}
		body := child(t, content, "application/json", path+" "+method)
		return child(t, body, "schema", path+" "+method)
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

	// requireKeys pins presence and rejects bare objects: an object
	// property needs properties or additionalProperties, and an array
	// property needs items (bare-object items rejected the same way).
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
			node, ok := props[key].(map[string]any)
			if !ok {
				t.Errorf("%s: missing property %q", where, key)
				continue
			}
			if desc, _ := node["description"].(string); desc == "" {
				t.Errorf("%s: property %q has no description", where, key)
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

	// requireExactKeys pins the closed property set of an object whose
	// owner emits a fixed key list: missing keys and phantom additions
	// both fail.
	requireExactKeys := func(t *testing.T, props map[string]any, where string, keys ...string) {
		t.Helper()
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

	t.Run("ask", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/ask", "post"), "ask")
		// askResponse in ask/handler.go emits exactly these keys.
		requireExactKeys(t, props, "ask",
			"answer_prose", "artifacts", "truth_class", "result_ref",
			"citation_ref", "result", "evidence_handles", "applied_facets",
			"query_trace", "partial", "limitations")
		requireDescribed(t, props, "ask",
			"answer_prose", "artifacts", "truth_class", "result_ref",
			"citation_ref", "result", "evidence_handles", "applied_facets",
			"query_trace", "partial", "limitations")
		// result is a bounded open projection of the canonical result.
		if _, ok := child(t, props, "result", "ask")["additionalProperties"]; !ok {
			t.Errorf("ask: property %q is an open map with no additionalProperties", "result")
		}
		artifactProps := propsOf(t, itemsOf(t, child(t, props, "artifacts", "ask"), "ask.artifacts"), "ask.artifacts[]")
		requireExactKeys(t, artifactProps, "ask.artifacts[]", "format", "content", "issues")
		requireDescribed(t, artifactProps, "ask.artifacts[]", "format", "content", "issues")
		// EvidenceCitationHandle in querycontract/evidence/citation_handle.go.
		handleProps := propsOf(t, itemsOf(t, child(t, props, "evidence_handles", "ask"), "ask.evidence_handles"), "ask.evidence_handles[]")
		requireExactKeys(t, handleProps, "ask.evidence_handles[]",
			"kind", "repo_id", "relative_path", "entity_id",
			"evidence_family", "reason", "start_line", "end_line", "confidence")
		requireDescribed(t, handleProps, "ask.evidence_handles[]",
			"kind", "repo_id", "relative_path", "entity_id",
			"evidence_family", "reason", "start_line", "end_line", "confidence")
		traceProps := propsOf(t, itemsOf(t, child(t, props, "query_trace", "ask"), "ask.query_trace"), "ask.query_trace[]")
		requireExactKeys(t, traceProps, "ask.query_trace[]",
			"tool", "args", "supported", "truth_class", "err")
		requireDescribed(t, traceProps, "ask.query_trace[]",
			"tool", "args", "supported", "truth_class", "err")
		if _, ok := child(t, traceProps, "args", "ask.query_trace[]")["additionalProperties"]; !ok {
			t.Errorf("ask.query_trace[]: property %q is an open map with no additionalProperties", "args")
		}
	})

	t.Run("content file search", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/content/files/search", "post"), "content files search")
		// contentSearchResponse plus the unscoped partial keys.
		requireKeys(t, props, "content files search",
			"results", "count", "limit", "offset", "truncated",
			"documents_not_searched", "documents_not_searched_state", "partial")
		requireDescribed(t, props, "content files search",
			"results", "count", "limit", "offset", "truncated",
			"documents_not_searched", "documents_not_searched_state", "partial")
		requireScalarTypes(t, props, "content files search", map[string]string{
			"count": "integer", "limit": "integer", "offset": "integer",
			"truncated": "boolean", "documents_not_searched": "integer",
			"documents_not_searched_state": "string",
		})
		partialProps := propsOf(t, child(t, props, "partial", "content files search"), "content files search.partial")
		requireExactKeys(t, partialProps, "content files search.partial",
			"reason", "rows_scanned_in_order", "rows_matched", "cursor",
			"budget_ms", "elapsed_ms", "overrun_ms", "progressed", "hint")
		requireDescribed(t, partialProps, "content files search.partial",
			"reason", "rows_scanned_in_order", "rows_matched", "cursor",
			"budget_ms", "elapsed_ms", "overrun_ms", "progressed", "hint")
		cursorProps := propsOf(t, child(t, partialProps, "cursor", "content files search.partial"), "content files search.partial.cursor")
		requireExactKeys(t, cursorProps, "content files search.partial.cursor", "repo_id", "relative_path")
		requireDescribed(t, cursorProps, "content files search.partial.cursor", "repo_id", "relative_path")
	})

	t.Run("content dockerfile installs", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/content/dockerfile-installs/search", "post"), "dockerfile installs")
		// writeDockerfileInstallSearch emits exactly these keys.
		requireExactKeys(t, props, "dockerfile installs",
			"package", "matches", "limit", "truncated", "next_cursor")
		requireDescribed(t, props, "dockerfile installs",
			"package", "matches", "limit", "truncated", "next_cursor")
		requireScalarTypes(t, props, "dockerfile installs", map[string]string{
			"package": "string", "limit": "integer", "truncated": "boolean",
		})
		rowProps := propsOf(t, itemsOf(t, child(t, props, "matches", "dockerfile installs"), "dockerfile installs.matches"), "dockerfile installs.matches[]")
		requireExactKeys(t, rowProps, "dockerfile installs.matches[]",
			"entity_id", "repo_id", "relative_path", "line_number",
			"name", "manager", "category", "stage", "version")
		requireDescribed(t, rowProps, "dockerfile installs.matches[]",
			"entity_id", "repo_id", "relative_path", "line_number",
			"name", "manager", "category", "stage", "version")
		cursorProps := propsOf(t, child(t, props, "next_cursor", "dockerfile installs"), "dockerfile installs.next_cursor")
		requireExactKeys(t, cursorProps, "dockerfile installs.next_cursor",
			"basis", "repo_id", "relative_path", "start_line", "entity_id")
		requireDescribed(t, cursorProps, "dockerfile installs.next_cursor",
			"basis", "repo_id", "relative_path", "start_line", "entity_id")
	})

	t.Run("content php config", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/content/php-config/search", "post"), "php config")
		// writePHPConfigSearch emits exactly these keys.
		requireExactKeys(t, props, "php config", "results", "count", "limit", "truncated", "next_cursor")
		requireDescribed(t, props, "php config", "results", "count", "limit", "truncated", "next_cursor")
		requireScalarTypes(t, props, "php config", map[string]string{
			"count": "integer", "limit": "integer", "truncated": "boolean", "next_cursor": "string",
		})
	})

	t.Run("semantic", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/search/semantic", "post"), "semantic")
		// semanticSearchResponse in semanticsearch/semantic_search.go.
		requireExactKeys(t, props, "semantic",
			"query", "repo_id", "anchor", "mode", "search_mode", "limit",
			"timeout_ms", "results", "truncated", "false_canonical_claim_count",
			"indexed_document_count", "corpus_limit", "corpus_may_be_truncated",
			"retrieval_state", "corpus_state", "facets", "rerank",
			"recommended_next_calls")
		requireDescribed(t, props, "semantic",
			"query", "repo_id", "anchor", "mode", "search_mode", "limit",
			"timeout_ms", "results", "truncated", "false_canonical_claim_count",
			"indexed_document_count", "corpus_limit", "corpus_may_be_truncated",
			"retrieval_state", "corpus_state", "facets", "rerank",
			"recommended_next_calls")
		anchorProps := propsOf(t, child(t, props, "anchor", "semantic"), "semantic.anchor")
		requireExactKeys(t, anchorProps, "semantic.anchor", "kind", "id")
		requireDescribed(t, anchorProps, "semantic.anchor", "kind", "id")
		resultProps := propsOf(t, itemsOf(t, child(t, props, "results", "semantic"), "semantic.results"), "semantic.results[]")
		requireExactKeys(t, resultProps, "semantic.results[]",
			"rank", "score", "search_method", "document", "graph_handles",
			"truth_scope", "freshness", "failures", "metadata", "ranking_basis")
		requireDescribed(t, resultProps, "semantic.results[]",
			"rank", "score", "search_method", "document", "graph_handles",
			"truth_scope", "freshness", "failures", "metadata", "ranking_basis")
		docProps := propsOf(t, child(t, resultProps, "document", "semantic.results[]"), "semantic.results[].document")
		requireExactKeys(t, docProps, "semantic.results[].document",
			"id", "repo_id", "source_kind", "title", "path", "context_text",
			"entity_refs", "graph_handles", "labels", "updated_at",
			"truth_scope", "freshness", "access_scope", "provenance")
		requireDescribed(t, docProps, "semantic.results[].document",
			"id", "repo_id", "source_kind", "title", "path", "context_text",
			"entity_refs", "graph_handles", "labels", "updated_at",
			"truth_scope", "freshness", "access_scope", "provenance")
		refProps := propsOf(t, itemsOf(t, child(t, docProps, "entity_refs", "semantic.results[].document"), "semantic.results[].document.entity_refs"), "semantic.results[].document.entity_refs[]")
		requireExactKeys(t, refProps, "semantic.results[].document.entity_refs[]",
			"id", "type", "name", "path", "start_line", "end_line")
		requireDescribed(t, refProps, "semantic.results[].document.entity_refs[]",
			"id", "type", "name", "path", "start_line", "end_line")
		for _, where := range []struct {
			name  string
			props map[string]any
		}{
			{"semantic.results[].graph_handles", propsOf(t, itemsOf(t, child(t, resultProps, "graph_handles", "semantic.results[]"), "semantic.results[].graph_handles"), "semantic.results[].graph_handles[]")},
			{"semantic.results[].document.graph_handles", propsOf(t, itemsOf(t, child(t, docProps, "graph_handles", "semantic.results[].document"), "semantic.results[].document.graph_handles"), "semantic.results[].document.graph_handles[]")},
		} {
			requireExactKeys(t, where.props, where.name, "kind", "id")
			requireDescribed(t, where.props, where.name, "kind", "id")
		}
		truthProps := propsOf(t, child(t, resultProps, "truth_scope", "semantic.results[]"), "semantic.results[].truth_scope")
		requireExactKeys(t, truthProps, "semantic.results[].truth_scope", "level", "basis")
		requireDescribed(t, truthProps, "semantic.results[].truth_scope", "level", "basis")
		freshProps := propsOf(t, child(t, resultProps, "freshness", "semantic.results[]"), "semantic.results[].freshness")
		requireExactKeys(t, freshProps, "semantic.results[].freshness", "state")
		requireDescribed(t, freshProps, "semantic.results[].freshness", "state")
		basisProps := propsOf(t, child(t, resultProps, "ranking_basis", "semantic.results[]"), "semantic.results[].ranking_basis")
		requireExactKeys(t, basisProps, "semantic.results[].ranking_basis",
			"baseline_rank", "baseline_score", "final_rank", "graph_boost", "contributions")
		requireDescribed(t, basisProps, "semantic.results[].ranking_basis",
			"baseline_rank", "baseline_score", "final_rank", "graph_boost", "contributions")
		contribProps := propsOf(t, itemsOf(t, child(t, basisProps, "contributions", "semantic.results[].ranking_basis"), "semantic.results[].ranking_basis.contributions"), "semantic.results[].ranking_basis.contributions[]")
		requireExactKeys(t, contribProps, "semantic.results[].ranking_basis.contributions[]", "kind", "handle", "weight")
		requireDescribed(t, contribProps, "semantic.results[].ranking_basis.contributions[]", "kind", "handle", "weight")
		callProps := propsOf(t, itemsOf(t, child(t, props, "recommended_next_calls", "semantic"), "semantic.recommended_next_calls"), "semantic.recommended_next_calls[]")
		requireExactKeys(t, callProps, "semantic.recommended_next_calls[]", "tool", "arguments", "reason")
		requireDescribed(t, callProps, "semantic.recommended_next_calls[]", "tool", "arguments", "reason")
		if _, ok := child(t, callProps, "arguments", "semantic.recommended_next_calls[]")["additionalProperties"]; !ok {
			t.Errorf("semantic.recommended_next_calls[]: property %q is an open map with no additionalProperties", "arguments")
		}
	})

	t.Run("semantic evidence refs", func(t *testing.T) {
		t.Parallel()
		// Both semantic-evidence routes answer 200 through shared
		// component schemas, so there is no inline object to complete.
		for _, path := range []string{
			"/api/v0/semantic/documentation-observations",
			"/api/v0/semantic/code-hints",
		} {
			schema := schemaAt(t, path, "get")
			if _, ok := schema["$ref"]; !ok {
				t.Errorf("%s: 200 schema is inline, expected a component $ref", path)
			}
		}
	})

	t.Run("entities resolve", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/entities/resolve", "post"), "resolve")
		// resolvedEntityResponse in entity/resolve_page.go emits exactly these.
		requireExactKeys(t, props, "resolve", "entities", "count", "limit", "truncated")
		requireDescribed(t, props, "resolve", "entities", "count", "limit", "truncated")
		requireScalarTypes(t, props, "resolve", map[string]string{
			"count": "integer", "limit": "integer", "truncated": "boolean",
		})
	})

	t.Run("entity context", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/entities/{entity_id}/context", "get"), "entity context")
		// GetEntityContext always emits the required keys; the semantic
		// blocks appear only when AttachSemanticSummary derives them.
		requireKeys(t, props, "entity context",
			"id", "labels", "name", "file_path", "language", "start_line",
			"end_line", "metadata", "semantic_summary", "semantic_profile",
			"python_semantics", "javascript_semantics", "typescript_semantics",
			"story", "repo_id", "repo_name", "relationships",
			"relationships_complete", "relationships_truncation_reason",
			"result_limits", "partial_reasons")
		requireDescribed(t, props, "entity context",
			"id", "labels", "name", "file_path", "language", "start_line",
			"end_line", "metadata", "semantic_summary", "semantic_profile",
			"python_semantics", "javascript_semantics", "typescript_semantics",
			"story", "repo_id", "repo_name", "relationships",
			"relationships_complete", "relationships_truncation_reason",
			"result_limits", "partial_reasons")
		requireScalarTypes(t, props, "entity context", map[string]string{
			"start_line": "integer", "end_line": "integer",
		})
		for _, key := range []string{"metadata", "semantic_profile", "python_semantics", "javascript_semantics", "typescript_semantics", "result_limits"} {
			if _, ok := child(t, props, key, "entity context")["additionalProperties"]; !ok {
				t.Errorf("entity context: property %q is an open map with no additionalProperties", key)
			}
		}
	})

	t.Run("workload story", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/workloads/{workload_id}/story", "get"), "workload story")
		// GetWorkloadStory plus AttachEvidenceBoundaries emit exactly these.
		requireExactKeys(t, props, "workload story",
			"workload_id", "name", "story", "result_limits",
			"partial_reasons", "evidence_boundaries")
		requireDescribed(t, props, "workload story",
			"workload_id", "name", "story", "result_limits",
			"partial_reasons", "evidence_boundaries")
	})

	t.Run("service api endpoints", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/services/{service_name}/api-surface/endpoints", "get"), "api endpoints")
		// buildGraphAPISurface plus the paged extras plus service_name.
		for _, key := range []string{
			"service_name", "truth_basis", "endpoint_count", "method_count",
			"operation_id_count", "endpoints", "source_paths", "spec_paths",
			"spec_versions", "api_versions", "detail_limit", "detail_truncated",
			"offset", "next_offset", "ordering", "frameworks", "framework_route_count",
		} {
			requireKeys(t, props, "api endpoints", key)
		}
		requireDescribed(t, props, "api endpoints",
			"service_name", "truth_basis", "endpoint_count", "method_count",
			"operation_id_count", "endpoints", "source_paths", "spec_paths",
			"spec_versions", "api_versions", "detail_limit", "detail_truncated",
			"offset", "next_offset", "ordering", "frameworks", "framework_route_count")
		rowProps := propsOf(t, itemsOf(t, child(t, props, "endpoints", "api endpoints"), "api endpoints.endpoints"), "api endpoints.endpoints[]")
		requireKeys(t, rowProps, "api endpoints.endpoints[]",
			"id", "path", "methods", "operation_ids", "source",
			"source_kinds", "source_paths", "evidence_source",
			"workload_id", "workload_name")
		requireDescribed(t, rowProps, "api endpoints.endpoints[]",
			"id", "path", "methods", "operation_ids", "source",
			"source_kinds", "source_paths", "evidence_source",
			"workload_id", "workload_name")
	})

	t.Run("service downstream consumers", func(t *testing.T) {
		t.Parallel()
		props := propsOf(t, schemaAt(t, "/api/v0/services/{service_name}/downstream-consumers", "get"), "downstream consumers")
		// GetServiceDownstreamConsumers body: required keys always present,
		// next_cursor only when a page follows, ceiling_reached only when
		// the merge cap fired.
		requireKeys(t, props, "downstream consumers",
			"service_name", "consumers", "truncated",
			"consumer_truncation_sources", "searched_hostnames",
			"effective_read_bound", "lower_bound", "ordering", "page_size",
			"next_cursor", "ceiling_reached")
		requireDescribed(t, props, "downstream consumers",
			"service_name", "consumers", "truncated",
			"consumer_truncation_sources", "searched_hostnames",
			"effective_read_bound", "lower_bound", "ordering", "page_size",
			"next_cursor", "ceiling_reached")
		sourceProps := propsOf(t, child(t, props, "consumer_truncation_sources", "downstream consumers"), "downstream consumers.consumer_truncation_sources")
		requireExactKeys(t, sourceProps, "downstream consumers.consumer_truncation_sources",
			"candidates", "evidence_files", "hostnames", "searches", "merge_cap")
		requireDescribed(t, sourceProps, "downstream consumers.consumer_truncation_sources",
			"candidates", "evidence_files", "hostnames", "searches", "merge_cap")
		cursorProps := propsOf(t, child(t, props, "next_cursor", "downstream consumers"), "downstream consumers.next_cursor")
		requireExactKeys(t, cursorProps, "downstream consumers.next_cursor", "repository", "repo_id")
		requireDescribed(t, cursorProps, "downstream consumers.next_cursor", "repository", "repo_id")
		if _, ok := itemsOf(t, child(t, props, "consumers", "downstream consumers"), "downstream consumers.consumers")["additionalProperties"]; !ok {
			t.Errorf("downstream consumers: property %q is an open map with no additionalProperties", "consumers")
		}
	})
}
