// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestOpenAPILanguageQueryResponseDocumentsSourceBackend is the #5761 P1-2
// review-fix regression: writeLanguageQueryResult (language/handler.go) has
// emitted a "source_backend" field on every response since #5761 landed, but
// the OpenAPI LanguageQueryResponse schema (openapi_components.go) never
// gained the matching property, so the live spec omitted a field the handler
// actually returns on every call -- the same documented-vs-actual drift
// TestOpenAPILanguageQueryDocuments501 (language_query_graph_error_test.go)
// exists to catch, and the same gap CodeSearchResponse, SymbolSearchResponse,
// and EntityContentSearchResponse already close for their own
// "source_backend" fields.
//
// The enum assertion is the #5761 P2-1 review-fix regression: the property's
// "enum" is asserted against the values sourceBackendForTruthBasis
// (language/reasons.go) actually derives from every TruthBasis outcome
// this route can produce, rather than a hand-frozen literal list, so a future
// change to sourceBackendForTruthBasis that silently drifts from the
// documented enum fails this test instead of only being caught by manual
// inspection.
//
// This test calls sourceBackendForTruthBasis directly rather than driving it
// through the route (#6642): it exists to pin the free function's own output
// set against the OpenAPI enum, not any one dispatch branch's behavior, so it
// lives in this family-owned white-box file rather than
// language_query_graph_error_test.go.
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
		sourceBackendForTruthBasis(TruthBasisNoBackendRead),
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
