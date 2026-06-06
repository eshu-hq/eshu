package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

type recordingServiceCatalogCorrelationStore struct {
	rows                   []ServiceCatalogCorrelationRow
	descriptorRows         []ServiceCatalogLocalDescriptorEvidenceRow
	lastFilter             ServiceCatalogCorrelationFilter
	lastDescriptorRepoID   string
	lastDescriptorRowLimit int
}

func (s *recordingServiceCatalogCorrelationStore) ListServiceCatalogCorrelations(
	_ context.Context,
	filter ServiceCatalogCorrelationFilter,
) ([]ServiceCatalogCorrelationRow, error) {
	s.lastFilter = filter
	return append([]ServiceCatalogCorrelationRow(nil), s.rows...), nil
}

func (s *recordingServiceCatalogCorrelationStore) ListServiceCatalogLocalDescriptorEvidence(
	_ context.Context,
	repositoryID string,
	limit int,
) ([]ServiceCatalogLocalDescriptorEvidenceRow, error) {
	s.lastDescriptorRepoID = repositoryID
	s.lastDescriptorRowLimit = limit
	if limit > 0 && limit < len(s.descriptorRows) {
		return append([]ServiceCatalogLocalDescriptorEvidenceRow(nil), s.descriptorRows[:limit]...), nil
	}
	return append([]ServiceCatalogLocalDescriptorEvidenceRow(nil), s.descriptorRows...), nil
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
	wantCandidates := []string{"repository:r_payments", "repository:r_payments_fork"}
	if got := byID["catalog-correlation-ambiguous"].CandidateRepositoryIDs; !slices.Equal(got, wantCandidates) {
		t.Fatalf("ambiguous candidate_repository_ids = %#v, want %#v", got, wantCandidates)
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
		Correlations    []ServiceCatalogCorrelationResult `json:"correlations"`
		Missing         []ServiceCatalogMissingEvidence   `json:"missing_evidence"`
		Count           int                               `json:"count"`
		EvidenceSummary ServiceCatalogEvidenceSummary     `json:"evidence_summary"`
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
	if got, want := resp.EvidenceSummary.LocalDescriptors.State, "absent"; got != want {
		t.Fatalf("local_descriptors.state = %q, want %q", got, want)
	}
}

func TestServiceCatalogListCorrelationsExplainsLocalOnlyDescriptorEvidence(t *testing.T) {
	t.Parallel()

	store := &recordingServiceCatalogCorrelationStore{
		descriptorRows: []ServiceCatalogLocalDescriptorEvidenceRow{{
			FactID:    "catalog-fact-1",
			FactKind:  "service_catalog.entity",
			Provider:  "backstage",
			EntityRef: "component:default/checkout",
			SourceURI: "file://repo/catalog-info.yaml",
		}},
	}
	handler := &ServiceCatalogHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/service-catalog/correlations?repository_id=repo-checkout&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastDescriptorRepoID, "repo-checkout"; got != want {
		t.Fatalf("descriptor repositoryID = %q, want %q", got, want)
	}

	var resp struct {
		Correlations    []ServiceCatalogCorrelationResult `json:"correlations"`
		EvidenceSummary ServiceCatalogEvidenceSummary     `json:"evidence_summary"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got := len(resp.Correlations); got != 0 {
		t.Fatalf("len(correlations) = %d, want 0", got)
	}
	if got, want := resp.EvidenceSummary.LocalDescriptors.State, "present"; got != want {
		t.Fatalf("local_descriptors.state = %q, want %q", got, want)
	}
	if got, want := resp.EvidenceSummary.LocalDescriptors.Count, 1; got != want {
		t.Fatalf("local_descriptors.count = %d, want %d", got, want)
	}
	if got, want := resp.EvidenceSummary.LocalDescriptors.Providers, []string{"backstage"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("local_descriptors.providers = %#v, want %#v", got, want)
	}
	if got, want := resp.EvidenceSummary.ExternalCatalogConfirmation.State, "missing"; got != want {
		t.Fatalf("external_catalog_confirmation.state = %q, want %q", got, want)
	}
	if got, want := resp.EvidenceSummary.ExternalCatalogConfirmation.Reason, "local_descriptor_without_catalog_correlation"; got != want {
		t.Fatalf("external_catalog_confirmation.reason = %q, want %q", got, want)
	}
}

func TestServiceCatalogListCorrelationsExplainsExternalCatalogMatch(t *testing.T) {
	t.Parallel()

	store := &recordingServiceCatalogCorrelationStore{
		rows: []ServiceCatalogCorrelationRow{{
			CorrelationID: "catalog-correlation-1",
			RepositoryID:  "repo-checkout",
			EntityRef:     "component:default/checkout",
			Outcome:       "exact",
			Reason:        "catalog repository id matches canonical repository identity",
		}},
		descriptorRows: []ServiceCatalogLocalDescriptorEvidenceRow{{
			FactID:    "catalog-fact-1",
			FactKind:  "service_catalog.repository_link",
			Provider:  "backstage",
			EntityRef: "component:default/checkout",
			SourceURI: "file://repo/catalog-info.yaml",
		}},
	}
	handler := &ServiceCatalogHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/service-catalog/correlations?repository_id=repo-checkout&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Count           int                           `json:"count"`
		EvidenceSummary ServiceCatalogEvidenceSummary `json:"evidence_summary"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp.Count, 1; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if got, want := resp.EvidenceSummary.ExternalCatalogConfirmation.State, "present"; got != want {
		t.Fatalf("external_catalog_confirmation.state = %q, want %q", got, want)
	}
	if got, want := resp.EvidenceSummary.ExternalCatalogConfirmation.Count, 1; got != want {
		t.Fatalf("external_catalog_confirmation.count = %d, want %d", got, want)
	}
}

func TestServiceCatalogListCorrelationsExplainsAmbiguousLocalDescriptor(t *testing.T) {
	t.Parallel()

	store := &recordingServiceCatalogCorrelationStore{
		rows: []ServiceCatalogCorrelationRow{{
			CorrelationID: "catalog-correlation-1",
			RepositoryID:  "repo-checkout",
			EntityRef:     "component:default/checkout",
			Outcome:       "ambiguous",
			Reason:        "repo-local catalog descriptor scope matches multiple active repository facts",
		}},
		descriptorRows: []ServiceCatalogLocalDescriptorEvidenceRow{{
			FactID:    "catalog-fact-1",
			FactKind:  "service_catalog.entity",
			Provider:  "backstage",
			EntityRef: "component:default/checkout",
			SourceURI: "file://repo/catalog-info.yaml",
		}},
	}
	handler := &ServiceCatalogHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/service-catalog/correlations?repository_id=repo-checkout&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		EvidenceSummary ServiceCatalogEvidenceSummary `json:"evidence_summary"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp.EvidenceSummary.LocalDescriptors.State, "present"; got != want {
		t.Fatalf("local_descriptors.state = %q, want %q", got, want)
	}
	if got, want := resp.EvidenceSummary.ExternalCatalogConfirmation.State, "missing"; got != want {
		t.Fatalf("external_catalog_confirmation.state = %q, want %q", got, want)
	}
	if got, want := resp.EvidenceSummary.ExternalCatalogConfirmation.Reason, "local_descriptor_ambiguous"; got != want {
		t.Fatalf("external_catalog_confirmation.reason = %q, want %q", got, want)
	}
}

func TestServiceCatalogListCorrelationsExplainsNoEvidence(t *testing.T) {
	t.Parallel()

	store := &recordingServiceCatalogCorrelationStore{}
	handler := &ServiceCatalogHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/service-catalog/correlations?repository_id=repo-checkout&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		EvidenceSummary ServiceCatalogEvidenceSummary `json:"evidence_summary"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp.EvidenceSummary.LocalDescriptors.State, "absent"; got != want {
		t.Fatalf("local_descriptors.state = %q, want %q", got, want)
	}
	if got, want := resp.EvidenceSummary.ExternalCatalogConfirmation.State, "missing"; got != want {
		t.Fatalf("external_catalog_confirmation.state = %q, want %q", got, want)
	}
	if got, want := resp.EvidenceSummary.Reason, "no_service_catalog_evidence_found"; got != want {
		t.Fatalf("evidence_summary.reason = %q, want %q", got, want)
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
