// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSemanticEvidenceHandlerListsCodeHintsWithTruthMetadata proves the
// semantic_evidence.code_hints.list capability end to end through the real
// GET /api/v0/semantic/code-hints route. The only committed coverage of
// SemanticEvidenceHandler.list
// (TestSemanticEvidenceHandlerListsDocumentationObservationsWithTruthMetadata)
// drives it through the documentation-observations route with fact kind
// facts.SemanticDocumentationObservationFactKind; the code-hints route
// dispatches the same shared handler with
// facts.SemanticCodeHintFactKind and the "code_hints" response key, and had
// no route-level assertion of its own before this test (issue #5681
// cluster A).
func TestSemanticEvidenceHandlerListsCodeHintsWithTruthMetadata(t *testing.T) {
	t.Parallel()

	store := &fakeSemanticEvidenceStore{
		readModel: semanticEvidenceListReadModel{
			Rows: []map[string]any{{
				"fact_id":             "fact:semantic-hint-1",
				"fact_kind":           facts.SemanticCodeHintFactKind,
				"truth_basis":         "semantic_observation",
				"provider_profile_id": "semantic-hints-default",
				"hint_type":           "likely_relationship",
				"target_kind":         "function",
				"target_id":           "function-1",
				"freshness_state":     facts.SemanticFreshnessFresh,
				"policy_state":        facts.SemanticPolicyAllowed,
			}},
		},
	}
	handler := &SemanticEvidenceHandler{Content: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/semantic/code-hints?repo=repo:payments&hint_type=likely_relationship&limit=1",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.filter.FactKind, facts.SemanticCodeHintFactKind; got != want {
		t.Fatalf("filter.FactKind = %q, want %q", got, want)
	}
	if got, want := store.filter.HintType, "likely_relationship"; got != want {
		t.Fatalf("filter.HintType = %q, want %q", got, want)
	}
	if got, want := store.filter.Repository, "repo:payments"; got != want {
		t.Fatalf("filter.Repository = %q, want %q", got, want)
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Truth == nil {
		t.Fatal("truth envelope = nil, want semantic evidence truth")
	}
	if got, want := envelope.Truth.Capability, semanticCodeHintsCapability; got != want {
		t.Fatalf("truth.capability = %q, want %q", got, want)
	}
	if got, want := envelope.Truth.Basis, TruthBasisSemanticFacts; got != want {
		t.Fatalf("truth.basis = %q, want %q", got, want)
	}

	data := envelope.Data.(map[string]any)
	rows, ok := data["code_hints"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("data[code_hints] = %#v, want one code hint row", data["code_hints"])
	}
	row, ok := rows[0].(map[string]any)
	if !ok {
		t.Fatalf("code_hints[0] type = %T, want map[string]any", rows[0])
	}
	if got, want := row["fact_kind"], facts.SemanticCodeHintFactKind; got != want {
		t.Fatalf("code_hints[0][fact_kind] = %#v, want %#v", got, want)
	}
	if got, want := row["hint_type"], "likely_relationship"; got != want {
		t.Fatalf("code_hints[0][hint_type] = %#v, want %#v", got, want)
	}
}
