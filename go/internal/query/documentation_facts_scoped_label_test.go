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

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// documentationFactsScopedGET serves one facts request with the given auth
// context attached, or with none when authCtx is nil.
func documentationFactsScopedGET(
	t *testing.T,
	handler *DocumentationHandler,
	target string,
	authCtx *AuthContext,
) (map[string]any, *ResponseEnvelope, string) {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	if authCtx != nil {
		req = req.WithContext(ContextWithAuthContext(req.Context(), *authCtx))
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: status = %d, want 200; body = %s", target, w.Code, w.Body.String())
	}
	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return resp.Data.(map[string]any), &resp, w.Body.String()
}

// TestDocumentationFactsSpanBindingMatchesExecutedFormForScopedToken proves the
// span attribute names the form the SQL was built with. A scoped token's
// kind-only source read carries authorization predicates, which move it from
// the probe to the join; the attribute must say so (#7128 review F2).
func TestDocumentationFactsSpanBindingMatchesExecutedFormForScopedToken(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := queryHandlerTracer
	queryHandlerTracer = provider.Tracer("documentation-facts-scoped-binding-test")
	t.Cleanup(func() { queryHandlerTracer = previous })

	store := &recordingDocumentationFactsStore{}
	handler := &DocumentationHandler{Content: store, Profile: ProfileProduction}
	documentationFactsScopedGET(t, handler, "/api/v0/documentation/facts?fact_kind=source", &AuthContext{
		Mode:            AuthModeScoped,
		TenantID:        "tenant-a",
		WorkspaceID:     "workspace-a",
		AllowedScopeIDs: []string{"scope:granted"},
	})

	executed := documentationFactBindingForm(store.filter)
	if executed != documentationFactBindingActiveJoin {
		t.Fatalf("store received form %q, want %q (scoped token takes the join)", executed, documentationFactBindingActiveJoin)
	}
	query, _ := buildDocumentationFactsSQL(store.filter)
	if !strings.Contains(query, "JOIN ingestion_scopes") {
		t.Fatalf("executed SQL has no scope join:\n%s", query)
	}
	var got string
	for _, span := range recorder.Ended() {
		if span.Name() != "query.documentation_facts" {
			continue
		}
		for _, attr := range span.Attributes() {
			if string(attr.Key) == documentationGenerationBindingAttr {
				got = attr.Value.AsString()
			}
		}
	}
	if got != executed {
		t.Fatalf("%s = %q, want the executed form %q", documentationGenerationBindingAttr, got, executed)
	}
}

// TestDocumentationFactsNoGrantTokenMatchesUngrantedScope proves a scoped
// token with no grants at all gets the page an ungranted scope gets from the
// store: a scope read is not found, an explicit generation is unknown, and an
// anchor-only read is an empty active page (#7128 review F4).
func TestDocumentationFactsNoGrantTokenMatchesUngrantedScope(t *testing.T) {
	noGrants := &AuthContext{Mode: AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a"}
	for _, tc := range []struct {
		name       string
		target     string
		wantStates string
		wantActive bool
		wantState  querycontract.FreshnessState
	}{
		{"scope", "/api/v0/documentation/facts?scope_id=scope:x", "no_documentation_facts,scope_not_found", false, querycontract.FreshnessFresh},
		{"explicit", "/api/v0/documentation/facts?generation_id=generation:x&repo=r", "no_documentation_facts", false, querycontract.FreshnessUnavailable},
		{"anchor", "/api/v0/documentation/facts?repo=r", "no_documentation_facts", true, querycontract.FreshnessFresh},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &recordingDocumentationFactsStore{}
			handler := &DocumentationHandler{Content: store, Profile: ProfileProduction}
			data, resp, _ := documentationFactsScopedGET(t, handler, tc.target, noGrants)
			if store.called {
				t.Fatal("store was called for a token with no grants")
			}
			var states []string
			for _, s := range data["states"].([]any) {
				states = append(states, s.(string))
			}
			if got := strings.Join(states, ","); got != tc.wantStates {
				t.Fatalf("states = %q, want %q", got, tc.wantStates)
			}
			binding := data["generation_binding"].(map[string]any)
			if binding["is_active"] != tc.wantActive {
				t.Fatalf("generation_binding = %#v, want is_active=%v", binding, tc.wantActive)
			}
			if resp.Truth.Freshness.State != tc.wantState {
				t.Fatalf("truth.freshness.state = %q, want %q", resp.Truth.Freshness.State, tc.wantState)
			}
		})
	}
}

// recordingDocumentationFactsStore records the filter the handler passes to
// the store, so a test can check the form the SQL is built from.
type recordingDocumentationFactsStore struct {
	fakePortContentStore
	called bool
	filter documentationFactFilter
}

func (s *recordingDocumentationFactsStore) DocumentationFacts(
	_ context.Context,
	filter documentationFactFilter,
) (documentationFactListReadModel, error) {
	s.called = true
	s.filter = filter
	return documentationFactListReadModel{}, nil
}
