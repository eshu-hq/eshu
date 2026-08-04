// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNeo4jReaderGraphConfiguredReflectsDriverPresence is the #5761 F1 unit
// regression for Neo4jReader.GraphConfigured(). NewNeo4jReader(nil, database)
// always returns a non-nil *Neo4jReader -- cmd/api/wiring.go and
// cmd/mcp-server/wiring.go call NewNeo4jReader unconditionally, even when
// openQueryGraph returns a nil driver for local_lightweight or
// ESHU_DISABLE_NEO4J=true (cmd/api/wiring_graph.go) -- so "h.Neo4j != nil" can
// never distinguish a graph-backed reader from a driverless one.
// GraphConfigured is the seam that lets a caller ask.
func TestNeo4jReaderGraphConfiguredReflectsDriverPresence(t *testing.T) {
	t.Parallel()

	var nilReader *Neo4jReader
	if nilReader.GraphConfigured() {
		t.Fatal("nil *Neo4jReader.GraphConfigured() = true, want false")
	}

	driverless := NewNeo4jReader(nil, "nornic")
	if driverless.GraphConfigured() {
		t.Fatal("NewNeo4jReader(nil, \"nornic\").GraphConfigured() = true, want false")
	}
}

// TestHandleLanguageQueryUnconfiguredReaderServesContentBackedEntityType is
// the #5761 F1 regression proving local_lightweight's `local_lightweight:
// supported` capability-matrix row is honest. Before the fix, wiring.go's
// unconditional NewNeo4jReader(nil, ...) call made h.Neo4j non-nil even with
// no driver, so the "h.Neo4j == nil" content fallback in
// queryByLanguageWithSemanticFilter never fired: the handler fell through to
// Neo4jReader.Run, which failed with a bare (non-sentinel)
// "neo4j driver is required" error, producing a generic HTTP 500 instead of
// the content-served answer the matrix row promises for a graph-backed entity
// kind with a content-store equivalent (Function -> graphLabelToContentEntityType).
//
// Uses a REAL NewNeo4jReader(nil, "nornic"), not a test fake for GraphQuery,
// because the defect is specifically that a real driverless reader is
// non-nil -- a hand-rolled fake GraphQuery can't reproduce that shape.
func TestHandleLanguageQueryUnconfiguredReaderServesContentBackedEntityType(t *testing.T) {
	t.Parallel()

	content := &languageQueryContentStore{
		rows: []EntityContent{{
			EntityID:     "content-fn-1",
			RepoID:       "repo-1",
			RelativePath: "main.go",
			EntityType:   "Function",
			EntityName:   "main",
			StartLine:    1,
			EndLine:      3,
			Language:     "go",
		}},
	}
	handler := &LanguageQueryHandler{
		Neo4j:   NewNeo4jReader(nil, "nornic"),
		Content: content,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"go","entity_type":"function","repo_id":"repo-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var env ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, body=%s", err, w.Body.String())
	}
	if env.Truth == nil {
		t.Fatalf("truth envelope missing: body=%s", w.Body.String())
	}
	if got, want := env.Truth.Basis, TruthBasisContentIndex; got != want {
		t.Fatalf("truth.basis = %q, want %q", got, want)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map[string]any", env.Data)
	}
	if got, want := data["source_backend"], sourceBackendForTruthBasis(TruthBasisContentIndex); got != want {
		t.Fatalf("data.source_backend = %#v, want %#v", got, want)
	}
	if got, want := content.lastEntityType, "Function"; got != want {
		t.Fatalf("content entity type = %q, want %q", got, want)
	}
}

