// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// This file is the #5761 F1 regression: handleLanguageQuery's four success
// paths used to write a bare WriteJSON(w, http.StatusOK, ...) body, so none of
// the route's capability-matrix claims (specs/capability-matrix/language-
// entities.v1.yaml's per-profile max_truth_level rows) were observable on the
// wire -- an envelope-accepting caller had no way to see whether an answer was
// authoritative_graph/exact or a demoted/derived one. AGENTS.md's "Add a new
// capability" recipe requires calling BuildTruthEnvelope with the capability
// id in the handler; these tests prove the wiring is real, not just present,
// by asserting the specific basis/level combination each branch and profile
// must produce.

// TestHandleLanguageQueryGraphBackedBranchDemotesUnderLocalLightweight proves
// the normalizeTruthBasis demotion itself: an injected fake Neo4j reader
// forces the graph-backed branch to observe TruthBasisAuthoritativeGraph (see
// queryByLanguageWithSemanticFilter's doc comment for when that basis is
// returned), and this asserts normalizeTruthBasis demotes it to hybrid (level
// derived) under local_lightweight, per contract.go's rule that
// local_lightweight can never claim authoritative_graph truth.
//
// This is a wiring proof, not a proof of the matrix's production
// `local_lightweight: {max_truth_level: derived}` row. Under a graph-less
// local_lightweight deployment the demotion is not what a caller actually
// hits: both production wirings construct query.NewNeo4jReader
// unconditionally (cmd/api/wiring.go and cmd/mcp-server/wiring.go), so
// h.Neo4j is a non-nil *Neo4jReader wrapping a nil driver rather than a nil
// interface. Before #5761 F1 that meant every graph-backed and graph-first
// read attempted the doomed graph call, since a plain nil check treated the
// driverless reader as configured. F1 fixed that: querycontract.GraphConfigured
// asks Neo4jReader.GraphConfigured()
// instead of comparing to nil, so a driverless reader is now correctly
// treated as unconfigured, and the graph-backed branch takes the
// content-store fallback (TruthBasisContentIndex) instead of calling
// Run at all -- see
// TestHandleLanguageQueryUnconfiguredReaderServesContentBackedEntityType
// (language_query_graphless_reader_test.go) for that behavior, and
// TestHandleLanguageQueryUnconfiguredReaderReturns501ForGraphOnlyEntityType
// for the Repository/Directory/File residue that has no content-store
// equivalent to fall back to. This test only proves the local_lightweight
// demotion mechanism fires correctly when authoritative_graph truth actually
// reaches it (an injected fake reader that reports itself configured), which
// is a different code path from the driverless-reader fallback described
// above.
func TestHandleLanguageQueryGraphBackedBranchDemotesUnderLocalLightweight(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Profile: ProfileLocalLightweight,
		Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return []map[string]any{{"entity_id": "e1", "name": "Foo"}}, nil
		}},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		strings.NewReader(`{"language":"go","entity_type":"function","query":"Foo"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.handleLanguageQuery(rec, req)

	envelope := decodeLanguageQueryEnvelope(t, rec)
	if envelope.Truth == nil {
		t.Fatalf("truth envelope missing, body = %s", rec.Body.String())
	}
	if envelope.Truth.Basis != TruthBasisHybrid {
		t.Fatalf("truth.basis = %q, want %q (local_lightweight must demote authoritative_graph)", envelope.Truth.Basis, TruthBasisHybrid)
	}
	if envelope.Truth.Level != TruthLevelDerived {
		t.Fatalf("truth.level = %q, want %q", envelope.Truth.Level, TruthLevelDerived)
	}
}

// TestHandleLanguageQueryGraphBackedBranchExactUnderAuthoritativeProfile
// proves the same graph-backed branch reports the undemoted
// authoritative_graph basis at TruthLevelExact once the profile carries the
// graph sidecar, matching the matrix's `local_authoritative`/`production`
// `exact` ceiling rows.
func TestHandleLanguageQueryGraphBackedBranchExactUnderAuthoritativeProfile(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return []map[string]any{{"entity_id": "e1", "name": "Foo"}}, nil
		}},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		strings.NewReader(`{"language":"go","entity_type":"function","query":"Foo"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.handleLanguageQuery(rec, req)

	envelope := decodeLanguageQueryEnvelope(t, rec)
	if envelope.Truth == nil {
		t.Fatalf("truth envelope missing, body = %s", rec.Body.String())
	}
	if envelope.Truth.Basis != TruthBasisAuthoritativeGraph {
		t.Fatalf("truth.basis = %q, want %q", envelope.Truth.Basis, TruthBasisAuthoritativeGraph)
	}
	if envelope.Truth.Level != TruthLevelExact {
		t.Fatalf("truth.level = %q, want %q", envelope.Truth.Level, TruthLevelExact)
	}
}

// TestHandleLanguageQueryContentBackedBranchReportsDerivedContentIndex proves
// the contentBackedEntityTypes branch ("variable" here, per language_query_
// entities.go) reports TruthBasisContentIndex at TruthLevelDerived -- a
// content-store read can never claim exact graph truth, regardless of
// profile.
func TestHandleLanguageQueryContentBackedBranchReportsDerivedContentIndex(t *testing.T) {
	t.Parallel()

	content := &languageQueryContentStore{
		rows: []EntityContent{{
			EntityID:     "content-variable-1",
			RepoID:       "repo-1",
			RelativePath: "src/config.ts",
			EntityType:   "Variable",
			EntityName:   "config",
			Language:     "typescript",
		}},
	}
	handler := &LanguageQueryHandler{Content: content}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		strings.NewReader(`{"language":"typescript","entity_type":"variable","query":"config"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.handleLanguageQuery(rec, req)

	envelope := decodeLanguageQueryEnvelope(t, rec)
	if envelope.Truth == nil {
		t.Fatalf("truth envelope missing, body = %s", rec.Body.String())
	}
	if envelope.Truth.Basis != TruthBasisContentIndex {
		t.Fatalf("truth.basis = %q, want %q", envelope.Truth.Basis, TruthBasisContentIndex)
	}
	if envelope.Truth.Level != TruthLevelDerived {
		t.Fatalf("truth.level = %q, want %q", envelope.Truth.Level, TruthLevelDerived)
	}
	// #5761 P2-2 review-fix regression: the contentBackedEntityTypes dispatch
	// branch (language_queries.go) passes reasonLanguageQueryContentOnly
	// directly rather than computing it per-request, so it must be pinned by
	// its exact string -- basis/level alone do not prove the branch used the
	// right reason constant. The want value is a literal, not a reference to
	// reasonLanguageQueryContentOnly itself: comparing the constant against
	// itself would stay green even if the constant's value were mutated,
	// since both sides of the assertion would drift together.
	const wantContentOnlyReason = "content-store read served this entity type"
	if envelope.Truth.Reason != wantContentOnlyReason {
		t.Fatalf("truth.reason = %q, want %q", envelope.Truth.Reason, wantContentOnlyReason)
	}
}

// TestHandleLanguageQueryPlainRequestGetsUnwrappedBody is the backward-
// compatibility proof: WriteSuccess (handler.go) only wraps the response in a
// ResponseEnvelope when the caller sends the envelope Accept header
// (acceptsEnvelope); a caller that does not ask for the envelope must keep
// getting the identical plain data body it always has.
func TestHandleLanguageQueryPlainRequestGetsUnwrappedBody(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return []map[string]any{{"entity_id": "e1", "name": "Foo"}}, nil
		}},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		strings.NewReader(`{"language":"go","entity_type":"function","query":"Foo"}`))
	req.Header.Set("Content-Type", "application/json")
	// Deliberately no Accept: application/eshu.envelope+json header.
	rec := httptest.NewRecorder()

	handler.handleLanguageQuery(rec, req)

	var plain map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &plain); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, body = %s", err, rec.Body.String())
	}
	if _, ok := plain["truth"]; ok {
		t.Fatalf("plain (non-envelope) response carries a top-level %q key, want the unwrapped data body: %s", "truth", rec.Body.String())
	}
	if _, ok := plain["results"]; !ok {
		t.Fatalf("plain response missing %q, want the unwrapped data body: %s", "results", rec.Body.String())
	}
}

func decodeLanguageQueryEnvelope(t *testing.T, rec *httptest.ResponseRecorder) ResponseEnvelope {
	t.Helper()
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, body = %s", err, rec.Body.String())
	}
	return envelope
}
