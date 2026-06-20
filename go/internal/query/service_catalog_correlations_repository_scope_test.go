package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestServiceCatalogListCorrelationsExplainsRepositoryScopedEvidence(t *testing.T) {
	t.Parallel()

	store := &recordingServiceCatalogCorrelationStore{
		rows: []ServiceCatalogCorrelationRow{
			{
				CorrelationID: "catalog-correlation-direct-service",
				EntityRef:     "component:default/payments-api",
				RepositoryID:  "repository:r_payments",
				ServiceID:     "component:default/payments-api",
				Outcome:       "exact",
			},
			{
				CorrelationID: "catalog-correlation-workload-only",
				EntityRef:     "component:default/payments-worker",
				RepositoryID:  "repository:r_payments",
				WorkloadID:    "workload:payments-worker",
				Outcome:       "exact",
			},
			{
				CorrelationID:          "catalog-correlation-ambiguous",
				EntityRef:              "component:default/payments-shared",
				CandidateRepositoryIDs: []string{"repository:r_payments", "repository:r_payments_fork"},
				Outcome:                "ambiguous",
				Reason:                 "repo-local catalog descriptor scope matches multiple active repository facts",
				RequiredAnchorKeys: []string{
					"repository_id",
					"normalized_url|repository_url|raw_url|url",
					"git-repository-scope:<repo_id>",
				},
			},
		},
	}
	handler := &ServiceCatalogHandler{
		Content:      repositorySelectorReadModelContentStore(),
		Correlations: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/service-catalog/correlations?repository_id=payments-api&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Correlations []ServiceCatalogCorrelationResult `json:"correlations"`
		Count        int                               `json:"count"`
		Missing      []ServiceCatalogMissingEvidence   `json:"missing_evidence"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp.Count, 3; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if len(resp.Missing) != 0 {
		t.Fatalf("missing_evidence = %#v, want empty when rows explain the repository scope", resp.Missing)
	}
	byID := serviceCatalogCorrelationResultsByID(resp.Correlations)
	if got, want := byID["catalog-correlation-direct-service"].ServiceID, "component:default/payments-api"; got != want {
		t.Fatalf("direct service_id = %q, want %q", got, want)
	}
	workloadOnly := byID["catalog-correlation-workload-only"]
	if got, want := workloadOnly.WorkloadID, "workload:payments-worker"; got != want {
		t.Fatalf("workload-only workload_id = %q, want %q", got, want)
	}
	if workloadOnly.ServiceID != "" {
		t.Fatalf("workload-only service_id = %q, want empty without service proof", workloadOnly.ServiceID)
	}
	wantCandidates := []string{"repository:r_payments", "repository:r_payments_fork"}
	if got := byID["catalog-correlation-ambiguous"].CandidateRepositoryIDs; !slices.Equal(got, wantCandidates) {
		t.Fatalf("ambiguous candidate_repository_ids = %#v, want %#v", got, wantCandidates)
	}
	wantAnchors := []string{
		"repository_id",
		"normalized_url|repository_url|raw_url|url",
		"git-repository-scope:<repo_id>",
	}
	if got := byID["catalog-correlation-ambiguous"].RequiredAnchorKeys; !slices.Equal(got, wantAnchors) {
		t.Fatalf("ambiguous required_anchor_keys = %#v, want %#v", got, wantAnchors)
	}
}
