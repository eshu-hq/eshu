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
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// #7128: GET /api/v0/documentation/facts and MCP list_documentation_facts share
// buildDocumentationFactsSQL. With no generation_id the read must bind to the
// scope's active generation instead of every retained generation.

func TestBuildDocumentationFactsSQLBindsActiveGenerationByDefault(t *testing.T) {
	t.Parallel()

	query, args := buildDocumentationFactsSQL(documentationFactFilter{
		FactKind: "documentation_section",
		ScopeID:  "docs-scope",
		Limit:    10,
	})

	want := "fact_records.generation_id = (SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $2)"
	if !strings.Contains(query, want) {
		t.Fatalf("default read must bind the active generation with %q, got:\n%s", want, query)
	}
	// The scalar subquery reuses the scope_id parameter: no extra bind value.
	if got, wantArgs := args[:2], []any{"documentation_section", "docs-scope"}; !equalDocumentationAnySlice(got, wantArgs) {
		t.Fatalf("args = %#v, want prefix %#v", args, wantArgs)
	}
	if got, want := len(args), 4; got != want {
		t.Fatalf("len(args) = %d, want %d (kind, scope, limit, offset): %#v", got, want, args)
	}
}

func TestBuildDocumentationFactsSQLKeepsExplicitGeneration(t *testing.T) {
	t.Parallel()

	query, args := buildDocumentationFactsSQL(documentationFactFilter{
		FactKind:     "documentation_section",
		ScopeID:      "docs-scope",
		GenerationID: "gen-old",
		Limit:        10,
	})

	if !strings.Contains(query, "fact_records.generation_id = $3") {
		t.Fatalf("explicit generation must stay a parameter equality, got:\n%s", query)
	}
	if strings.Contains(query, "SELECT active_generation_id") || strings.Contains(query, "active_generation_id") {
		t.Fatalf("explicit generation must not add an active binding, got:\n%s", query)
	}
	if got := args[2]; got != "gen-old" {
		t.Fatalf("args[2] = %#v, want gen-old", got)
	}
}

