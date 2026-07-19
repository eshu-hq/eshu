// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// stubPackageRegistryAggregateStore lets handler tests assert on the filter +
// dimension that reaches the Reader without standing up NornicDB. The
// production Reader (GraphPackageRegistryAggregateStore) is exercised
// separately via stubGraphQuery to confirm the Cypher and parameter shape.
type stubPackageRegistryAggregateStore struct {
	count         PackageRegistryAggregateCount
	countErr      error
	inventory     []PackageRegistryInventoryRow
	inventoryErr  error
	lastFilter    PackageRegistryAggregateFilter
	lastDimension PackageRegistryInventoryDimension
	lastLimit     int
	lastOffset    int
	countCalls    int
	invCalls      int
}

func (s *stubPackageRegistryAggregateStore) CountPackageRegistryPackages(
	_ context.Context,
	filter PackageRegistryAggregateFilter,
) (PackageRegistryAggregateCount, error) {
	s.countCalls++
	s.lastFilter = filter
	if s.countErr != nil {
		return PackageRegistryAggregateCount{}, s.countErr
	}
	return s.count, nil
}

func (s *stubPackageRegistryAggregateStore) PackageRegistryPackageInventory(
	_ context.Context,
	filter PackageRegistryAggregateFilter,
	dim PackageRegistryInventoryDimension,
	limit int,
	offset int,
) ([]PackageRegistryInventoryRow, error) {
	s.invCalls++
	s.lastFilter = filter
	s.lastDimension = dim
	s.lastLimit = limit
	s.lastOffset = offset
	if s.inventoryErr != nil {
		return nil, s.inventoryErr
	}
	return append([]PackageRegistryInventoryRow(nil), s.inventory...), nil
}

func TestPackageRegistryAggregateRoutesReturn503WhenStoreMissing(t *testing.T) {
	t.Parallel()

	handler := &PackageRegistryHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/package-registry/packages/count",
		"/api/v0/package-registry/packages/inventory",
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

// TestCountPackageRegistryPackagesReturnsRollup carries the
// countPackageRegistryPackages handler identifier so
// scripts/verify-route-coverage.sh's name-search finds this route's test
// coverage (the route predates this rename; only the test name changed).
func TestCountPackageRegistryPackagesReturnsRollup(t *testing.T) {
	t.Parallel()

	store := &stubPackageRegistryAggregateStore{
		count: PackageRegistryAggregateCount{
			TotalPackages: 42,
			ByEcosystem:   map[string]int{"npm": 20, "pypi": 15, "maven": 7},
		},
	}
	handler := &PackageRegistryHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages/count?ecosystem=npm", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.countCalls != 1 {
		t.Fatalf("Count called %d times, want 1", store.countCalls)
	}
	if got, want := store.lastFilter.Ecosystem, "npm"; got != want {
		t.Fatalf("Ecosystem = %q, want %q", got, want)
	}
	var body struct {
		TotalPackages int            `json:"total_packages"`
		ByEcosystem   map[string]int `json:"by_ecosystem"`
		Scope         map[string]any `json:"scope"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	if body.TotalPackages != 42 {
		t.Fatalf("total_packages = %d, want 42; body = %s", body.TotalPackages, w.Body.String())
	}
	if body.ByEcosystem["npm"] != 20 {
		t.Fatalf("by_ecosystem[npm] = %d, want 20", body.ByEcosystem["npm"])
	}
	if body.Scope["ecosystem"] != "npm" {
		t.Fatalf("scope.ecosystem = %v, want npm", body.Scope["ecosystem"])
	}
}

// TestPackageRegistryPackageInventoryReturnsBuckets carries the
// packageRegistryPackageInventory handler identifier so
// scripts/verify-route-coverage.sh's name-search finds this route's test
// coverage (the route predates this rename; only the test name changed).
func TestPackageRegistryPackageInventoryReturnsBuckets(t *testing.T) {
	t.Parallel()

	store := &stubPackageRegistryAggregateStore{
		inventory: []PackageRegistryInventoryRow{
			{Dimension: PackageRegistryInventoryByEcosystem, Value: "npm", Count: 50},
			{Dimension: PackageRegistryInventoryByEcosystem, Value: "pypi", Count: 30},
		},
	}
	handler := &PackageRegistryHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages/inventory?group_by=ecosystem&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.lastDimension != PackageRegistryInventoryByEcosystem {
		t.Fatalf("dimension = %q, want ecosystem", store.lastDimension)
	}
	if store.lastLimit != 11 {
		t.Fatalf("internal limit = %d, want 11 (caller limit + 1)", store.lastLimit)
	}
	var body struct {
		Count     int    `json:"count"`
		GroupBy   string `json:"group_by"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	if body.Count != 2 {
		t.Fatalf("count = %d, want 2", body.Count)
	}
	if body.GroupBy != "ecosystem" {
		t.Fatalf("group_by = %q, want ecosystem", body.GroupBy)
	}
	if body.Truncated {
		t.Fatalf("truncated = true, want false (only 2 buckets, limit 10)")
	}
}

func TestPackageRegistryAggregateInventoryReportsTruncated(t *testing.T) {
	t.Parallel()

	rows := make([]PackageRegistryInventoryRow, 6)
	for i := range rows {
		rows[i] = PackageRegistryInventoryRow{
			Dimension: PackageRegistryInventoryByRegistry,
			Value:     "registry",
			Count:     i,
		}
	}
	store := &stubPackageRegistryAggregateStore{inventory: rows}
	handler := &PackageRegistryHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages/inventory?group_by=registry&limit=5", nil)
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

func TestPackageRegistryAggregateRejectsOutOfContractVisibility(t *testing.T) {
	t.Parallel()

	store := &stubPackageRegistryAggregateStore{}
	handler := &PackageRegistryHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// `internal` and `restricted` are typos; the closed enum is
	// public / private / unknown — the same value space the package-registry
	// collector emits in `parseVisibility`. Both aggregate endpoints must
	// surface out-of-contract values as a 400 to avoid silent zero counts.
	for _, target := range []string{
		"/api/v0/package-registry/packages/count?visibility=internal",
		"/api/v0/package-registry/packages/inventory?visibility=internal",
		"/api/v0/package-registry/packages/count?visibility=restricted",
		"/api/v0/package-registry/packages/inventory?visibility=restricted",
	} {
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
	if store.countCalls != 0 || store.invCalls != 0 {
		t.Fatalf("store called for out-of-contract visibility (countCalls=%d invCalls=%d)",
			store.countCalls, store.invCalls)
	}
}

func TestPackageRegistryAggregateAcceptsContractVisibilityValues(t *testing.T) {
	t.Parallel()

	// `unknown` is a first-class ingestion value (parseVisibility defaults
	// to it for missing or unrecognized inputs), so callers must be able to
	// filter aggregates to that slice. The validator and the OpenAPI /
	// MCP-tool enums must keep all three in scope.
	for _, value := range []string{"public", "private", "unknown"} {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			store := &stubPackageRegistryAggregateStore{}
			handler := &PackageRegistryHandler{Aggregates: store}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet,
				"/api/v0/package-registry/packages/count?visibility="+value, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			if store.lastFilter.Visibility != value {
				t.Fatalf("filter.Visibility = %q, want %q", store.lastFilter.Visibility, value)
			}
		})
	}
}

func TestPackageRegistryAggregateInventoryRejectsUnknownDimension(t *testing.T) {
	t.Parallel()

	store := &stubPackageRegistryAggregateStore{}
	handler := &PackageRegistryHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages/inventory?group_by=normalized_name", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.invCalls != 0 {
		t.Fatalf("store called for unknown dimension")
	}
}

