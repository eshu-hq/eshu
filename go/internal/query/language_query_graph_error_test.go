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

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestHandleLanguageQueryMapsGraphReadAvailabilityErrors is the #5761
// regression. POST /api/v0/code/language-query was the one graph-backed route
// that still answered a bounded graph-read failure with HTTP 500 and the raw
// err.Error() in the body, so a caller could not distinguish "the graph is
// down, retry" from "your request was wrong", and driver text reached the
// response.
//
// Three of the route's four guarded call sites are covered here: the
// entity_type=="guard" special case (graph-first-with-content-fallback), the
// graphBackedEntityTypes dispatch (graph-only), and the
// graphFirstContentBackedEntityTypes dispatch (graph-first-with-content-
// fallback, using "sql_table" -- a live graph-backed entity type per
// language_query_entities.go). Each is a separate call site with its own
// error return, so one being mapped does not imply another is.
// TestHandleLanguageQueryContentBackedBranchMapsGraphReadAvailabilityErrors
// below covers the fourth (contentBackedEntityTypes), which is Postgres
// content-store backed rather than graph-backed and needs a different fake.
func TestHandleLanguageQueryMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()

	branches := []struct {
		name string
		body string
	}{
		{name: "guard_branch", body: `{"language":"go","entity_type":"guard","query":"x"}`},
		{name: "graph_backed_branch", body: `{"language":"go","entity_type":"function","query":"x"}`},
		{name: "graph_first_content_backed_branch", body: `{"language":"go","entity_type":"sql_table","query":"x"}`},
	}

	for _, branch := range branches {
		for _, test := range graphReadSweepCases() {
			t.Run(branch.name+"/"+test.name, func(t *testing.T) {
				t.Parallel()
				handler := &LanguageQueryHandler{
					Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
						return nil, test.err
					}},
				}
				req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query", strings.NewReader(branch.body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Accept", EnvelopeMIMEType)
				rec := httptest.NewRecorder()

				handler.handleLanguageQuery(rec, req)

				assertGraphReadSweepResponse(t, rec, test)
			})
		}
	}
}