func TestDocumentationFactGenerationBindingKind(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		filter documentationFactFilter
		want   string
	}{
		{"scope only", documentationFactFilter{ScopeID: "s"}, "active_scope"},
		{"scope with blank generation", documentationFactFilter{ScopeID: "s", GenerationID: "  "}, "active_scope"},
		{"anchor only", documentationFactFilter{Repository: "repository:r_1"}, "active_join"},
		{"blank scope is anchor only", documentationFactFilter{ScopeID: "  ", SourceID: "src"}, "active_join"},
		{"explicit", documentationFactFilter{ScopeID: "s", GenerationID: "g"}, "explicit"},
		{"explicit without scope", documentationFactFilter{SourceID: "src", GenerationID: "g"}, "explicit"},
	} {
		if got := documentationFactGenerationBindingKind(tc.filter); got != tc.want {
			t.Errorf("%s: binding kind = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestBuildDocumentationFactsSQLAnchorOnlyJoinsActiveGeneration(t *testing.T) {
	t.Parallel()

	query, _ := buildDocumentationFactsSQL(documentationFactFilter{
		Repository: "repository:r_1",
		SourceID:   "doc-source:git:repository:r_1",
		Limit:      10,
	})
	assertDocumentationFactsActiveBindingWithoutScope(t, query)
	if got := strings.Count(query, "JOIN ingestion_scopes"); got != 1 {
		t.Fatalf("anchor-only read must join ingestion_scopes exactly once, got %d:\n%s", got, query)
	}
	if strings.Contains(query, "LEFT JOIN") {
		t.Fatalf("anchor-only read must use an INNER JOIN, got:\n%s", query)
	}

	scoped, _ := buildDocumentationFactsSQL(documentationFactFilter{
		Repository:      "repository:r_1",
		Limit:           10,
		AllowedScopeIDs: []string{"scope-a"},
	})
	assertDocumentationFactsActiveBindingWithoutScope(t, scoped)
	if strings.Contains(scoped, "LEFT JOIN") {
		t.Fatalf("scoped-token read must not LEFT JOIN ingestion_scopes, got:\n%s", scoped)
	}
	if got := strings.Count(scoped, "JOIN ingestion_scopes"); got != 1 {
		t.Fatalf("scoped-token read must join ingestion_scopes exactly once, got %d:\n%s", got, scoped)
	}
}

func TestBuildDocumentationFactsSQLKindOnlySourceReadKeepsOrderedEarlyStop(t *testing.T) {
	t.Parallel()

	// fact_kind=source with nothing else is served by an ordered scan of the
	// documentation_source partial index that stops after LIMIT rows. A join
	// would replace it with a sort of every source row (measured on ops-qa in
	// docs/internal/evidence/7128-*.md), so this read probes the scope by
	// primary key per candidate row instead.
	query, _ := buildDocumentationFactsSQL(documentationFactFilter{
		FactKind: "documentation_source",
		Limit:    10,
	})
	if !strings.Contains(query, "SELECT s.active_generation_id FROM ingestion_scopes s") ||
		!strings.Contains(query, "= fact_records.generation_id") {
		t.Fatalf("kind-only read must bind the active generation, got:\n%s", query)
	}
	if strings.Contains(query, "JOIN ingestion_scopes") {
		t.Fatalf("kind-only read must not join ingestion_scopes, got:\n%s", query)
	}

	// The same read under a scoped token needs the scope payload for the
	// authorization predicates, so it takes the single INNER JOIN.
	scoped, _ := buildDocumentationFactsSQL(documentationFactFilter{
		FactKind:        "documentation_source",
		Limit:           10,
		AllowedScopeIDs: []string{"scope-a"},
	})
	if got := strings.Count(scoped, "JOIN ingestion_scopes"); got != 1 || strings.Contains(scoped, "LEFT JOIN") {
		t.Fatalf("scoped kind-only read must INNER JOIN ingestion_scopes once, got:\n%s", scoped)
	}
}

func TestBuildDocumentationFactsSQLScopedTokenWithScopeStillBindsActive(t *testing.T) {
	t.Parallel()

	query, _ := buildDocumentationFactsSQL(documentationFactFilter{
		ScopeID:         "docs-scope",
		Limit:           10,
		AllowedScopeIDs: []string{"docs-scope"},
	})
	if !strings.Contains(query, "SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $") {
		t.Fatalf("scope read under a scoped token must bind the active generation, got:\n%s", query)
	}
}

func TestBuildDocumentationFactsSQLExplicitGenerationWithoutScopeKeepsUnboundRead(t *testing.T) {
	t.Parallel()

	query, _ := buildDocumentationFactsSQL(documentationFactFilter{
		SourceID:     "doc-source:git:repository:r_1",
		GenerationID: "gen-old",
		Limit:        10,
	})
	if strings.Contains(query, "active_generation_id") {
		t.Fatalf("explicit generation must not bind the active generation, got:\n%s", query)
	}
	if !strings.Contains(query, "fact_records.generation_id = $") {
		t.Fatalf("explicit generation predicate missing:\n%s", query)
	}
}

// assertDocumentationFactsActiveBindingWithoutScope asserts an anchor-only read
// restricts rows to each scope's active generation through ingestion_scopes.
func assertDocumentationFactsActiveBindingWithoutScope(t *testing.T, query string) {
	t.Helper()
	if !strings.Contains(query, "ingestion_scopes.active_generation_id = fact_records.generation_id") &&
		!strings.Contains(query, "s.active_generation_id = fact_records.generation_id") {
		t.Fatalf("anchor-only read must bind rows to the scope's active generation, got:\n%s", query)
	}
}

// --- read model (ContentReader) ------------------------------------------------

func factRowJSON(generationID string) []byte {
	raw, _ := json.Marshal(map[string]any{
		"fact_id":       "fact:1",
		"fact_kind":     "documentation_document",
		"scope_id":      "docs-scope",
		"generation_id": generationID,
		"payload":       map[string]any{"document_id": "doc:1"},
	})
	return raw
}

func TestContentReaderDocumentationFactsActiveScopeLabelsBoundGenerationWithoutExtraRead(t *testing.T) {
	t.Parallel()

	// One queued result: a second query would fail the fake driver.
	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns:       []string{"payload"},
		rows:          [][]driver.Value{{factRowJSON("gen-active")}},
		queryContains: []string{"SELECT active_generation_id FROM ingestion_scopes"},
	}})
	got, err := NewContentReader(db).DocumentationFacts(t.Context(), documentationFactFilter{
		ScopeID: "docs-scope",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("DocumentationFacts() error = %v", err)
	}
	if got.Binding.GenerationID != "gen-active" || !got.Binding.IsActive {
		t.Fatalf("Binding = %#v, want active gen-active", got.Binding)
	}
	if got.EmptyReason != "" || got.Freshness.State != "" {
		t.Fatalf("non-empty page must carry no empty reason or freshness override, got %#v", got)
	}
}

