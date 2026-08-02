// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// This file is the #5761 P1-1/P1-2 review-fix regression. Before this fix,
// handleLanguageQuery's graphBackedEntityTypes branch always reported
// TruthBasisAuthoritativeGraph even when queryByLanguageWithSemanticFilter
// actually served the answer from the content store (h.Neo4j == nil) or
// merged content-store metadata into the graph rows -- and the two
// graph-first branches (graphFirstContentBackedEntityTypes and "guard")
// always reported a hardcoded TruthBasisHybrid regardless of which backend
// actually served the result. These tests bind each dispatch branch to the
// specific basis/level/reason/source_backend combination it must produce for
// a given, deterministic backend configuration.

// TestHandleLanguageQueryGraphBackedBranchReportsHybridWhenContentEnriches
// proves the graphBackedEntityTypes branch reports TruthBasisHybrid (not the
// old hardcoded TruthBasisAuthoritativeGraph) once
// enrichLanguageResultsWithContentMetadata actually merges a content-store
// value into the graph row.
func TestHandleLanguageQueryGraphBackedBranchReportsHybridWhenContentEnriches(t *testing.T) {
	t.Parallel()

	content := &languageQueryContentStore{
		rows: []EntityContent{{
			EntityID:     "content-1",
			RepoID:       "repo-1",
			RelativePath: "src/handler.py",
			EntityType:   "Function",
			EntityName:   "handler",
			StartLine:    12,
			EndLine:      20,
			Language:     "python",
			Metadata:     map[string]any{"docstring": "Handles the request."},
		}},
	}
	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":  "graph-1",
				"name":       "handler",
				"labels":     []string{"Function"},
				"file_path":  "src/handler.py",
				"repo_id":    "repo-1",
				"language":   "python",
				"start_line": int64(12),
				"end_line":   int64(20),
			},
		}},
		Content: content,
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		strings.NewReader(`{"language":"python","entity_type":"function","query":"handler","repo_id":"repo-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.handleLanguageQuery(rec, req)

	envelope := decodeLanguageQueryEnvelope(t, rec)
	if envelope.Truth == nil {
		t.Fatalf("truth envelope missing, body = %s", rec.Body.String())
	}
	if envelope.Truth.Basis != TruthBasisHybrid {
		t.Fatalf("truth.basis = %q, want %q (content enrichment merged into the graph row): body=%s", envelope.Truth.Basis, TruthBasisHybrid, rec.Body.String())
	}
	if envelope.Truth.Level != TruthLevelDerived {
		t.Fatalf("truth.level = %q, want %q", envelope.Truth.Level, TruthLevelDerived)
	}
	// #5761 P2-1: languageQueryGraphBackedReason's TruthBasisHybrid branch is
	// otherwise unfalsifiable -- collapsing the whole function to its default
	// branch (reasonLanguageQueryGraphOnly, no "enriched" clause) left the
	// package green because nothing asserted this branch's reason text.
	if got, want := envelope.Truth.Reason, "graph-only read served this entity type, enriched with content-store metadata"; got != want {
		t.Fatalf("truth.reason = %q, want %q", got, want)
	}
	if want := `"source_backend":"hybrid_graph_and_content"`; !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("body = %s, want %s", rec.Body.String(), want)
	}
}

// TestHandleLanguageQueryGraphBackedBranchFallsBackToContentIndexWhenNeo4jNil
// proves the graphBackedEntityTypes branch reports TruthBasisContentIndex
// (not the old hardcoded TruthBasisAuthoritativeGraph) when h.Neo4j is nil --
// the content store served the entire read, with no graph involved at all.
//
// This covers a defensive path, not a production configuration: both wirings
// construct query.NewNeo4jReader unconditionally (cmd/api/wiring.go,
// cmd/mcp-server/wiring.go), so a shipped handler always has a non-nil
// GraphQuery. The branch is still worth pinning because the basis it reports
// decides whether classifyAnswerTruth may treat the answer as fact, so a
// future wiring that does leave Neo4j nil must not silently claim
// authoritative_graph.
func TestHandleLanguageQueryGraphBackedBranchFallsBackToContentIndexWhenNeo4jNil(t *testing.T) {
	t.Parallel()

	content := &languageQueryContentStore{
		rows: []EntityContent{{
			EntityID:     "content-1",
			RepoID:       "repo-1",
			RelativePath: "src/handler.py",
			EntityType:   "Function",
			EntityName:   "handler",
			Language:     "python",
		}},
	}
	handler := &LanguageQueryHandler{Content: content}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		strings.NewReader(`{"language":"python","entity_type":"function","query":"handler","repo_id":"repo-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.handleLanguageQuery(rec, req)

	envelope := decodeLanguageQueryEnvelope(t, rec)
	if envelope.Truth == nil {
		t.Fatalf("truth envelope missing, body = %s", rec.Body.String())
	}
	if envelope.Truth.Basis != TruthBasisContentIndex {
		t.Fatalf("truth.basis = %q, want %q (no graph reader configured; content-store served the read): body=%s", envelope.Truth.Basis, TruthBasisContentIndex, rec.Body.String())
	}
	if envelope.Truth.Level != TruthLevelDerived {
		t.Fatalf("truth.level = %q, want %q", envelope.Truth.Level, TruthLevelDerived)
	}
	// #5761 P2-1: languageQueryGraphBackedReason's TruthBasisContentIndex
	// branch is otherwise unfalsifiable -- collapsing the whole function to
	// its default branch left the package green because nothing asserted
	// this branch's reason text.
	if got, want := envelope.Truth.Reason, "no graph reader was configured for this entity type; the content-store fallback served the result"; got != want {
		t.Fatalf("truth.reason = %q, want %q", got, want)
	}
	if want := `"source_backend":"postgres_content_store"`; !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("body = %s, want %s", rec.Body.String(), want)
	}
}

