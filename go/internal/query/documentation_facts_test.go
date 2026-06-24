// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestDocumentationHandlerListsCollectedFacts(t *testing.T) {
	t.Parallel()

	handler := &DocumentationHandler{
		Content: fakePortContentStore{
			documentationFactsModel: documentationFactListReadModel{
				Facts: []map[string]any{{
					"fact_id":       "fact:doc:1",
					"fact_kind":     "documentation_document",
					"scope_id":      "doc-source:confluence:example.atlassian.net:196609",
					"generation_id": "gen-1",
					"payload": map[string]any{
						"source_id":     "doc-source:confluence:example.atlassian.net:196609",
						"document_id":   "doc:confluence:123",
						"title":         "Platform Runbook",
						"canonical_uri": "https://example.atlassian.net/wiki/spaces/PLAT/pages/123",
					},
				}},
				NextCursor: "1",
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/documentation/facts?scope_id=doc-source:confluence:example.atlassian.net:196609&fact_kind=documentation_document&limit=1",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	facts := data["facts"].([]any)
	if got, want := len(facts), 1; got != want {
		t.Fatalf("len(facts) = %d, want %d", got, want)
	}
	if got, want := data["count"], float64(1); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	if got, want := data["limit"], float64(1); got != want {
		t.Fatalf("limit = %#v, want %#v", got, want)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	if got, want := data["next_cursor"], "1"; got != want {
		t.Fatalf("next_cursor = %#v, want %#v", got, want)
	}
	if got, want := data["missing_evidence"], false; got != want {
		t.Fatalf("missing_evidence = %#v, want %#v", got, want)
	}
	states, ok := data["states"].([]any)
	if !ok {
		t.Fatalf("states = %#v, want array", data["states"])
	}
	if got := len(states); got != 0 {
		t.Fatalf("len(states) = %d, want 0", got)
	}
	fact := facts[0].(map[string]any)
	if got, want := fact["fact_kind"], "documentation_document"; got != want {
		t.Fatalf("fact_kind = %#v, want %#v", got, want)
	}
	payload := fact["payload"].(map[string]any)
	if got, want := payload["title"], "Platform Runbook"; got != want {
		t.Fatalf("payload.title = %#v, want %#v", got, want)
	}
	if resp.Truth == nil {
		t.Fatalf("truth = nil, want documentation facts truth envelope")
	}
	if got, want := resp.Truth.Capability, documentationFactsCapability; got != want {
		t.Fatalf("truth.capability = %#v, want %#v", got, want)
	}
}

func TestDocumentationHandlerListsDiagramFacts(t *testing.T) {
	t.Parallel()

	var captured documentationFactFilter
	handler := &DocumentationHandler{
		Content: fakePortContentStore{
			documentationFactsFilter: &captured,
			documentationFactsModel: documentationFactListReadModel{
				Facts: []map[string]any{{
					"fact_id":       "fact:diagram:section:1",
					"fact_kind":     "documentation_section",
					"scope_id":      "scope-diagram",
					"generation_id": "gen-diagram",
					"payload": map[string]any{
						"document_id":    "doc:git:repository:r_diagram:docs/architecture.mmd",
						"section_id":     "section:diagram",
						"heading_text":   "architecture",
						"content":        "Repository Docs\nDocumentation API",
						"content_format": "mermaid",
						"source_metadata": map[string]any{
							"path":          "docs/architecture.mmd",
							"format_family": "diagram",
						},
						"linked_entities": []any{map[string]any{
							"entity_type": "repository",
							"entity_id":   "repository:r_diagram",
						}},
					},
				}},
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/documentation/facts?repo=repository:r_diagram&fact_kind=section&q=Documentation%20API&limit=1",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := captured.Repository, "repository:r_diagram"; got != want {
		t.Fatalf("Repository = %q, want %q", got, want)
	}
	if got, want := captured.FactKind, "documentation_section"; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := captured.Query, "Documentation API"; got != want {
		t.Fatalf("Query = %q, want %q", got, want)
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	facts := data["facts"].([]any)
	if got, want := len(facts), 1; got != want {
		t.Fatalf("len(facts) = %d, want %d", got, want)
	}
	payload := facts[0].(map[string]any)["payload"].(map[string]any)
	if got, want := payload["content_format"], "mermaid"; got != want {
		t.Fatalf("payload.content_format = %#v, want %#v", got, want)
	}
	metadata := payload["source_metadata"].(map[string]any)
	if got, want := metadata["format_family"], "diagram"; got != want {
		t.Fatalf("payload.source_metadata.format_family = %#v, want %#v", got, want)
	}
}

func TestDocumentationHandlerRequiresFactScopeOrAnchor(t *testing.T) {
	t.Parallel()

	handler := &DocumentationHandler{
		Content: fakePortContentStore{},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/documentation/facts?fact_kind=documentation_section", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	assertDocumentationError(t, w.Body.Bytes(), "invalid_argument")
	for _, anchor := range []string{"scope_id", "repo", "target_id", "service_id", "source_id", "document_id", "section_id"} {
		if !strings.Contains(w.Body.String(), anchor) {
			t.Fatalf("error body missing anchor %q: %s", anchor, w.Body.String())
		}
	}
}

func TestDocumentationHandlerFactsResponseExplainsScopedEmptyPage(t *testing.T) {
	t.Parallel()

	handler := &DocumentationHandler{
		Content: fakePortContentStore{
			documentationFactsModel: documentationFactListReadModel{},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/documentation/facts?scope_id=docs-scope&fact_kind=documentation_section&limit=2",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	facts := data["facts"].([]any)
	if got := len(facts); got != 0 {
		t.Fatalf("len(facts) = %d, want 0", got)
	}
	if got, want := data["count"], float64(0); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	if got, want := data["limit"], float64(2); got != want {
		t.Fatalf("limit = %#v, want %#v", got, want)
	}
	if got, want := data["truncated"], false; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	if got, want := data["missing_evidence"], true; got != want {
		t.Fatalf("missing_evidence = %#v, want %#v", got, want)
	}
	states := data["states"].([]any)
	if got, want := states, []any{"no_documentation_facts"}; !equalDocumentationAnySlice(got, want) {
		t.Fatalf("states = %#v, want %#v", got, want)
	}
	if _, ok := data["next_cursor"]; ok {
		t.Fatalf("next_cursor present on complete empty page: %#v", data["next_cursor"])
	}
}

func TestDocumentationHandlerAllowsSourceFactDiscovery(t *testing.T) {
	t.Parallel()

	var captured documentationFactFilter
	handler := &DocumentationHandler{Content: fakePortContentStore{documentationFactsFilter: &captured}, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/documentation/facts?fact_kind=source&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := captured.FactKind, "documentation_source"; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
}

func TestNormalizeDocumentationFactKindAcceptsSemanticDocumentationObservation(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		facts.SemanticDocumentationObservationFactKind,
		documentationFactKindAliasSemanticObservation,
		documentationFactKindAliasDocumentationObservation,
		documentationFactKindAliasSemanticDocumentationObservation,
	} {
		got, ok := normalizeDocumentationFactKind(raw)
		if !ok {
			t.Fatalf("normalizeDocumentationFactKind(%q) rejected semantic observation alias", raw)
		}
		if got, want := got, facts.SemanticDocumentationObservationFactKind; got != want {
			t.Fatalf("normalizeDocumentationFactKind(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestDocumentationHandlerAllowsSemanticObservationFactDiscovery(t *testing.T) {
	t.Parallel()

	var captured documentationFactFilter
	handler := &DocumentationHandler{
		Content: fakePortContentStore{documentationFactsFilter: &captured},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/documentation/facts?fact_kind=semantic_observation&source_id=doc-source:git:platform-docs&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := captured.FactKind, facts.SemanticDocumentationObservationFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
}

func TestContentReaderDocumentationFactsFiltersAndPaginates(t *testing.T) {
	t.Parallel()

	first := []byte(`{
		"fact_id": "fact:section:1",
		"fact_kind": "documentation_section",
		"scope_id": "doc-source:confluence:example.atlassian.net:196609",
		"generation_id": "gen-1",
		"payload": {
			"document_id": "doc:confluence:123",
			"section_id": "body",
			"heading_text": "Deployments",
			"content": "Payments deployment runbook"
		}
	}`)
	second := []byte(`{"fact_id": "fact:section:2", "fact_kind": "documentation_section", "payload": {}}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"payload"},
			rows:    [][]driver.Value{{first}, {second}},
		},
	})
	reader := NewContentReader(db)

	got, err := reader.documentationFacts(t.Context(), documentationFactFilter{
		FactKind:   "documentation_section",
		ScopeID:    "doc-source:confluence:example.atlassian.net:196609",
		DocumentID: "doc:confluence:123",
		Query:      "deployment",
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("documentationFacts() error = %v, want nil", err)
	}
	if got, want := len(got.Facts), 1; got != want {
		t.Fatalf("len(Facts) = %d, want %d", got, want)
	}
	if got, want := got.NextCursor, "1"; got != want {
		t.Fatalf("NextCursor = %#v, want %#v", got, want)
	}
	payload := got.Facts[0]["payload"].(map[string]any)
	if got, want := payload["heading_text"], "Deployments"; got != want {
		t.Fatalf("payload.heading_text = %#v, want %#v", got, want)
	}
}

func TestContentReaderDocumentationFactsSearchesLinkTargetURI(t *testing.T) {
	t.Parallel()

	link := []byte(`{
		"fact_id": "fact:link:1",
		"fact_kind": "documentation_link",
		"scope_id": "doc-source:confluence:example.atlassian.net:196609",
		"generation_id": "gen-1",
		"payload": {
			"document_id": "doc:confluence:123",
			"section_id": "service-links",
			"target_uri": "service://payments-api"
		}
	}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"payload"},
		rows:    [][]driver.Value{{link}},
		queryContains: []string{
			"fact_records.payload->>'target_uri'",
		},
	}})
	reader := NewContentReader(db)

	got, err := reader.documentationFacts(t.Context(), documentationFactFilter{
		FactKind:   "documentation_link",
		ScopeID:    "doc-source:confluence:example.atlassian.net:196609",
		DocumentID: "doc:confluence:123",
		Query:      "payments-api",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("documentationFacts() error = %v, want nil", err)
	}
	if got, want := len(got.Facts), 1; got != want {
		t.Fatalf("len(Facts) = %d, want %d", got, want)
	}
	payload := got.Facts[0]["payload"].(map[string]any)
	if got, want := payload["target_uri"], "service://payments-api"; got != want {
		t.Fatalf("payload.target_uri = %#v, want %#v", got, want)
	}
}

func TestContentReaderDocumentationFactsReturnsEmptyForNoMatch(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"payload"},
		rows:    nil,
	}})
	reader := NewContentReader(db)

	got, err := reader.documentationFacts(t.Context(), documentationFactFilter{
		FactKind: "documentation_link",
		ScopeID:  "doc-source:confluence:example.atlassian.net:196609",
		Query:    "not-present",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("documentationFacts() error = %v, want nil", err)
	}
	if got := len(got.Facts); got != 0 {
		t.Fatalf("len(Facts) = %d, want 0", got)
	}
	if got := got.NextCursor; got != "" {
		t.Fatalf("NextCursor = %#v, want empty", got)
	}
}

func TestBuildDocumentationFactsSQLIsScopedAndBounded(t *testing.T) {
	t.Parallel()

	query, args := buildDocumentationFactsSQL(documentationFactFilter{
		FactKind:     "documentation_section",
		ScopeID:      "docs-scope",
		GenerationID: "gen-1",
		DocumentID:   "doc:confluence:123",
		SectionID:    "body",
		Query:        "deployment",
		Limit:        10,
	})

	for _, fragment := range []string{
		"fact_records.fact_kind = $1",
		"fact_records.is_tombstone = FALSE",
		"fact_records.scope_id = $2",
		"fact_records.generation_id = $3",
		"fact_records.payload->>'document_id' = $4",
		"fact_records.payload->>'section_id' = $5",
		"LOWER(",
		"fact_records.payload->>'target_uri'",
		"ORDER BY fact_records.observed_at DESC, fact_records.fact_id DESC",
		"LIMIT $7 OFFSET $8",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("documentation facts SQL missing fragment %q: %s", fragment, query)
		}
	}
	if got, want := args[:6], []any{
		"documentation_section",
		"docs-scope",
		"gen-1",
		"doc:confluence:123",
		"body",
		"%deployment%",
	}; !equalDocumentationAnySlice(got, want) {
		t.Fatalf("filter args = %#v, want %#v", got, want)
	}
}

func TestBuildDocumentationFactsSQLDefaultAllowlistIncludesSemanticDocumentationObservation(t *testing.T) {
	t.Parallel()

	query, _ := buildDocumentationFactsSQL(documentationFactFilter{
		ScopeID: "docs-scope",
		Limit:   10,
	})

	for _, want := range []string{
		facts.DocumentationSourceFactKind,
		facts.DocumentationDocumentFactKind,
		facts.DocumentationSectionFactKind,
		facts.DocumentationLinkFactKind,
		facts.DocumentationEntityMentionFactKind,
		facts.DocumentationClaimCandidateFactKind,
		facts.SemanticDocumentationObservationFactKind,
	} {
		if !strings.Contains(query, "'"+want+"'") {
			t.Fatalf("documentation facts default SQL allowlist missing %q: %s", want, query)
		}
	}
	for _, unexpected := range []string{
		facts.DocumentationFindingFactKind,
		facts.DocumentationEvidencePacketFactKind,
		facts.SemanticCodeHintFactKind,
	} {
		if strings.Contains(query, "'"+unexpected+"'") {
			t.Fatalf("documentation facts default SQL allowlist includes %q, want provenance-only documentation facts: %s", unexpected, query)
		}
	}
}

func TestOpenAPISpecIncludesDocumentationFacts(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := spec["paths"].(map[string]any)
	path, ok := paths["/api/v0/documentation/facts"].(map[string]any)
	if !ok {
		t.Fatal("OpenAPISpec() missing /api/v0/documentation/facts")
	}
	get := path["get"].(map[string]any)
	if got, want := get["operationId"], "listDocumentationFacts"; got != want {
		t.Fatalf("operationId = %#v, want %#v", got, want)
	}
	schema := get["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	for _, name := range []string{"facts", "count", "limit", "truncated", "missing_evidence", "states", "next_cursor"} {
		if _, ok := properties[name]; !ok {
			t.Fatalf("documentation facts OpenAPI response missing %q", name)
		}
	}
	required := schema["required"].([]any)
	for _, name := range []string{"facts", "count", "limit", "truncated", "missing_evidence", "states"} {
		if !openAPIStringListIncludes(required, name) {
			t.Fatalf("documentation facts OpenAPI required fields missing %q", name)
		}
	}
	if openAPIStringListIncludes(required, "next_cursor") {
		t.Fatal("documentation facts OpenAPI requires next_cursor, want present only on truncated pages")
	}
	factKindEnum := openAPIParameterStringEnum(t, get["parameters"].([]any), "fact_kind")
	for _, name := range []string{
		documentationFactKindAliasSemanticObservation,
		documentationFactKindAliasDocumentationObservation,
		documentationFactKindAliasSemanticDocumentationObservation,
		facts.SemanticDocumentationObservationFactKind,
	} {
		if !openAPIStringListIncludes(factKindEnum, name) {
			t.Fatalf("documentation facts OpenAPI fact_kind enum missing %q", name)
		}
	}
}

func openAPIStringListIncludes(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func openAPIParameterStringEnum(t *testing.T, parameters []any, name string) []any {
	t.Helper()

	for _, parameter := range parameters {
		row := parameter.(map[string]any)
		if row["name"] != name {
			continue
		}
		schema := row["schema"].(map[string]any)
		enum, ok := schema["enum"].([]any)
		if !ok {
			t.Fatalf("OpenAPI parameter %q enum = %#v, want string enum", name, schema["enum"])
		}
		return enum
	}
	t.Fatalf("OpenAPI parameters missing %q", name)
	return nil
}