func TestContentReaderDocumentationFactsAnchorOnlyIsActiveByConstruction(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"payload"},
		rows:    [][]driver.Value{{factRowJSON("gen-a")}},
	}})
	got, err := NewContentReader(db).DocumentationFacts(t.Context(), documentationFactFilter{
		Repository: "repository:r_1",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("DocumentationFacts() error = %v", err)
	}
	if got.Binding.GenerationID != "" || !got.Binding.IsActive {
		t.Fatalf("Binding = %#v, want empty generation and is_active", got.Binding)
	}
}

func TestContentReaderDocumentationFactsEmptyScopeExplainsScopeState(t *testing.T) {
	t.Parallel()

	activeGen := driver.Value("gen-active")
	for _, tc := range []struct {
		name          string
		diagnostic    contentReaderQueryResult
		wantReason    string
		wantFreshness querycontract.FreshnessState
		wantCause     querycontract.FreshnessCause
		wantGen       string
		wantActive    bool
	}{
		{
			name: "scope not found",
			diagnostic: contentReaderQueryResult{
				columns: []string{"status", "active_generation_id"},
			},
			wantReason: "scope_not_found",
		},
		{
			name: "failed scope has no active generation",
			diagnostic: contentReaderQueryResult{
				columns: []string{"status", "active_generation_id"},
				rows:    [][]driver.Value{{"failed", nil}},
			},
			wantReason:    "no_active_generation",
			wantFreshness: querycontract.FreshnessUnavailable,
			wantCause:     querycontract.FreshnessCauseDeadLetteredDomain,
		},
		{
			name: "pending scope has no active generation",
			diagnostic: contentReaderQueryResult{
				columns: []string{"status", "active_generation_id"},
				rows:    [][]driver.Value{{"pending", nil}},
			},
			wantReason:    "no_active_generation",
			wantFreshness: querycontract.FreshnessBuilding,
			wantCause:     querycontract.FreshnessCausePendingRepoGeneration,
		},
		{
			name: "active generation with no matching facts",
			diagnostic: contentReaderQueryResult{
				columns: []string{"status", "active_generation_id"},
				rows:    [][]driver.Value{{"active", activeGen}},
			},
			wantReason: "no_rows",
			wantGen:    "gen-active",
			wantActive: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.diagnostic.queryContains = []string{"FROM ingestion_scopes", "scope_id = $1"}
			tc.diagnostic.wantArgs = []driver.Value{"docs-scope"}
			db := openContentReaderTestDB(t, []contentReaderQueryResult{
				{columns: []string{"payload"}},
				tc.diagnostic,
			})
			got, err := NewContentReader(db).DocumentationFacts(t.Context(), documentationFactFilter{
				ScopeID: "docs-scope",
				Limit:   10,
			})
			if err != nil {
				t.Fatalf("DocumentationFacts() error = %v", err)
			}
			if got.EmptyReason != tc.wantReason {
				t.Fatalf("EmptyReason = %q, want %q", got.EmptyReason, tc.wantReason)
			}
			if got.Freshness.State != tc.wantFreshness || got.Freshness.Cause != tc.wantCause {
				t.Fatalf("Freshness = %#v, want state %q cause %q", got.Freshness, tc.wantFreshness, tc.wantCause)
			}
			if tc.wantFreshness != "" && got.Freshness.Detail == "" {
				t.Fatalf("a non-fresh page must explain itself in Freshness.Detail, got %#v", got.Freshness)
			}
			if got.Binding.GenerationID != tc.wantGen || got.Binding.IsActive != tc.wantActive {
				t.Fatalf("Binding = %#v, want gen %q active %v", got.Binding, tc.wantGen, tc.wantActive)
			}
		})
	}
}

