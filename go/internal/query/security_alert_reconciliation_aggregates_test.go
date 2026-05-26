package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubSecurityAlertReconciliationAggregateStore struct {
	count         SecurityAlertReconciliationAggregateCount
	countErr      error
	inventory     []SecurityAlertReconciliationInventoryRow
	inventoryErr  error
	lastFilter    SecurityAlertReconciliationAggregateFilter
	lastDimension SecurityAlertReconciliationInventoryDimension
	lastLimit     int
	lastOffset    int
	countCalls    int
	invCalls      int
}

func (s *stubSecurityAlertReconciliationAggregateStore) CountSecurityAlertReconciliations(
	_ context.Context,
	filter SecurityAlertReconciliationAggregateFilter,
) (SecurityAlertReconciliationAggregateCount, error) {
	s.countCalls++
	s.lastFilter = filter
	if s.countErr != nil {
		return SecurityAlertReconciliationAggregateCount{}, s.countErr
	}
	return s.count, nil
}

func (s *stubSecurityAlertReconciliationAggregateStore) SecurityAlertReconciliationInventory(
	_ context.Context,
	filter SecurityAlertReconciliationAggregateFilter,
	dim SecurityAlertReconciliationInventoryDimension,
	limit int,
	offset int,
) ([]SecurityAlertReconciliationInventoryRow, error) {
	s.invCalls++
	s.lastFilter = filter
	s.lastDimension = dim
	s.lastLimit = limit
	s.lastOffset = offset
	if s.inventoryErr != nil {
		return nil, s.inventoryErr
	}
	return append([]SecurityAlertReconciliationInventoryRow(nil), s.inventory...), nil
}