// TestHandleLanguageQueryGraphBackedBranchReportsPlainGraphOnlyReasonOnPureGraphHit
// covers languageQueryGraphBackedReason's third (default) branch (#5761
// P2-1): a pure graph hit with no content store configured to merge. Before
// this test, only the TruthBasisHybrid and TruthBasisContentIndex branches
// above had a coverage path at all for this function -- the default branch's
// reasonLanguageQueryGraphOnly text was never asserted, so a mutation
// collapsing the whole switch to this branch (its own default!) would have
// been invisible from any of the other tests, since it exactly matches what
// they'd see if they asserted nothing. This proves the plain reason text
// exactly, including the absence of the Hybrid branch's ", enriched with
// content-store metadata" suffix.
func TestHandleLanguageQueryGraphBackedBranchReportsPlainGraphOnlyReasonOnPureGraphHit(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id": "func-1",
				"name":      "handler",
				"labels":    []string{"Function"},
				"file_path": "src/handler.py",
				"repo_id":   "repo-1",
				"language":  "python",
			},
		}},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		strings.NewReader(`{"language":"python","entity_type":"function","query":"handler","repo_id":"repo-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.handleLanguageQuery(rec, req)

	envelope := decodeLanguageQueryEnvelope(t, rec)
	if envelope.Truth == nil {
		t.Fatalf("truth envelope missing, body = %s", rec.Body.String())
	}
	if envelope.Truth.Basis != TruthBasisAuthoritativeGraph {
		t.Fatalf("truth.basis = %q, want %q (pure graph hit, no content store configured): body=%s", envelope.Truth.Basis, TruthBasisAuthoritativeGraph, rec.Body.String())
	}
	if got, want := envelope.Truth.Reason, "graph-only read served this entity type"; got != want {
		t.Fatalf("truth.reason = %q, want %q", got, want)
	}
	if want := `"source_backend":"graph"`; !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("body = %s, want %s", rec.Body.String(), want)
	}
}

// TestHandleLanguageQueryGuardBranchReportsAuthoritativeGraphOnPureGraphHit is
// the #5761 P1-2 regression for the "guard" entity type: before this fix the
// guard branch always reported the hardcoded TruthBasisHybrid, so mutating
// that literal to TruthBasisAuthoritativeGraph left the whole suite green.
// This binds the guard branch specifically to TruthBasisAuthoritativeGraph
// when Neo4j returns rows and no content store is configured to merge.
func TestHandleLanguageQueryGuardBranchReportsAuthoritativeGraphOnPureGraphHit(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id": "guard-1",
				"name":      "isValid",
				"labels":    []string{"Function"},
				"file_path": "src/guard.go",
				"repo_id":   "repo-1",
				"language":  "go",
			},
		}},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		strings.NewReader(`{"language":"go","entity_type":"guard","query":"isValid","repo_id":"repo-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.handleLanguageQuery(rec, req)

	envelope := decodeLanguageQueryEnvelope(t, rec)
	if envelope.Truth == nil {
		t.Fatalf("truth envelope missing, body = %s", rec.Body.String())
	}
	if envelope.Truth.Basis != TruthBasisAuthoritativeGraph {
		t.Fatalf("truth.basis = %q, want %q (pure graph hit, no content store configured): body=%s", envelope.Truth.Basis, TruthBasisAuthoritativeGraph, rec.Body.String())
	}
	if envelope.Truth.Level != TruthLevelExact {
		t.Fatalf("truth.level = %q, want %q", envelope.Truth.Level, TruthLevelExact)
	}
	// #5761 P2-1: exact match pins languageQueryGraphFirstReason's default
	// branch, including the absence of the TruthBasisContentIndex branch's
	// "returned no rows" clause and the TruthBasisHybrid branch's "enriched
	// with content-store metadata" clause -- both would otherwise survive a
	// mutation collapsing the switch to this same default branch.
	if got, want := envelope.Truth.Reason, "graph-first content-fallback read with a semantic_kind=guard filter; the graph served the result"; got != want {
		t.Fatalf("truth.reason = %q, want %q", got, want)
	}
	if want := `"source_backend":"graph"`; !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("body = %s, want %s", rec.Body.String(), want)
	}
}