func TestContentReaderDocumentationFactsExplicitGenerationLabelsLifecycle(t *testing.T) {
	t.Parallel()

	superseded := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name          string
		rows          [][]driver.Value
		wantFreshness querycontract.FreshnessState
		wantCause     querycontract.FreshnessCause
		wantActive    bool
	}{
		{"active", [][]driver.Value{{"docs-scope", "active", nil}}, "", "", true},
		{"superseded", [][]driver.Value{{"docs-scope", "superseded", superseded}}, querycontract.FreshnessStale, "", false},
		{"completed", [][]driver.Value{{"docs-scope", "completed", nil}}, querycontract.FreshnessStale, "", false},
		{"failed", [][]driver.Value{{"docs-scope", "failed", nil}}, querycontract.FreshnessStale, "", false},
		{"pending", [][]driver.Value{{"docs-scope", "pending", nil}}, querycontract.FreshnessBuilding, querycontract.FreshnessCausePendingRepoGeneration, false},
		{"unknown generation", nil, querycontract.FreshnessUnavailable, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			db := openContentReaderTestDB(t, []contentReaderQueryResult{
				{columns: []string{"payload"}, rows: [][]driver.Value{{factRowJSON("gen-old")}}},
				{
					columns:       []string{"scope_id", "status", "superseded_at"},
					rows:          tc.rows,
					queryContains: []string{"FROM scope_generations", "generation_id = $1"},
					wantArgs:      []driver.Value{"gen-old"},
				},
			})
			got, err := NewContentReader(db).DocumentationFacts(t.Context(), documentationFactFilter{
				ScopeID:      "docs-scope",
				GenerationID: "gen-old",
				Limit:        10,
			})
			if err != nil {
				t.Fatalf("DocumentationFacts() error = %v", err)
			}
			if got.Freshness.State != tc.wantFreshness || got.Freshness.Cause != tc.wantCause {
				t.Fatalf("Freshness = %#v, want state %q cause %q", got.Freshness, tc.wantFreshness, tc.wantCause)
			}
			if tc.wantFreshness != "" && got.Freshness.Detail == "" {
				t.Fatalf("a non-fresh page must explain itself in Freshness.Detail, got %#v", got.Freshness)
			}
			if got.Binding.GenerationID != "gen-old" || got.Binding.IsActive != tc.wantActive {
				t.Fatalf("Binding = %#v, want gen-old active %v", got.Binding, tc.wantActive)
			}
			if got.EmptyReason != "" {
				t.Fatalf("EmptyReason = %q, want empty for an explicit read", got.EmptyReason)
			}
			if len(got.Facts) != 1 {
				t.Fatalf("explicit read must still return the generation's rows, got %d", len(got.Facts))
			}
		})
	}
}

func TestContentReaderDocumentationFactsRecordsEmptyPageEvent(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{columns: []string{"payload"}},
		{columns: []string{"status", "active_generation_id"}, rows: [][]driver.Value{{"failed", nil}}},
	})
	reader := NewContentReader(db)
	reader.tracer = provider.Tracer("documentation-facts-test")

	if _, err := reader.DocumentationFacts(t.Context(), documentationFactFilter{ScopeID: "docs-scope", Limit: 10}); err != nil {
		t.Fatalf("DocumentationFacts() error = %v", err)
	}
	var reason string
	for _, span := range recorder.Ended() {
		for _, event := range span.Events() {
			if event.Name != "documentation.empty_page" {
				continue
			}
			for _, attr := range event.Attributes {
				if string(attr.Key) == "reason" {
					reason = attr.Value.AsString()
				}
			}
		}
	}
	if reason != "no_active_generation" {
		t.Fatalf("documentation.empty_page reason = %q, want no_active_generation", reason)
	}
}