// TestHandleLanguageQueryUnconfiguredReaderReturns501ForGraphOnlyEntityType is
// the #5761 F1 residue regression: "repository", "directory", and "file" have
// no content-store equivalent (graphLabelToContentEntityType returns "" for
// all three -- language_query_entities.go), so an unconfigured graph reader
// cannot serve them at all. The handler must answer the bounded
// 501 unsupported_capability envelope naming symbol_graph.language_entities
// (the repo idiom at handler.go:137-145), not a generic 500 leaking the bare
// "neo4j driver is required" error.
func TestHandleLanguageQueryUnconfiguredReaderReturns501ForGraphOnlyEntityType(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j:   NewNeo4jReader(nil, "nornic"),
		Profile: ProfileLocalLightweight,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"go","entity_type":"repository","repo_id":"repo-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotImplemented; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if want := `"code":"unsupported_capability"`; !strings.Contains(w.Body.String(), want) {
		t.Fatalf("body = %s, want to contain %s", w.Body.String(), want)
	}
	if want := `"capability":"symbol_graph.language_entities"`; !strings.Contains(w.Body.String(), want) {
		t.Fatalf("body = %s, want to contain %s", w.Body.String(), want)
	}
	// P2-5: this residue 501 must name the actual cause (a graph-only entity
	// kind with no graph backend configured) rather than reusing the profile
	// gate's "language query requires a supported query profile" message --
	// under local_lightweight the catalog publishes this capability as
	// supported, so a caller seeing the profile-gate wording here would be
	// misled about why the request failed.
	if want := `"message":"entity type \"repository\" requires a graph backend"`; !strings.Contains(w.Body.String(), want) {
		t.Fatalf("body = %s, want to contain %s", w.Body.String(), want)
	}
	// #5761 F1 fix: the matrix entry for symbol_graph.language_entities must
	// carry RequiredProfile so requiredProfile() reports the tier that
	// actually serves this residue path (local_authoritative, the first tier
	// with a graph sidecar) instead of falling through to
	// requiredProfile's local_full_stack default -- local_lightweight already
	// publishes this capability as "supported", so naming local_full_stack
	// here would tell an operator at a supported profile to upgrade twice as
	// far as necessary.
	if want := `"current":"local_lightweight","required":"local_authoritative"`; !strings.Contains(w.Body.String(), want) {
		t.Fatalf("body = %s, want to contain %s", w.Body.String(), want)
	}
}

// TestHandleLanguageQueryUnconfiguredReaderGuardEntityTypeServesGuardsOnlyFromContent
// combines the #5761 F1 and F2 regressions for entity_type=="guard" under an
// unconfigured graph reader: the graph-first branch must skip the driverless
// Neo4jReader entirely (F1, extending the guard at language_queries.go:434)
// and, once on the content-store fallback, must filter by
// semantic_kind=guard rather than returning every Function (F2). content.rows
// deliberately holds one plain Function-shaped row to prove the content query
// was scoped by entity type "guard", not "Function" -- the production
// contentEntityTypeFilter (elixir_semantic_types.go) is what actually applies
// the semantic_kind predicate; this fake only proves the handler asks for the
// right entity type.
func TestHandleLanguageQueryUnconfiguredReaderGuardEntityTypeServesGuardsOnlyFromContent(t *testing.T) {
	t.Parallel()

	content := &languageQueryContentStore{
		rows: []EntityContent{{
			EntityID:     "content-guard-1",
			RepoID:       "repo-1",
			RelativePath: "lib/foo.ex",
			EntityType:   "Function",
			EntityName:   "is_valid?",
			StartLine:    3,
			EndLine:      5,
			Language:     "elixir",
			Metadata:     map[string]any{"semantic_kind": "guard"},
		}},
	}
	handler := &LanguageQueryHandler{
		Neo4j:   NewNeo4jReader(nil, "nornic"),
		Content: content,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		bytes.NewBufferString(`{"language":"elixir","entity_type":"guard","repo_id":"repo-1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := content.lastEntityType, "guard"; got != want {
		t.Fatalf("content entity type = %q, want %q (guard fallback must filter by semantic_kind=guard, not return every Function)", got, want)
	}
}