// TestHandleLanguageQueryGuardBranchReportsHybridReasonWhenContentEnriches
// covers languageQueryGraphFirstReason's TruthBasisHybrid branch (#5761
// P2-1) for the "guard" call site: the graph serves the result and content
// enrichment merges at least one value in. Before this test, only the
// default (pure graph hit) and TruthBasisContentIndex (graph-empty fallback)
// branches had coverage for this function -- the Hybrid branch's ", enriched
// with content-store metadata" suffix was never asserted anywhere.
func TestHandleLanguageQueryGuardBranchReportsHybridReasonWhenContentEnriches(t *testing.T) {
	t.Parallel()

	content := &languageQueryContentStore{
		rows: []EntityContent{{
			EntityID:     "content-1",
			RepoID:       "repo-1",
			RelativePath: "src/guard.go",
			EntityType:   "Function",
			EntityName:   "isValid",
			StartLine:    1,
			Language:     "go",
			Metadata:     map[string]any{"docstring": "Reports whether the input is valid."},
		}},
	}
	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id":  "guard-1",
				"name":       "isValid",
				"labels":     []string{"Function"},
				"file_path":  "src/guard.go",
				"repo_id":    "repo-1",
				"language":   "go",
				"start_line": int64(1),
			},
		}},
		Content: content,
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		strings.NewReader(`{"language":"go","entity_type":"guard","query":"isValid","repo_id":"repo-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.handleLanguageQuery(rec, req)

	envelope := decodeLanguageQueryEnvelope(t, rec)
	if envelope.Truth == nil {
		t.Fatalf("truth envelope missing, body = %s", rec.Body.String())
	}
	if envelope.Truth.Basis != TruthBasisHybrid {
		t.Fatalf("truth.basis = %q, want %q (content enrichment merged into the graph row): body=%s", envelope.Truth.Basis, TruthBasisHybrid, rec.Body.String())
	}
	if got, want := envelope.Truth.Reason, "graph-first content-fallback read with a semantic_kind=guard filter; the graph served the result, enriched with content-store metadata"; got != want {
		t.Fatalf("truth.reason = %q, want %q", got, want)
	}
	if want := `"source_backend":"hybrid_graph_and_content"`; !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("body = %s, want %s", rec.Body.String(), want)
	}
}

// TestHandleLanguageQuerySqlTableBranchReportsAuthoritativeGraphOnPureGraphHit
// is the #5761 P1-2 regression for a graphFirstContentBackedEntityTypes
// member ("sql_table"): before this fix this branch always reported the
// hardcoded TruthBasisHybrid, so mutating that literal to
// TruthBasisAuthoritativeGraph left the whole suite green. This binds the
// sql_table branch specifically to TruthBasisAuthoritativeGraph when Neo4j
// returns rows and no content store is configured to merge, and proves its
// reason omits the guard branch's semantic_kind=guard filter note.
func TestHandleLanguageQuerySqlTableBranchReportsAuthoritativeGraphOnPureGraphHit(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: &mockLanguageQueryGraphReader{rows: []map[string]any{
			{
				"entity_id": "table-1",
				"name":      "users",
				"labels":    []string{"SqlTable"},
				"file_path": "db/schema.sql",
				"repo_id":   "repo-1",
				"language":  "sql",
			},
		}},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		strings.NewReader(`{"language":"sql","entity_type":"sql_table","query":"users","repo_id":"repo-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.handleLanguageQuery(rec, req)

	envelope := decodeLanguageQueryEnvelope(t, rec)
	if envelope.Truth == nil {
		t.Fatalf("truth envelope missing, body = %s", rec.Body.String())
	}
	if envelope.Truth.Basis != TruthBasisAuthoritativeGraph {
		t.Fatalf("truth.basis = %q, want %q (pure graph hit, no content store configured): body=%s", envelope.Truth.Basis, TruthBasisAuthoritativeGraph, rec.Body.String())
	}
	if envelope.Truth.Level != TruthLevelExact {
		t.Fatalf("truth.level = %q, want %q", envelope.Truth.Level, TruthLevelExact)
	}
	// #5761 P2-1: exact match pins languageQueryGraphFirstReason's default
	// branch for a graphFirstContentBackedEntityTypes member (no filterNote),
	// including the absence of the TruthBasisContentIndex branch's "returned
	// no rows" clause and the TruthBasisHybrid branch's "enriched with
	// content-store metadata" clause.
	if got, want := envelope.Truth.Reason, "graph-first content-fallback read; the graph served the result"; got != want {
		t.Fatalf("truth.reason = %q, want %q", got, want)
	}
	if want := `"source_backend":"graph"`; !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("body = %s, want %s", rec.Body.String(), want)
	}
}
