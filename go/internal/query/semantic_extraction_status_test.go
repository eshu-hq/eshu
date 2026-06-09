package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestStatusHandlerSemanticExtractionNoProviderReturnsEnvelope(t *testing.T) {
	t.Parallel()

	handler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, time.June, 8, 14, 0, 0, 0, time.UTC),
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/semantic-extraction", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /api/v0/status/semantic-extraction status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := envelope.Data.(map[string]any)
	if got, want := data["state"], "unavailable"; got != want {
		t.Fatalf("data.state = %#v, want %#v", got, want)
	}
	if got, want := data["reason"], "provider_not_configured"; got != want {
		t.Fatalf("data.reason = %#v, want %#v", got, want)
	}
	if got, want := data["code_hints_enabled"], false; got != want {
		t.Fatalf("data.code_hints_enabled = %#v, want %#v", got, want)
	}
	if got, want := data["documentation_observations_enabled"], false; got != want {
		t.Fatalf("data.documentation_observations_enabled = %#v, want %#v", got, want)
	}
	if got, want := data["deterministic_paths_affected"], false; got != want {
		t.Fatalf("data.deterministic_paths_affected = %#v, want %#v", got, want)
	}
	if envelope.Truth == nil {
		t.Fatal("truth = nil, want semantic status envelope")
	}
	if got, want := envelope.Truth.Capability, semanticExtractionStatusCapability; got != want {
		t.Fatalf("truth.capability = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Freshness.State, FreshnessUnavailable; got != want {
		t.Fatalf("truth.freshness.state = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Level, TruthLevelFallback; got != want {
		t.Fatalf("truth.level = %q, want %q", got, want)
	}
}

func TestStatusIndexIncludesSemanticExtractionNoProvider(t *testing.T) {
	t.Parallel()

	handler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, time.June, 8, 14, 0, 0, 0, time.UTC),
			},
		},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/index", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /api/v0/status/index status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	semantic, ok := payload["semantic_extraction"].(map[string]any)
	if !ok {
		t.Fatalf("semantic_extraction missing or wrong type: %#v", payload["semantic_extraction"])
	}
	if got, want := semantic["state"], "unavailable"; got != want {
		t.Fatalf("semantic_extraction.state = %#v, want %#v", got, want)
	}
	if got, want := semantic["deterministic_paths_affected"], false; got != want {
		t.Fatalf("semantic_extraction.deterministic_paths_affected = %#v, want %#v", got, want)
	}
}

func TestSemanticNoProviderDoesNotChangeDocumentationFactsTruth(t *testing.T) {
	t.Parallel()

	statusHandler := &StatusHandler{
		StatusReader: fakeStatusReader{snapshot: statuspkg.RawSnapshot{}},
		Profile:      ProfileProduction,
	}
	statusMux := http.NewServeMux()
	statusHandler.Mount(statusMux)
	statusReq := httptest.NewRequest(http.MethodGet, "/api/v0/status/semantic-extraction", nil)
	statusReq.Header.Set("Accept", EnvelopeMIMEType)
	statusRec := httptest.NewRecorder()
	statusMux.ServeHTTP(statusRec, statusReq)
	if got, want := statusRec.Code, http.StatusOK; got != want {
		t.Fatalf("semantic status = %d, want %d; body=%s", got, want, statusRec.Body.String())
	}

	documentationHandler := &DocumentationHandler{
		Content: fakePortContentStore{
			documentationFactsModel: documentationFactListReadModel{
				Facts: []map[string]any{{
					"fact_id":   "fact:doc:1",
					"fact_kind": "documentation_section",
					"payload": map[string]any{
						"document_id":  "doc:source:1",
						"section_id":   "body",
						"heading_text": "Runbook",
					},
				}},
			},
		},
		Profile: ProfileProduction,
	}
	docMux := http.NewServeMux()
	documentationHandler.Mount(docMux)
	docReq := httptest.NewRequest(http.MethodGet, "/api/v0/documentation/facts?scope_id=docs-scope&fact_kind=documentation_section&limit=1", nil)
	docReq.Header.Set("Accept", EnvelopeMIMEType)
	docRec := httptest.NewRecorder()
	docMux.ServeHTTP(docRec, docReq)
	if got, want := docRec.Code, http.StatusOK; got != want {
		t.Fatalf("documentation facts status = %d, want %d; body=%s", got, want, docRec.Body.String())
	}
	var docEnvelope ResponseEnvelope
	if err := json.Unmarshal(docRec.Body.Bytes(), &docEnvelope); err != nil {
		t.Fatalf("json.Unmarshal(documentation facts) error = %v, want nil", err)
	}
	if docEnvelope.Truth == nil {
		t.Fatal("documentation facts truth = nil, want unchanged source-only envelope")
	}
	if got, want := docEnvelope.Truth.Capability, documentationFactsCapability; got != want {
		t.Fatalf("documentation facts truth.capability = %q, want %q", got, want)
	}
	if got, want := docEnvelope.Truth.Freshness.State, FreshnessFresh; got != want {
		t.Fatalf("documentation facts freshness = %q, want %q", got, want)
	}
}
