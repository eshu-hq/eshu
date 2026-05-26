package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubSupplyChainImpactAggregateStore struct {
	count          SupplyChainImpactAggregateCount
	countErr       error
	inventory      []SupplyChainImpactInventoryRow
	inventoryErr   error
	lastFilter     SupplyChainImpactAggregateFilter
	lastDimension  SupplyChainImpactInventoryDimension
	lastLimit      int
	lastOffset     int
	callCountCount int
	callInvCount   int
}

func (s *stubSupplyChainImpactAggregateStore) CountSupplyChainImpactFindings(
	_ context.Context,
	filter SupplyChainImpactAggregateFilter,
) (SupplyChainImpactAggregateCount, error) {
	s.callCountCount++
	s.lastFilter = filter
	if s.countErr != nil {
		return SupplyChainImpactAggregateCount{}, s.countErr
	}
	return s.count, nil
}

func (s *stubSupplyChainImpactAggregateStore) SupplyChainImpactInventory(
	_ context.Context,
	filter SupplyChainImpactAggregateFilter,
	dim SupplyChainImpactInventoryDimension,
	limit int,
	offset int,
) ([]SupplyChainImpactInventoryRow, error) {
	s.callInvCount++
	s.lastFilter = filter
	s.lastDimension = dim
	s.lastLimit = limit
	s.lastOffset = offset
	if s.inventoryErr != nil {
		return nil, s.inventoryErr
	}
	return append([]SupplyChainImpactInventoryRow(nil), s.inventory...), nil
}

func TestSupplyChainImpactAggregateRoutesReturn503WhenStoreMissing(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/impact/findings/count",
		"/api/v0/supply-chain/impact/inventory",
	} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusServiceUnavailable; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
}

func TestSupplyChainImpactAggregateCountReturnsTotals(t *testing.T) {
	t.Parallel()

	store := &stubSupplyChainImpactAggregateStore{
		count: SupplyChainImpactAggregateCount{
			TotalFindings:    7,
			AffectedFindings: 4,
			AffectedExact:    3,
			AffectedRange:    1,
			NotAffected:      3,
			ByPriorityBucket: map[string]int{"critical": 2, "high": 5},
			BySeverity:       map[string]int{"critical": 2, "high": 4, "medium": 1},
		},
	}
	handler := &SupplyChainHandler{ImpactAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/findings/count?repository_id=repo-A", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.callCountCount != 1 {
		t.Fatalf("Count called %d times, want 1", store.callCountCount)
	}
	if got, want := store.lastFilter.RepositoryID, "repo-A"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	var body struct {
		TotalFindings    int            `json:"total_findings"`
		AffectedFindings int            `json:"affected_findings"`
		NotAffected      int            `json:"not_affected"`
		ByPriorityBucket map[string]int `json:"by_priority_bucket"`
		BySeverity       map[string]int `json:"by_severity"`
		Scope            map[string]any `json:"scope"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	if body.TotalFindings != 7 {
		t.Fatalf("total_findings = %d, want 7; body = %s", body.TotalFindings, w.Body.String())
	}
	if body.AffectedFindings != 4 {
		t.Fatalf("affected_findings = %d, want 4", body.AffectedFindings)
	}
	if body.ByPriorityBucket["high"] != 5 {
		t.Fatalf("by_priority_bucket[high] = %d, want 5", body.ByPriorityBucket["high"])
	}
	if body.BySeverity["critical"] != 2 {
		t.Fatalf("by_severity[critical] = %d, want 2", body.BySeverity["critical"])
	}
	if body.Scope["repository_id"] != "repo-A" {
		t.Fatalf("scope.repository_id = %v, want repo-A", body.Scope["repository_id"])
	}
}

func TestSupplyChainImpactAggregateInventoryReturnsBuckets(t *testing.T) {
	t.Parallel()

	store := &stubSupplyChainImpactAggregateStore{
		inventory: []SupplyChainImpactInventoryRow{
			{Dimension: SupplyChainImpactInventoryByImpactStatus, Value: "affected_exact", Count: 12},
			{Dimension: SupplyChainImpactInventoryByImpactStatus, Value: "affected_range", Count: 3},
			{Dimension: SupplyChainImpactInventoryByImpactStatus, Value: "not_affected_known_fixed", Count: 1},
		},
	}
	handler := &SupplyChainHandler{ImpactAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/inventory?group_by=impact_status&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.lastDimension != SupplyChainImpactInventoryByImpactStatus {
		t.Fatalf("dimension = %q, want impact_status", store.lastDimension)
	}
	if store.lastLimit != 11 {
		t.Fatalf("internal limit = %d, want 11 (caller limit + 1)", store.lastLimit)
	}
	var body struct {
		Buckets   []map[string]any `json:"buckets"`
		Count     int              `json:"count"`
		GroupBy   string           `json:"group_by"`
		Truncated bool             `json:"truncated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	if body.Count != 3 {
		t.Fatalf("count = %d, want 3", body.Count)
	}
	if body.GroupBy != "impact_status" {
		t.Fatalf("group_by = %q, want impact_status", body.GroupBy)
	}
	if body.Truncated {
		t.Fatalf("truncated = true, want false (only 3 buckets, limit 10)")
	}
}