// --- handler --------------------------------------------------------------------

func documentationFactsGET(t *testing.T, store fakePortContentStore, target string) (map[string]any, *ResponseEnvelope) {
	t.Helper()
	handler := &DocumentationHandler{Content: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return resp.Data.(map[string]any), &resp
}

func TestDocumentationFactsHandlerLabelsActiveBinding(t *testing.T) {
	t.Parallel()

	data, resp := documentationFactsGET(t, fakePortContentStore{
		documentationFactsModel: documentationFactListReadModel{
			Facts:   []map[string]any{{"fact_id": "f1"}},
			Binding: querycontract.DocumentationFactGenerationBinding{GenerationID: "gen-active", IsActive: true},
		},
	}, "/api/v0/documentation/facts?scope_id=docs-scope")

	binding, ok := data["generation_binding"].(map[string]any)
	if !ok {
		t.Fatalf("generation_binding = %#v, want object", data["generation_binding"])
	}
	if binding["mode"] != "active" || binding["generation_id"] != "gen-active" || binding["is_active"] != true {
		t.Fatalf("generation_binding = %#v, want active/gen-active/true", binding)
	}
	if resp.Truth.Freshness.State != querycontract.FreshnessFresh {
		t.Fatalf("truth.freshness.state = %q, want fresh", resp.Truth.Freshness.State)
	}
	if states := data["states"].([]any); len(states) != 0 {
		t.Fatalf("states = %#v, want none for a non-empty page", states)
	}
}

func TestDocumentationFactsHandlerLabelsExplicitSupersededGenerationStale(t *testing.T) {
	t.Parallel()

	data, resp := documentationFactsGET(t, fakePortContentStore{
		documentationFactsModel: documentationFactListReadModel{
			Facts:   []map[string]any{{"fact_id": "f1"}},
			Binding: querycontract.DocumentationFactGenerationBinding{GenerationID: "gen-old", IsActive: false},
			Freshness: querycontract.DocumentationFactFreshness{
				State:  querycontract.FreshnessStale,
				Detail: "generation gen-old is superseded for scope docs-scope",
			},
		},
	}, "/api/v0/documentation/facts?scope_id=docs-scope&generation_id=gen-old")

	binding, ok := data["generation_binding"].(map[string]any)
	if !ok {
		t.Fatalf("generation_binding = %#v, want object", data["generation_binding"])
	}
	if binding["mode"] != "explicit" || binding["generation_id"] != "gen-old" || binding["is_active"] != false {
		t.Fatalf("generation_binding = %#v, want explicit/gen-old/false", binding)
	}
	if resp.Truth.Freshness.State != querycontract.FreshnessStale {
		t.Fatalf("truth.freshness.state = %q, want stale", resp.Truth.Freshness.State)
	}
	if !strings.Contains(resp.Truth.Freshness.Detail, "superseded") {
		t.Fatalf("truth.freshness.detail = %q, want superseded explanation", resp.Truth.Freshness.Detail)
	}
	if resp.Truth.Freshness.Cause != "" {
		t.Fatalf("stale generation must not invent a freshness cause, got %q", resp.Truth.Freshness.Cause)
	}
}

func TestDocumentationFactsHandlerExplainsEmptyScopePages(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		model      documentationFactListReadModel
		wantStates []string
		wantState  querycontract.FreshnessState
		wantCause  querycontract.FreshnessCause
	}{
		{
			name:       "scope not found",
			model:      documentationFactListReadModel{EmptyReason: "scope_not_found"},
			wantStates: []string{"no_documentation_facts", "scope_not_found"},
			wantState:  querycontract.FreshnessFresh,
		},
		{
			name: "no active generation after dead letter",
			model: documentationFactListReadModel{
				EmptyReason: "no_active_generation",
				Freshness: querycontract.DocumentationFactFreshness{
					State:  querycontract.FreshnessUnavailable,
					Cause:  querycontract.FreshnessCauseDeadLetteredDomain,
					Detail: "scope has no active generation",
				},
			},
			wantStates: []string{"no_documentation_facts", "no_active_generation"},
			wantState:  querycontract.FreshnessUnavailable,
			wantCause:  querycontract.FreshnessCauseDeadLetteredDomain,
		},
		{
			name: "no active generation yet",
			model: documentationFactListReadModel{
				EmptyReason: "no_active_generation",
				Freshness: querycontract.DocumentationFactFreshness{
					State:  querycontract.FreshnessBuilding,
					Cause:  querycontract.FreshnessCausePendingRepoGeneration,
					Detail: "scope has no active generation",
				},
			},
			wantStates: []string{"no_documentation_facts", "no_active_generation"},
			wantState:  querycontract.FreshnessBuilding,
			wantCause:  querycontract.FreshnessCausePendingRepoGeneration,
		},
		{
			name:       "active generation holds no matching facts",
			model:      documentationFactListReadModel{EmptyReason: "no_rows"},
			wantStates: []string{"no_documentation_facts"},
			wantState:  querycontract.FreshnessFresh,
		},
		{
			name:       "empty page without a proven reason",
			model:      documentationFactListReadModel{},
			wantStates: []string{"no_documentation_facts"},
			wantState:  querycontract.FreshnessFresh,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, resp := documentationFactsGET(t, fakePortContentStore{documentationFactsModel: tc.model},
				"/api/v0/documentation/facts?scope_id=docs-scope")
			var states []string
			for _, s := range data["states"].([]any) {
				states = append(states, s.(string))
			}
			if strings.Join(states, ",") != strings.Join(tc.wantStates, ",") {
				t.Fatalf("states = %v, want %v", states, tc.wantStates)
			}
			if data["missing_evidence"] != true {
				t.Fatalf("missing_evidence = %#v, want true", data["missing_evidence"])
			}
			if resp.Truth.Freshness.State != tc.wantState || resp.Truth.Freshness.Cause != tc.wantCause {
				t.Fatalf("truth.freshness = %#v, want state %q cause %q", resp.Truth.Freshness, tc.wantState, tc.wantCause)
			}
		})
	}
}

