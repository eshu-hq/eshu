// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestStatusHandlerSemanticExtractionReturnsRedactedProviderProfiles(t *testing.T) {
	t.Parallel()

	const credentialHandle = "DEEPSEEK_API_KEY"
	handler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, time.June, 8, 14, 0, 0, 0, time.UTC),
				SemanticExtraction: statuspkg.SemanticExtractionStatus{
					ProviderProfiles: []statuspkg.SemanticProviderProfileStatus{
						{
							ProfileID:              "semantic-docs-default",
							DisplayName:            "Documentation default",
							ProviderKind:           "deepseek",
							CredentialSourceKind:   "environment_variable",
							CredentialConfigured:   true,
							ModelID:                "deepseek-chat",
							EmbeddingDimensions:    3,
							EndpointProfileID:      "deepseek-public-api",
							SourceClasses:          []string{"documentation"},
							SourcePolicyConfigured: true,
							State:                  statuspkg.SemanticProviderProfileConfigured,
						},
					},
				},
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
	if body := rec.Body.String(); strings.Contains(body, credentialHandle) {
		t.Fatalf("semantic extraction status leaked credential handle %q: %s", credentialHandle, body)
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := envelope.Data.(map[string]any)
	if got, want := data["state"], "available"; got != want {
		t.Fatalf("data.state = %#v, want %#v", got, want)
	}
	if got, want := data["provider_configured"], true; got != want {
		t.Fatalf("data.provider_configured = %#v, want %#v", got, want)
	}
	profiles, ok := data["provider_profiles"].([]any)
	if !ok {
		t.Fatalf("data.provider_profiles missing or wrong type: %#v", data["provider_profiles"])
	}
	if len(profiles) != 1 {
		t.Fatalf("len(data.provider_profiles) = %d, want 1", len(profiles))
	}
	profile := profiles[0].(map[string]any)
	if got, want := profile["profile_id"], "semantic-docs-default"; got != want {
		t.Fatalf("profile.profile_id = %#v, want %#v", got, want)
	}
	if got, want := profile["credential_source_kind"], "environment_variable"; got != want {
		t.Fatalf("profile.credential_source_kind = %#v, want %#v", got, want)
	}
	if got, want := profile["embedding_dimensions"], float64(3); got != want {
		t.Fatalf("profile.embedding_dimensions = %#v, want %#v", got, want)
	}
	if _, ok := profile["credential_handle"]; ok {
		t.Fatalf("profile exposed credential_handle: %#v", profile)
	}
}

func TestStatusHandlerSemanticExtractionReturnsQueueBudgetAuditReadbacks(t *testing.T) {
	t.Parallel()

	const rawProviderResponse = "provider response body must not appear"
	handler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, time.June, 9, 6, 0, 0, 0, time.UTC),
				SemanticExtraction: statuspkg.SemanticExtractionStatus{
					State:              statuspkg.SemanticExtractionAvailable,
					ProviderConfigured: true,
					Queue: statuspkg.SemanticExtractionQueueSnapshot{
						Total:           4,
						Pending:         2,
						Succeeded:       1,
						BudgetExhausted: 1,
						ProviderProfileCounts: []statuspkg.SemanticExtractionProviderProfileQueueCount{
							{
								ProviderKind:         "deepseek",
								ProviderProfileID:    "semantic-docs-default",
								ProviderProfileClass: "hosted",
								Count:                4,
							},
						},
						FailureClassCounts: []statuspkg.NamedCount{{Name: "provider_unavailable", Count: 1}},
					},
					Budget: statuspkg.SemanticExtractionBudgetSnapshot{
						EstimatedInputTokens: 400,
						ActualCostMicros:     120,
						Exhausted:            1,
					},
					Audit: statuspkg.SemanticExtractionAuditSnapshot{
						ActorClassCounts: []statuspkg.NamedCount{{Name: "hosted_worker", Count: 4}},
						ACLStateCounts:   []statuspkg.NamedCount{{Name: "acl_allowed", Count: 4}},
					},
					Detail: rawProviderResponse,
				},
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
	if body := rec.Body.String(); strings.Contains(body, rawProviderResponse) ||
		strings.Contains(body, "prompt_text") || strings.Contains(body, "response_text") {
		t.Fatalf("semantic extraction status leaked raw provider or prompt data: %s", body)
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := envelope.Data.(map[string]any)
	if got, want := data["deterministic_paths_affected"], false; got != want {
		t.Fatalf("data.deterministic_paths_affected = %#v, want %#v", got, want)
	}
	queue, ok := data["queue"].(map[string]any)
	if !ok {
		t.Fatalf("data.queue missing or wrong type: %#v", data["queue"])
	}
	if got, want := queue["budget_exhausted"], float64(1); got != want {
		t.Fatalf("queue.budget_exhausted = %#v, want %#v", got, want)
	}
	budget := data["budget"].(map[string]any)
	if got, want := budget["estimated_input_tokens"], float64(400); got != want {
		t.Fatalf("budget.estimated_input_tokens = %#v, want %#v", got, want)
	}
	audit := data["audit"].(map[string]any)
	if _, ok := audit["acl_state_counts"]; !ok {
		t.Fatalf("audit.acl_state_counts missing: %#v", audit)
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