func TestSecurityAlertReconciliationAggregateRoutesReturn503WhenStoreMissing(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/security-alerts/reconciliations/count",
		"/api/v0/supply-chain/security-alerts/reconciliations/inventory",
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

func TestSecurityAlertReconciliationAggregateCountReturnsRollups(t *testing.T) {
	t.Parallel()

	store := &stubSecurityAlertReconciliationAggregateStore{
		count: SecurityAlertReconciliationAggregateCount{
			TotalReconciliations: 12,
			ByReconciliationStatus: map[string]int{
				"eshu_only":          3,
				"provider_only":      2,
				"both_active":        6,
				"both_dismissed":     1,
			},
			ByProvider: map[string]int{
				"github_security_advisories": 8,
				"snyk":                       4,
			},
			ByProviderState: map[string]int{
				"open":      9,
				"fixed":     2,
				"dismissed": 1,
			},
		},
	}
	handler := &SupplyChainHandler{SecurityAlertAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/security-alerts/reconciliations/count?repository_id=repo-A", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.countCalls != 1 {
		t.Fatalf("Count called %d times, want 1", store.countCalls)
	}
	if got, want := store.lastFilter.RepositoryID, "repo-A"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	var body struct {
		TotalReconciliations   int            `json:"total_reconciliations"`
		ByReconciliationStatus map[string]int `json:"by_reconciliation_status"`
		ByProvider             map[string]int `json:"by_provider"`
		ByProviderState        map[string]int `json:"by_provider_state"`
		Scope                  map[string]any `json:"scope"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	if body.TotalReconciliations != 12 {
		t.Fatalf("total_reconciliations = %d, want 12; body = %s", body.TotalReconciliations, w.Body.String())
	}
	if body.ByReconciliationStatus["both_active"] != 6 {
		t.Fatalf("by_reconciliation_status[both_active] = %d, want 6", body.ByReconciliationStatus["both_active"])
	}
	if body.ByProvider["github_security_advisories"] != 8 {
		t.Fatalf("by_provider[github_security_advisories] = %d, want 8", body.ByProvider["github_security_advisories"])
	}
	if body.ByProviderState["open"] != 9 {
		t.Fatalf("by_provider_state[open] = %d, want 9", body.ByProviderState["open"])
	}
	if body.Scope["repository_id"] != "repo-A" {
		t.Fatalf("scope.repository_id = %v, want repo-A", body.Scope["repository_id"])
	}
}

func TestSecurityAlertReconciliationAggregateInventoryReturnsBuckets(t *testing.T) {
	t.Parallel()

	store := &stubSecurityAlertReconciliationAggregateStore{
		inventory: []SecurityAlertReconciliationInventoryRow{
			{Dimension: SecurityAlertReconciliationInventoryByStatus, Value: "eshu_only", Count: 12},
			{Dimension: SecurityAlertReconciliationInventoryByStatus, Value: "provider_only", Count: 3},
			{Dimension: SecurityAlertReconciliationInventoryByStatus, Value: "both_active", Count: 1},
		},
	}
	handler := &SupplyChainHandler{SecurityAlertAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/security-alerts/reconciliations/inventory?group_by=reconciliation_status&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.lastDimension != SecurityAlertReconciliationInventoryByStatus {
		t.Fatalf("dimension = %q, want reconciliation_status", store.lastDimension)
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
	if body.GroupBy != "reconciliation_status" {
		t.Fatalf("group_by = %q, want reconciliation_status", body.GroupBy)
	}
	if body.Truncated {
		t.Fatalf("truncated = true, want false (only 3 buckets, limit 10)")
	}
}

func TestSecurityAlertReconciliationAggregateInventoryReportsTruncated(t *testing.T) {
	t.Parallel()

	rows := make([]SecurityAlertReconciliationInventoryRow, 6)
	for i := range rows {
		rows[i] = SecurityAlertReconciliationInventoryRow{
			Dimension: SecurityAlertReconciliationInventoryByProvider,
			Value:     "provider",
			Count:     i,
		}
	}
	store := &stubSecurityAlertReconciliationAggregateStore{inventory: rows}
	handler := &SupplyChainHandler{SecurityAlertAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/security-alerts/reconciliations/inventory?group_by=provider&limit=5", nil)
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

func TestSecurityAlertReconciliationAggregateInventoryRejectsUnknownDimension(t *testing.T) {
	t.Parallel()

	store := &stubSecurityAlertReconciliationAggregateStore{}
	handler := &SupplyChainHandler{SecurityAlertAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/security-alerts/reconciliations/inventory?group_by=ecosystem", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.invCalls != 0 {
		t.Fatalf("store called for unknown dimension")
	}
}

func TestSecurityAlertReconciliationAggregateInventoryRejectsOversizedLimit(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{SecurityAlertAggregates: &stubSecurityAlertReconciliationAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/security-alerts/reconciliations/inventory?limit=9999", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestSecurityAlertReconciliationAggregateInventoryRejectsNegativeOffset(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{SecurityAlertAggregates: &stubSecurityAlertReconciliationAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/security-alerts/reconciliations/inventory?offset=-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestSecurityAlertReconciliationAggregateInventoryRejectsOversizedOffset(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{SecurityAlertAggregates: &stubSecurityAlertReconciliationAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/security-alerts/reconciliations/inventory?offset=10001", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestSecurityAlertReconciliationAggregateInventoryNullsNextOffsetAtCeiling(t *testing.T) {
	t.Parallel()

	rows := make([]SecurityAlertReconciliationInventoryRow, 6)
	for i := range rows {
		rows[i] = SecurityAlertReconciliationInventoryRow{
			Dimension: SecurityAlertReconciliationInventoryByRepository,
			Value:     "repo",
			Count:     i,
		}
	}
	store := &stubSecurityAlertReconciliationAggregateStore{inventory: rows}
	handler := &SupplyChainHandler{SecurityAlertAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// offset=10000, limit=5 → truncated but offset+limit=10005 > max(10000).
	// next_offset must serialize as JSON null so callers cannot encode an
	// out-of-contract offset on the follow-up request.
	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/security-alerts/reconciliations/inventory?group_by=repository_id&limit=5&offset=10000", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var body struct {
		Truncated  bool `json:"truncated"`
		NextOffset *int `json:"next_offset"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	if !body.Truncated {
		t.Fatalf("truncated = false, want true")
	}
	if body.NextOffset != nil {
		t.Fatalf("next_offset = %d, want null when offset+limit exceeds documented max", *body.NextOffset)
	}
}

func TestNextSecurityAlertReconciliationAggregateOffsetBound(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		offset    int
		limit     int
		truncated bool
		want      any
	}{
		{"not truncated returns nil", 0, 100, false, nil},
		{"normal next offset", 200, 100, true, 300},
		{"exactly at ceiling boundary returns ceiling", 9900, 100, true, 10000},
		{"would exceed ceiling returns nil", 9950, 100, true, nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := nextSecurityAlertReconciliationAggregateOffset(tc.offset, tc.limit, tc.truncated)
			if got != tc.want {
				t.Fatalf("nextSecurityAlertReconciliationAggregateOffset(%d, %d, %v) = %v, want %v",
					tc.offset, tc.limit, tc.truncated, got, tc.want)
			}
		})
	}
}

func TestSecurityAlertReconciliationInventoryGroupExpressionEnumIsClosed(t *testing.T) {
	t.Parallel()

	cases := []SecurityAlertReconciliationInventoryDimension{
		SecurityAlertReconciliationInventoryByStatus,
		SecurityAlertReconciliationInventoryByProvider,
		SecurityAlertReconciliationInventoryByProviderState,
		SecurityAlertReconciliationInventoryByRepository,
		SecurityAlertReconciliationInventoryByPackage,
	}
	for _, dim := range cases {
		if _, err := securityAlertReconciliationInventoryGroupExpression(dim); err != nil {
			t.Fatalf("dimension %q must be supported: %v", dim, err)
		}
	}
	if _, err := securityAlertReconciliationInventoryGroupExpression("ecosystem"); err == nil {
		t.Fatal("securityAlertReconciliationInventoryGroupExpression must reject unknown dimensions to keep SQL substitution safe")
	}
}
