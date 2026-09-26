// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestDocumentationFindingsLabelsHistoricalGeneration(t *testing.T) {
	t.Parallel()
	model := documentationFindingListReadModel{
		Findings:  []map[string]any{{"generation_id": "generation:old"}},
		Binding:   querycontract.DocumentationFactGenerationBinding{GenerationID: "generation:old"},
		Freshness: querycontract.DocumentationFactFreshness{State: querycontract.FreshnessStale},
	}
	handler := &DocumentationHandler{
		Content: fakePortContentStore{documentationFindingsModel: model},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v0/documentation/findings?scope_id=scope:related&generation_id=generation:old", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Truth == nil || envelope.Truth.Freshness.State != querycontract.FreshnessStale {
		t.Fatalf("truth freshness = %#v, want stale", envelope.Truth)
	}
	binding, ok := envelope.Data.(map[string]any)["generation_binding"].(map[string]any)
	if !ok || binding["mode"] != "explicit" || binding["generation_id"] != "generation:old" || binding["is_active"] != false {
		t.Fatalf("generation binding = %#v, want explicit historical", binding)
	}
}

func TestSemanticEvidenceExplainsNoActiveGeneration(t *testing.T) {
	t.Parallel()
	model := semanticEvidenceListReadModel{
		Freshness: querycontract.DocumentationFactFreshness{
			State: querycontract.FreshnessUnavailable,
			Cause: querycontract.FreshnessCauseDeadLetteredDomain,
		},
		EmptyReason: querycontract.DocumentationFactEmptyNoActiveGeneration,
	}
	handler := &SemanticEvidenceHandler{
		Content: &fakeSemanticEvidenceStore{readModel: model},
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v0/semantic/documentation-observations?scope_id=scope:failed", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Truth == nil || envelope.Truth.Freshness.State != querycontract.FreshnessUnavailable {
		t.Fatalf("truth freshness = %#v, want unavailable", envelope.Truth)
	}
	data := envelope.Data.(map[string]any)
	states, ok := data["states"].([]any)
	if !ok || len(states) != 2 || states[1] != querycontract.DocumentationFactEmptyNoActiveGeneration {
		t.Fatalf("states = %#v, want no active generation", data["states"])
	}
	binding, ok := data["generation_binding"].(map[string]any)
	if !ok || binding["mode"] != "active" || binding["is_active"] != false {
		t.Fatalf("generation binding = %#v, want inactive scope", data["generation_binding"])
	}
}