// TestHandleLanguageQueryContentBackedBranchMapsGraphReadAvailabilityErrors
// covers the fourth guarded call site, contentBackedEntityTypes
// (language_queries.go's queryContentByLanguage branch). Unlike the other
// three branches, this one is Postgres content-store backed -- it can only be
// reached for an entity_type absent from both graphBackedEntityTypes and
// graphFirstContentBackedEntityTypes, such as "variable" -- so it needs a
// ContentStore fake instead of a GraphQuery fake.
//
// This is a helper-contract test, not a production-failure regression: the
// real ContentReader.SearchEntitiesByLanguageAndType
// (content_reader_entity_search.go) returns a plain wrapped SQL error, never
// ErrGraphUnavailable or ErrGraphReadDeadline, so this call site cannot
// actually produce a bounded sentinel in production today -- the only two
// producers of those sentinels are neo4j_read_policy.go:314 (deadline) and
// neo4j_read_policy.go:320 (unavailable), both graph-read paths. There is no
// content-store-only precedent; repository_content.go and
// entity_content_types.go's content fallbacks both guard graph reads, not
// content-store reads. This test exists to pin WriteGraphReadError's own
// contract (any bounded sentinel it is ever given must be mapped, regardless
// of call site) and to catch a future change that wires a real sentinel-
// producing failure path into this branch without also covering it here.
func TestHandleLanguageQueryContentBackedBranchMapsGraphReadAvailabilityErrors(t *testing.T) {
	t.Parallel()

	for _, test := range graphReadSweepCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := &LanguageQueryHandler{
				Content: fakeErrLanguageQueryContentStore{err: test.err},
			}
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
				strings.NewReader(`{"language":"go","entity_type":"variable","query":"x"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()

			handler.handleLanguageQuery(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}

// fakeErrLanguageQueryContentStore is a ContentStore whose
// SearchEntitiesByLanguageAndType always returns err, so tests can drive the
// contentBackedEntityTypes call site through a bounded-availability sentinel
// without a real Postgres content store.
type fakeErrLanguageQueryContentStore struct {
	fakePortContentStore
	err error
}

func (f fakeErrLanguageQueryContentStore) SearchEntitiesByLanguageAndType(
	context.Context, string, string, string, string, int,
) ([]EntityContent, error) {
	return nil, f.err
}

// TestLanguageQueryCarriesLanguageEntitiesCapability pins the capability the
// error envelope reports. The envelope's capability is what an operator
// pivots on when triaging a 503/504, so an empty or drifting value makes the
// failure unattributable.
//
// symbol_graph.language_entities is a route-level capability minted for this
// route (#5761), not a reused id. The route's own MCP tool,
// execute_language_query, is already bound to five symbol_graph.* facets
// (decorators, argument_names, class_methods, imports, inheritance) in
// specs/capability-matrix.v1.yaml, but each names one specific semantic facet,
// not "look up entities of kind K in language L" -- what this route actually
// does. code_search.symbol_lookup (owned by code_symbol.go's
// POST /api/v0/code/symbols/search) was rejected for the same reason: it is a
// different route with different failure semantics.
func TestLanguageQueryCarriesLanguageEntitiesCapability(t *testing.T) {
	t.Parallel()

	handler := &LanguageQueryHandler{
		Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			return nil, ErrGraphUnavailable
		}},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		strings.NewReader(`{"language":"go","entity_type":"function","query":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.handleLanguageQuery(rec, req)

	if want := `"capability":"symbol_graph.language_entities"`; !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("body = %s, want %s", rec.Body.String(), want)
	}
}

// TestHandleLanguageQueryCapabilityGateReturns501WhenUnsupported proves the
// capability gate added ahead of every read (AGENTS.md's "capability gate
// before touching GraphQuery or ContentStore" invariant) actually blocks the
// route when the profile cannot serve the capability. The real committed
// matrix supports symbol_graph.language_entities at every profile (content-
// only entity kinds are servable without a graph sidecar), so no real profile
// is unsupported today; this test proves the gate mechanism itself by
// temporarily forcing the matrix entry unsupported and restoring it after.
// This test deliberately does not call t.Parallel(): it mutates the
// package-level capabilityMatrix map in place for the duration of the test
// (restored via t.Cleanup), and other tests in this package read that same
// map concurrently through BuildTruthEnvelope and capabilityUnsupported. That
// is only safe because this test runs serially -- running it in parallel
// with any test that reads capabilityMatrix would be a data race, and could
// also let another test observe the temporarily-cleared entry and fail for
// the wrong reason. Do not add t.Parallel() here without giving
// capabilityMatrix its own synchronization.
func TestHandleLanguageQueryCapabilityGateReturns501WhenUnsupported(t *testing.T) {
	original, ok := capabilityMatrix[languageQueryCapability]
	if !ok {
		t.Fatalf("capabilityMatrix missing %q", languageQueryCapability)
	}
	capabilityMatrix[languageQueryCapability] = capabilitySupport{}
	t.Cleanup(func() { capabilityMatrix[languageQueryCapability] = original })

	handler := &LanguageQueryHandler{
		Neo4j: fakeGraphReader{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			t.Fatal("Neo4j.Run called; the capability gate must block the read")
			return nil, nil
		}},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/language-query",
		strings.NewReader(`{"language":"go","entity_type":"function","query":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.handleLanguageQuery(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusNotImplemented, rec.Body.String())
	}
	if want := `"code":"unsupported_capability"`; !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("body = %s, want %s", rec.Body.String(), want)
	}
	// P2-5: the profile gate must keep its own message, distinct from the
	// graph-only-residue 501 (TestHandleLanguageQueryUnconfiguredReaderReturns501ForGraphOnlyEntityType),
	// so an operator reading the body can tell which of the two unsupported-
	// capability causes actually fired.
	if want := `"message":"language query requires a supported query profile"`; !strings.Contains(rec.Body.String(), want) {
		t.Fatalf("body = %s, want %s", rec.Body.String(), want)
	}
}

// TestOpenAPILanguageQueryDocuments501 proves the language-query OpenAPI
// fragment declares the 501 response the handler can actually return:
// handleLanguageQuery writes http.StatusNotImplemented with
// ErrorCodeUnsupportedCapability when capabilityUnsupported gates
// languageQueryCapability at the running profile (language_queries.go), but
// until this route's openapi_paths_code.go fragment listed "501" the live
// spec omitted a response the handler could genuinely produce -- exactly the
// documented-vs-actual drift AGENTS.md's "OpenAPI fragments and handler
// behavior must agree" invariant exists to catch. Modeled on
// TestOpenAPIContractImpactSurfaceDocumentsFamiliesAndEvidenceBoundary's
// status-set assertion in openapi_contract_impact_test.go.
func TestOpenAPILanguageQueryDocuments501(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	languageQueryPath := querytestutil.MustMapField(t, paths, "/api/v0/code/language-query")
	languageQueryPost := querytestutil.MustMapField(t, languageQueryPath, "post")
	responses := querytestutil.MustMapField(t, languageQueryPost, "responses")
	for _, status := range []string{"400", "500", "501", "503", "504"} {
		if _, ok := responses[status]; !ok {
			t.Fatalf("language-query responses missing status %s", status)
		}
	}
}

// TestOpenAPILanguageQueryResponseDocumentsSourceBackend is the #5761 P1-2
// review-fix regression: writeLanguageQueryResult (language_queries.go) has
// emitted a "source_backend" field on every response since #5761 landed, but
// the OpenAPI LanguageQueryResponse schema (openapi_components.go) never
// gained the matching property, so the live spec omitted a field the handler
// actually returns on every call -- the same documented-vs-actual drift
// TestOpenAPILanguageQueryDocuments501 above exists to catch, and the same
// gap CodeSearchResponse, SymbolSearchResponse, and EntityContentSearchResponse
// already close for their own "source_backend" fields.
//
// The enum assertion is the #5761 P2-1 review-fix regression: the property's
// "enum" is asserted against the values sourceBackendForTruthBasis
// (language_query_reasons.go) actually derives from every TruthBasis outcome
// this route can produce, rather than a hand-frozen literal list, so a future
// change to sourceBackendForTruthBasis that silently drifts from the
// documented enum fails this test instead of only being caught by manual
// inspection.
func TestOpenAPILanguageQueryResponseDocumentsSourceBackend(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	schemas := querytestutil.MustMapField(t, querytestutil.MustMapField(t, spec, "components"), "schemas")
	languageQueryResponse := querytestutil.MustMapField(t, schemas, "LanguageQueryResponse")
	properties := querytestutil.MustMapField(t, languageQueryResponse, "properties")
	sourceBackend, ok := properties["source_backend"].(map[string]any)
	if !ok {
		t.Fatalf("LanguageQueryResponse.properties.source_backend missing or wrong type: %#v", properties["source_backend"])
	}
	if got, want := sourceBackend["type"], "string"; got != want {
		t.Fatalf("LanguageQueryResponse.properties.source_backend.type = %#v, want %#v", got, want)
	}

	rawEnum, ok := sourceBackend["enum"].([]any)
	if !ok {
		t.Fatalf("LanguageQueryResponse.properties.source_backend.enum missing or wrong type: %#v", sourceBackend["enum"])
	}
	gotEnum := make(map[string]bool, len(rawEnum))
	for _, v := range rawEnum {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("LanguageQueryResponse.properties.source_backend.enum entry %#v is not a string", v)
		}
		gotEnum[s] = true
	}

	wantValues := []string{
		sourceBackendForTruthBasis(TruthBasisAuthoritativeGraph),
		sourceBackendForTruthBasis(TruthBasisHybrid),
		sourceBackendForTruthBasis(TruthBasisContentIndex),
		sourceBackendForTruthBasis(TruthBasis("unrecognized_basis_for_test")),
	}
	wantEnum := make(map[string]bool, len(wantValues))
	for _, v := range wantValues {
		wantEnum[v] = true
	}

	if len(gotEnum) != len(wantEnum) {
		t.Fatalf("LanguageQueryResponse.properties.source_backend.enum = %v, want %v", rawEnum, wantValues)
	}
	for v := range wantEnum {
		if !gotEnum[v] {
			t.Fatalf("LanguageQueryResponse.properties.source_backend.enum = %v, missing derived value %q", rawEnum, v)
		}
	}
}