func TestDocumentationFactsHandlerAlwaysEmitsGenerationBinding(t *testing.T) {
	t.Parallel()

	// A store that reports nothing about the binding must still yield the field,
	// with the mode the request implies.
	data, _ := documentationFactsGET(t, fakePortContentStore{},
		"/api/v0/documentation/facts?repo=repository:r_1")
	binding, ok := data["generation_binding"].(map[string]any)
	if !ok || binding["mode"] != "active" {
		t.Fatalf("generation_binding = %#v, want an active-mode object", data["generation_binding"])
	}
}

func TestDocumentationFactsHandlerRecordsGenerationBindingSpanAttribute(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target string
		want   string
	}{
		{"scope", "/api/v0/documentation/facts?scope_id=docs-scope", "active_scope"},
		{"anchor", "/api/v0/documentation/facts?repo=repository:r_1", "active_join"},
		{"explicit", "/api/v0/documentation/facts?scope_id=docs-scope&generation_id=g1", "explicit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			previous := queryHandlerTracer
			queryHandlerTracer = provider.Tracer("documentation-facts-binding-test")
			t.Cleanup(func() { queryHandlerTracer = previous })

			documentationFactsGET(t, fakePortContentStore{}, tc.target)

			var got string
			for _, span := range recorder.Ended() {
				if span.Name() != "query.documentation_facts" {
					continue
				}
				for _, attr := range span.Attributes() {
					if string(attr.Key) == "eshu.documentation.generation_binding" {
						got = attr.Value.AsString()
					}
				}
			}
			if got != tc.want {
				t.Fatalf("eshu.documentation.generation_binding = %q, want %q", got, tc.want)
			}
		})
	}
}
