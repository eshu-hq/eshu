// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/repository"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// fakeRelationshipEvidenceContentStore answers the relationship evidence
// read with a fixed read model. It embeds the content store port so the
// handler's Content field accepts it; only the read-model method is
// exercised, and any other store read panics instead of silently answering.
type fakeRelationshipEvidenceContentStore struct {
	querycontract.ContentStore
	readModel repository.RelationshipEvidenceReadModel
}

func (s fakeRelationshipEvidenceContentStore) RelationshipEvidenceByResolvedID(
	context.Context, string,
) (repository.RelationshipEvidenceReadModel, error) {
	return s.readModel, nil
}

func TestEvidenceHandlerReturnsRelationshipEvidenceByResolvedID(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Content: fakeRelationshipEvidenceContentStore{
			readModel: repository.RelationshipEvidenceReadModel{
				Available: true,
				Row: map[string]any{
					"lookup_basis":      "resolved_id",
					"resolved_id":       "resolved-1",
					"generation_id":     "gen-1",
					"relationship_type": "DEPLOYS_FROM",
					"confidence":        0.93,
					"evidence_count":    2,
					"evidence_kinds":    []string{"HELM_VALUES_REFERENCE", "ARGOCD_APPLICATIONSET_GENERATOR"},
					"source": map[string]any{
						"repo_id":   "repo-deploy",
						"repo_name": "platform-deployments",
					},
					"target": map[string]any{
						"repo_id":   "repo-service",
						"repo_name": "checkout-service",
					},
					"evidence_preview": []any{
						map[string]any{
							"kind":       "HELM_VALUES_REFERENCE",
							"confidence": 0.91,
							"details": map[string]any{
								"path":       "charts/checkout/values.yaml",
								"start_line": 12,
							},
						},
					},
				},
			},
		},
		Profile: querycontract.ProfileProduction,
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/evidence/relationships/resolved-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["resolved_id"], "resolved-1"; got != want {
		t.Fatalf("resolved_id = %#v, want %#v", got, want)
	}
	if got, want := resp["lookup_basis"], "resolved_id"; got != want {
		t.Fatalf("lookup_basis = %#v, want %#v", got, want)
	}
	if got, want := resp["relationship_type"], "DEPLOYS_FROM"; got != want {
		t.Fatalf("relationship_type = %#v, want %#v", got, want)
	}
	if got, want := resp["confidence_basis"], "evidence_aggregate"; got != want {
		t.Fatalf("confidence_basis = %#v, want %#v", got, want)
	}
	source := resp["source"].(map[string]any)
	if got, want := source["repo_name"], "platform-deployments"; got != want {
		t.Fatalf("source.repo_name = %#v, want %#v", got, want)
	}
	target := resp["target"].(map[string]any)
	if got, want := target["repo_name"], "checkout-service"; got != want {
		t.Fatalf("target.repo_name = %#v, want %#v", got, want)
	}
	if !testutil.AnySliceContains(resp["evidence_kinds"].([]any), "HELM_VALUES_REFERENCE") {
		t.Fatalf("evidence_kinds = %#v, want HELM_VALUES_REFERENCE", resp["evidence_kinds"])
	}
}

func TestEvidenceHandlerReturnsNotFoundForMissingRelationshipEvidence(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Content: fakeRelationshipEvidenceContentStore{},
		Profile: querycontract.ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/evidence/relationships/missing-resolved-id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestListAdmissionDecisionsReturnsEmptyPageWithoutAccessScope(t *testing.T) {
	t.Parallel()

	handler := &Handler{Profile: querycontract.ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/evidence/admission-decisions?domain=terraform&scope_id=scope-1&generation_id=gen-1", nil)
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{Mode: auth.AuthModeScoped}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := resp["count"], float64(0); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	if got, want := resp["truncated"], false; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
}

func TestBuildEvidenceCitationsRequiresContentStore(t *testing.T) {
	t.Parallel()

	handler := &Handler{Profile: querycontract.ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/evidence/citations", strings.NewReader(`{"handles":[]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotImplemented; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}