func TestPackageRegistryAggregateInventoryRejectsOversizedLimit(t *testing.T) {
	t.Parallel()

	handler := &PackageRegistryHandler{Aggregates: &stubPackageRegistryAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages/inventory?limit=9999", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestPackageRegistryAggregateInventoryRejectsNegativeOffset(t *testing.T) {
	t.Parallel()

	handler := &PackageRegistryHandler{Aggregates: &stubPackageRegistryAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages/inventory?offset=-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestPackageRegistryAggregateInventoryRejectsOversizedOffset(t *testing.T) {
	t.Parallel()

	handler := &PackageRegistryHandler{Aggregates: &stubPackageRegistryAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages/inventory?offset=10001", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestPackageRegistryAggregateInventoryNullsNextOffsetAtCeiling(t *testing.T) {
	t.Parallel()

	rows := make([]PackageRegistryInventoryRow, 6)
	for i := range rows {
		rows[i] = PackageRegistryInventoryRow{
			Dimension: PackageRegistryInventoryByEcosystem,
			Value:     "npm",
			Count:     i,
		}
	}
	store := &stubPackageRegistryAggregateStore{inventory: rows}
	handler := &PackageRegistryHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages/inventory?group_by=ecosystem&limit=5&offset=10000", nil)
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

func TestNextPackageRegistryAggregateOffsetBound(t *testing.T) {
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
			got := nextPackageRegistryAggregateOffset(tc.offset, tc.limit, tc.truncated)
			if got != tc.want {
				t.Fatalf("nextPackageRegistryAggregateOffset(%d, %d, %v) = %v, want %v",
					tc.offset, tc.limit, tc.truncated, got, tc.want)
			}
		})
	}
}

func TestPackageRegistryInventoryGroupExpressionEnumIsClosed(t *testing.T) {
	t.Parallel()

	cases := []PackageRegistryInventoryDimension{
		PackageRegistryInventoryByEcosystem,
		PackageRegistryInventoryByRegistry,
		PackageRegistryInventoryByNamespace,
		PackageRegistryInventoryByPackageManager,
		PackageRegistryInventoryByVisibility,
	}
	for _, dim := range cases {
		if _, err := packageRegistryInventoryGroupExpression(dim); err != nil {
			t.Fatalf("dimension %q must be supported: %v", dim, err)
		}
	}
	if _, err := packageRegistryInventoryGroupExpression("normalized_name"); err == nil {
		t.Fatal("packageRegistryInventoryGroupExpression must reject unknown dimensions to keep Cypher substitution safe")
	}
}
