// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package openapi

import (
	"encoding/json"
	"testing"
)

// Accuracy-code pins: every P1/P2 from the /api/v0/code OpenAPI-vs-handler
// audit lands here. Owner references name the Go handler, mapper, or struct
// that emits the keys each subtest asserts. All routes below are
// fragment-side only; no handler changed with this file.

func accCodeSpec(t *testing.T) map[string]any {
	t.Helper()
	var spec map[string]any
	if err := json.Unmarshal([]byte(Spec()), &spec); err != nil {
		t.Fatalf("unmarshal spec: %v", err)
	}
	return spec
}

func accCodeResponses(t *testing.T, paths map[string]any, path, method string) map[string]any {
	t.Helper()
	route, ok := paths[path].(map[string]any)
	if !ok {
		t.Fatalf("spec paths missing %s", path)
	}
	op := lane4Child(t, route, method, path)
	return lane4Child(t, op, "responses", path+" "+method)
}

func accCodeRequireStatus(t *testing.T, paths map[string]any, path, method, code string) {
	t.Helper()
	responses := accCodeResponses(t, paths, path, method)
	if _, ok := responses[code].(map[string]any); !ok {
		t.Errorf("%s %s: responses missing documented %q", path, method, code)
	}
}

func TestContractShapesAccCode(t *testing.T) {
	t.Parallel()
	paths := lane4SpecPaths(t)

	t.Run("documented 501 on profile-gated code routes", func(t *testing.T) {
		t.Parallel()
		// Owner: each handler writes 501 via WriteContractError on
		// unsupported profiles (e.g. codequery/symbol.go handleSymbolSearch,
		// structural_inventory.go, import_dependencies.go, graph_metrics.go,
		// topic.go, security_secrets.go, inspection.go,
		// relationship_handlers.go transitive-only, story_handlers.go,
		// callers.go, compare_paths.go, divergence.go,
		// divergence_investigate.go, divergence_report.go,
		// deadcode/results.go, deadcode/cross_repo.go,
		// deadcode/investigation.go).
		for _, route := range []string{
			"/api/v0/code/symbols/search",
			"/api/v0/code/structure/inventory",
			"/api/v0/code/imports/investigate",
			"/api/v0/code/call-graph/metrics",
			"/api/v0/code/topics/investigate",
			"/api/v0/code/security/secrets/investigate",
			"/api/v0/code/quality/inspect",
			"/api/v0/code/relationships",
			"/api/v0/code/relationships/story",
			"/api/v0/code/call-chain",
			"/api/v0/code/call-chain/compare",
			"/api/v0/code/divergence/findings",
			"/api/v0/code/divergence/investigate",
			"/api/v0/code/divergence/report",
			"/api/v0/code/dead-code",
			"/api/v0/code/dead-code/cross-repo",
			"/api/v0/code/dead-code/investigate",
		} {
			accCodeRequireStatus(t, paths, route, "post", "501")
		}
	})

	t.Run("codeowners effective_owner is optional and 500s documented", func(t *testing.T) {
		t.Parallel()
		// Owner: codeowners/ownership_precedence.go EffectiveRepositoryOwner
		// carries omitempty on both fields, and ListOwnership emits {} when
		// nothing resolves (ownership.go scoped-empty page); the two 500
		// paths are the ownership read failure and the effective-owner
		// resolution failure in codeowners/ownership.go.
		owners := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/codeowners/ownership", "get", "200"), "owners")
		effective := lane4Child(t, owners, "effective_owner", "owners")
		if _, ok := effective["required"]; ok {
			t.Error("owners.effective_owner: handler emits {} so properties must not be required")
		}
		accCodeRequireStatus(t, paths, "/api/v0/codeowners/ownership", "get", "500")
	})

	t.Run("relationships documents the ambiguous variant", func(t *testing.T) {
		t.Parallel()
		// Owner: codemodel/code_relationships_resolution.go
		// AmbiguousRelationshipsResponse returns status/target_resolution/
		// summary plus empty legs and omits the resolved-shape anchor keys.
		rels := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/relationships", "post", "200"), "relationships")
		lane4RequireKeys(t, rels, "relationships",
			"status", "target_resolution", "summary")
		lane4RequireDescribed(t, rels, "relationships",
			"status", "target_resolution", "summary")
	})

	t.Run("shared Relationship carries story-row keys", func(t *testing.T) {
		t.Parallel()
		// Owner: codequery/relationships/story GraphCypher (Neo4j) and
		// nornicDBRelationshipStoryGraphCypher (NornicDB) project
		// source_handle, target_handle, and why_trail_truncated on story
		// rows only; code-graph one-hop reads project none of the three.
		spec := accCodeSpec(t)
		components, ok := spec["components"].(map[string]any)
		if !ok {
			t.Fatal("spec has no components object")
		}
		schemas := lane4Child(t, components, "schemas", "components")
		rel := lane4Child(t, schemas, "Relationship", "schemas")
		props := lane4PropsOf(t, rel, "Relationship")
		lane4RequireKeys(t, props, "Relationship",
			"source_handle", "target_handle", "why_trail_truncated")
		lane4RequireDescribed(t, props, "Relationship",
			"source_handle", "target_handle", "why_trail_truncated")
	})

	t.Run("call-chain items are not EntityRef", func(t *testing.T) {
		t.Parallel()
		// Owner: chain/cypher.go projects {id, name, labels, language,
		// docstring, method_kind} per hop; responses.go
		// attachCallChainNodeSemantics adds metadata/semantic_summary only
		// when GraphResultMetadata is non-empty. file_path, start_line,
		// end_line, repo_id, and repo_name are never emitted.
		chain := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/call-chain", "post", "200"), "call-chain")
		chains := lane4ItemsOf(t, lane4Child(t, chain, "chains", "chain"), "chains")
		chainProps := lane4PropsOf(t, chains, "chains[]")
		chainArr := lane4Child(t, chainProps, "chain", "chains[]")
		hops := lane4PropsOf(t, lane4ItemsOf(t, chainArr, "chain"), "chain[]")
		lane4RequireKeys(t, hops, "call-chain.chains[].chain[]",
			"id", "name", "labels", "language", "docstring", "method_kind")
		lane4RequireDescribed(t, hops, "call-chain.chains[].chain[]",
			"id", "name", "labels", "language", "docstring", "method_kind")
		for _, absent := range []string{"file_path", "start_line", "end_line", "repo_id", "repo_name"} {
			if _, ok := hops[absent]; ok {
				t.Errorf("call-chain.chains[].chain[]: documents %q the handler never emits", absent)
			}
		}
	})

	t.Run("compare documents 404, 501, cross_repo refusal, id-name nodes", func(t *testing.T) {
		t.Parallel()
		// Owner: compare_paths.go answers 404 for unresolvable endpoints,
		// 501 on unsupported profiles, and 400 when cross_repo is set
		// (single-repo v1); path nodes are {id, name} only.
		accCodeRequireStatus(t, paths, "/api/v0/code/call-chain/compare", "post", "404")
		accCodeRequireStatus(t, paths, "/api/v0/code/call-chain/compare", "post", "501")
		compare := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/call-chain/compare", "post", "200"), "compare")
		comparePaths := lane4ItemsOf(t, lane4Child(t, compare, "paths", "compare"), "paths")
		compareProps := lane4PropsOf(t, comparePaths, "compare.paths[]")
		nodes := lane4PropsOf(t, lane4ItemsOf(t, lane4Child(t, compareProps, "nodes", "paths[]"), "nodes"), "nodes[]")
		lane4RequireExactKeys(t, nodes, "compare.paths[].nodes[]", "id", "name")
	})

	t.Run("complexity names complexity and result_key", func(t *testing.T) {
		t.Parallel()
		// Owner: complexity_queries.go single-entity answers include
		// complexity; list and empty answers include result_key.
		complexity := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/complexity", "post", "200"), "complexity")
		lane4RequireKeys(t, complexity, "complexity", "complexity", "result_key")
		lane4RequireDescribed(t, complexity, "complexity", "complexity", "result_key")
	})

	t.Run("inventory echoes scope filters", func(t *testing.T) {
		t.Parallel()
		// Owner: structural_inventory.go echoes file_path, symbol,
		// decorator, method_name, and class_name.
		inventory := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/structure/inventory", "post", "200"), "inventory")
		lane4RequireKeys(t, inventory, "inventory",
			"file_path", "symbol", "decorator", "method_name", "class_name")
		lane4RequireDescribed(t, inventory, "inventory",
			"file_path", "symbol", "decorator", "method_name", "class_name")
	})

	t.Run("inspect names source, backend, and result key", func(t *testing.T) {
		t.Parallel()
		// Owner: inspection.go answers source, source_backend, check,
		// repo_id, language, limit, offset, truncated, result_key,
		// thresholds, results, and recommended_next_calls.
		quality := lane4PropsOf(t, lane4SchemaAt(t, paths, "/api/v0/code/quality/inspect", "post", "200"), "quality")
		lane4RequireKeys(t, quality, "quality", "source", "source_backend", "result_key")
		lane4RequireDescribed(t, quality, "quality", "source", "source_backend", "result_key")
	})

	t.Run("search accepts search_type", func(t *testing.T) {
		t.Parallel()
		// Owner: codequery/handler.go handleSearch reads search_type
		// ("variable" selects the variable_lookup capability).
		route, ok := paths["/api/v0/code/search"].(map[string]any)
		if !ok {
			t.Fatal("spec paths missing /api/v0/code/search")
		}
		op := lane4Child(t, route, "post", "search")
		body := lane4Child(t, lane4Child(t, op, "requestBody", "search"), "content", "search")
		schema := lane4Child(t, lane4Child(t, body, "application/json", "search"), "schema", "search")
		props := lane4PropsOf(t, schema, "search")
		lane4RequireKeys(t, props, "search", "search_type")
		lane4RequireDescribed(t, props, "search", "search_type")
	})

	t.Run("language-query enum matches supported types", func(t *testing.T) {
		t.Parallel()
		// Owner: language/entities.go allSupportedEntityTypes unions the
		// graph-backed, content-backed, and graph-first maps (39 types).
		route, ok := paths["/api/v0/code/language-query"].(map[string]any)
		if !ok {
			t.Fatal("spec paths missing /api/v0/code/language-query")
		}
		op := lane4Child(t, route, "post", "language-query")
		body := lane4Child(t, lane4Child(t, op, "requestBody", "language-query"), "content", "language-query")
		schema := lane4Child(t, lane4Child(t, body, "application/json", "language-query"), "schema", "language-query")
		props := lane4PropsOf(t, schema, "language-query")
		entityType := lane4Child(t, props, "entity_type", "language-query")
		enum, ok := entityType["enum"].([]any)
		if !ok {
			t.Fatal("language-query.entity_type: no enum to pin")
		}
		got := make(map[string]bool, len(enum))
		for _, e := range enum {
			s, _ := e.(string)
			got[s] = true
		}
		for _, want := range []string{
			"repository", "directory", "file", "module", "function",
			"class", "struct", "enum", "union", "macro", "variable",
			"annotation", "protocol", "impl_block", "type_alias",
			"type_annotation", "typedef", "component", "guard",
			"protocol_implementation", "module_attribute",
			"terraform_module", "terraform_backend", "terraform_check",
			"terraform_import", "terraform_lock_provider",
			"terraform_moved_block", "terraform_removed_block",
			"terragrunt_config", "terragrunt_dependency",
			"terragrunt_local", "terragrunt_input",
			"sql_table", "sql_view", "sql_function", "sql_trigger",
			"sql_index", "sql_migration", "sql_column",
		} {
			if !got[want] {
				t.Errorf("language-query.entity_type: enum missing supported type %q", want)
			}
		}
		if len(enum) != 39 {
			t.Errorf("language-query.entity_type: enum has %d entries, want 39 supported types", len(enum))
		}
	})

	t.Run("topics accepts terms", func(t *testing.T) {
		t.Parallel()
		// Owner: codequery/topic.go Terms feeds CodeTopicSearchTerms.
		route, ok := paths["/api/v0/code/topics/investigate"].(map[string]any)
		if !ok {
			t.Fatal("spec paths missing /api/v0/code/topics/investigate")
		}
		op := lane4Child(t, route, "post", "topics")
		body := lane4Child(t, lane4Child(t, op, "requestBody", "topics"), "content", "topics")
		schema := lane4Child(t, lane4Child(t, body, "application/json", "topics"), "schema", "topics")
		props := lane4PropsOf(t, schema, "topics")
		lane4RequireKeys(t, props, "topics", "terms")
		lane4RequireDescribed(t, props, "topics", "terms")
	})

	t.Run("divergence findings accepts qualified kinds", func(t *testing.T) {
		t.Parallel()
		// Owner: divergence.go validate accepts the qualified
		// parallel_implementation.* spellings (codedivergence.Kind values).
		route, ok := paths["/api/v0/code/divergence/findings"].(map[string]any)
		if !ok {
			t.Fatal("spec paths missing /api/v0/code/divergence/findings")
		}
		op := lane4Child(t, route, "post", "divergence-findings")
		body := lane4Child(t, lane4Child(t, op, "requestBody", "findings"), "content", "findings")
		schema := lane4Child(t, lane4Child(t, body, "application/json", "findings"), "schema", "findings")
		props := lane4PropsOf(t, schema, "findings")
		kind := lane4Child(t, props, "kind", "findings")
		enum, ok := kind["enum"].([]any)
		if !ok {
			t.Fatal("divergence-findings.kind: no enum to pin")
		}
		got := make(map[string]bool, len(enum))
		for _, e := range enum {
			s, _ := e.(string)
			got[s] = true
		}
		for _, want := range []string{
			"parallel_implementation.exact",
			"parallel_implementation.renamed",
			"parallel_implementation.drifted",
			"parallel_implementation.wrapper_bypass",
			"parallel_implementation.convention_outlier",
		} {
			if !got[want] {
				t.Errorf("divergence-findings.kind: enum missing accepted spelling %q", want)
			}
		}
	})

	t.Run("cypher and visualize document 403", func(t *testing.T) {
		t.Parallel()
		// Owner: auth/scoped_routes_shared_key_only.go rejects scoped and
		// browser-session tokens before either handler runs.
		accCodeRequireStatus(t, paths, "/api/v0/code/cypher", "post", "403")
		accCodeRequireStatus(t, paths, "/api/v0/code/visualize", "post", "403")
	})
}