func TestSupplyChainImpactAggregateInventoryReportsTruncated(t *testing.T) {
	t.Parallel()

	rows := make([]SupplyChainImpactInventoryRow, 6)
	for i := range rows {
		rows[i] = SupplyChainImpactInventoryRow{
			Dimension: SupplyChainImpactInventoryByRepository,
			Value:     "repo",
			Count:     i,
		}
	}
	store := &stubSupplyChainImpactAggregateStore{inventory: rows}
	handler := &SupplyChainHandler{ImpactAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/inventory?group_by=repository_id&limit=5", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var body struct {
		Count      int  `json:"count"`
		Truncated  bool `json:"truncated"`
		NextOffset int  `json:"next_offset"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	if body.Count != 5 {
		t.Fatalf("count = %d, want 5 (page trim)", body.Count)
	}
	if !body.Truncated {
		t.Fatalf("truncated = false, want true")
	}
	if body.NextOffset != 5 {
		t.Fatalf("next_offset = %d, want 5", body.NextOffset)
	}
}

func TestSupplyChainImpactAggregateInventoryRejectsUnknownDimension(t *testing.T) {
	t.Parallel()

	store := &stubSupplyChainImpactAggregateStore{}
	handler := &SupplyChainHandler{ImpactAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/inventory?group_by=ecosystem", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.callInvCount != 0 {
		t.Fatalf("store called for unknown dimension")
	}
}

func TestSupplyChainImpactAggregateInventoryRejectsOversizedLimit(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{ImpactAggregates: &stubSupplyChainImpactAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/inventory?limit=9999", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestSupplyChainImpactAggregateInventoryRejectsNegativeOffset(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{ImpactAggregates: &stubSupplyChainImpactAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/inventory?offset=-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestSupplyChainImpactInventoryGroupExpressionEnumIsClosed(t *testing.T) {
	t.Parallel()

	cases := []SupplyChainImpactInventoryDimension{
		SupplyChainImpactInventoryByImpactStatus,
		SupplyChainImpactInventoryByPriorityBucket,
		SupplyChainImpactInventoryBySeverity,
		SupplyChainImpactInventoryByRepository,
	}
	for _, dim := range cases {
		if _, err := supplyChainImpactInventoryGroupExpression(dim); err != nil {
			t.Fatalf("dimension %q must be supported: %v", dim, err)
		}
	}
	if _, err := supplyChainImpactInventoryGroupExpression("ecosystem"); err == nil {
		t.Fatal("supplyChainImpactInventoryGroupExpression must reject unknown dimensions to keep SQL substitution safe")
	}
}
