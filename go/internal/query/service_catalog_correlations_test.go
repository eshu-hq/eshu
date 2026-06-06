package query

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

type recordingServiceCatalogCorrelationStore struct {
	rows       []ServiceCatalogCorrelationRow
	lastFilter ServiceCatalogCorrelationFilter
}

func (s *recordingServiceCatalogCorrelationStore) ListServiceCatalogCorrelations(
	_ context.Context,
	filter ServiceCatalogCorrelationFilter,
) ([]ServiceCatalogCorrelationRow, error) {
	s.lastFilter = filter
	return append([]ServiceCatalogCorrelationRow(nil), s.rows...), nil
}

func TestServiceCatalogListCorrelationsRequiresScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &ServiceCatalogHandler{Correlations: &recordingServiceCatalogCorrelationStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/service-catalog/correlations?limit=10",
		"/api/v0/service-catalog/correlations?repository_id=repo-api",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
}

func TestServiceCatalogListCorrelationsUsesBoundedStore(t *testing.T) {
	t.Parallel()

	store := &recordingServiceCatalogCorrelationStore{
		rows: []ServiceCatalogCorrelationRow{
			{
				CorrelationID:  "catalog-correlation-1",
				Provider:       "backstage",
				EntityRef:      "component:default/checkout",
				EntityType:     "component",
				DisplayName:    "Checkout",
				RepositoryID:   "repo-checkout",
				ServiceID:      "service-checkout",
				OwnerRef:       "group:default/payments",
				Lifecycle:      "production",
				Tier:           "tier_1",
				Outcome:        "exact",
				Reason:         "catalog repository annotation matched canonical repository identity",
				DriftKind:      "owner",
				DriftStatus:    "matches",
				ProvenanceOnly: false,
			},
			{CorrelationID: "catalog-correlation-2", EntityRef: "component:default/catalog-only"},
		},
	}
	handler := &ServiceCatalogHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/service-catalog/correlations?repository_id=repo-checkout&owner_ref=group:default/payments&limit=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.RepositoryID, "repo-checkout"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.OwnerRef, "group:default/payments"; got != want {
		t.Fatalf("OwnerRef = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Limit, 2; got != want {
		t.Fatalf("Limit = %d, want %d", got, want)
	}

	var resp struct {
		Correlations []ServiceCatalogCorrelationResult `json:"correlations"`
		Count        int                               `json:"count"`
		Limit        int                               `json:"limit"`
		Truncated    bool                              `json:"truncated"`
		NextCursor   map[string]string                 `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Correlations), 1; got != want {
		t.Fatalf("len(correlations) = %d, want %d", got, want)
	}
	if got, want := resp.Correlations[0].EntityRef, "component:default/checkout"; got != want {
		t.Fatalf("EntityRef = %q, want %q", got, want)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.NextCursor["after_correlation_id"], "catalog-correlation-1"; got != want {
		t.Fatalf("next_cursor.after_correlation_id = %q, want %q", got, want)
	}
}

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
	if got, want := byID["catalog-correlation-ambiguous"].CandidateRepositoryIDs, []string{"repository:r_payments", "repository:r_payments_fork"}; !slices.Equal(got, want) {
		t.Fatalf("ambiguous candidate_repository_ids = %#v, want %#v", got, want)
	}
}

func TestServiceCatalogListCorrelationsReportsMissingEvidenceForRepositoryScope(t *testing.T) {
	t.Parallel()

	store := &recordingServiceCatalogCorrelationStore{}
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
		Missing      []ServiceCatalogMissingEvidence   `json:"missing_evidence"`
		Count        int                               `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Correlations), 0; got != want {
		t.Fatalf("len(correlations) = %d, want %d", got, want)
	}
	if got, want := resp.Count, 0; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if got, want := len(resp.Missing), 1; got != want {
		t.Fatalf("len(missing_evidence) = %d, want %d: %#v", got, want, resp.Missing)
	}
	if got, want := resp.Missing[0].Class, "repository_service_catalog_correlation"; got != want {
		t.Fatalf("missing_evidence[0].class = %q, want %q", got, want)
	}
}

func TestPostgresServiceCatalogCorrelationsResolveCandidateRepositoryIDs(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{"fact_id", "payload"},
			rows: [][]driver.Value{
				{
					"catalog-correlation-ambiguous",
					[]byte(`{
						"entity_ref": "component:default/payments-shared",
						"outcome": "ambiguous",
						"provenance_only": true,
						"candidate_repository_ids": ["repository:r_payments", "repository:r_payments_fork"]
					}`),
				},
			},
		},
	})
	store := NewPostgresServiceCatalogCorrelationStore(db)

	rows, err := store.ListServiceCatalogCorrelations(context.Background(), ServiceCatalogCorrelationFilter{
		RepositoryID: "repository:r_payments",
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("ListServiceCatalogCorrelations() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := rows[0].CandidateRepositoryIDs, []string{"repository:r_payments", "repository:r_payments_fork"}; !slices.Equal(got, want) {
		t.Fatalf("CandidateRepositoryIDs = %#v, want %#v", got, want)
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("len(queries) = %d, want %d", got, want)
	}
	if !strings.Contains(recorder.queries[0], "fact.payload->'candidate_repository_ids' ? $5") {
		t.Fatalf("query missing candidate repository predicate:\n%s", recorder.queries[0])
	}
}

func TestServiceCatalogCorrelationQueryUsesActiveFactReadModel(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.fact_kind = $1",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"fact.payload->>'entity_ref' = $4",
		"fact.payload->>'repository_id' = $5",
		"fact.payload->'candidate_repository_ids' ? $5",
		"fact.payload->>'owner_ref' = $8",
		"fact.payload->>'outcome' = $9",
	} {
		if !strings.Contains(listServiceCatalogCorrelationsQuery, want) {
			t.Fatalf("listServiceCatalogCorrelationsQuery missing %q:\n%s", want, listServiceCatalogCorrelationsQuery)
		}
	}
}

func serviceCatalogCorrelationResultsByID(
	rows []ServiceCatalogCorrelationResult,
) map[string]ServiceCatalogCorrelationResult {
	out := make(map[string]ServiceCatalogCorrelationResult, len(rows))
	for _, row := range rows {
		out[row.CorrelationID] = row
	}
	return out
}
