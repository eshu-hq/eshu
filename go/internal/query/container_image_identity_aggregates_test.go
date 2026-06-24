// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stubContainerImageIdentityAggregateStore struct {
	count         ContainerImageIdentityAggregateCount
	countErr      error
	inventory     []ContainerImageIdentityInventoryRow
	inventoryErr  error
	lastFilter    ContainerImageIdentityAggregateFilter
	lastDimension ContainerImageIdentityInventoryDimension
	lastLimit     int
	lastOffset    int
	countCalls    int
	invCalls      int
}

func (s *stubContainerImageIdentityAggregateStore) CountContainerImageIdentities(
	_ context.Context,
	filter ContainerImageIdentityAggregateFilter,
) (ContainerImageIdentityAggregateCount, error) {
	s.countCalls++
	s.lastFilter = filter
	if s.countErr != nil {
		return ContainerImageIdentityAggregateCount{}, s.countErr
	}
	return s.count, nil
}

func (s *stubContainerImageIdentityAggregateStore) ContainerImageIdentityInventory(
	_ context.Context,
	filter ContainerImageIdentityAggregateFilter,
	dim ContainerImageIdentityInventoryDimension,
	limit int,
	offset int,
) ([]ContainerImageIdentityInventoryRow, error) {
	s.invCalls++
	s.lastFilter = filter
	s.lastDimension = dim
	s.lastLimit = limit
	s.lastOffset = offset
	if s.inventoryErr != nil {
		return nil, s.inventoryErr
	}
	return append([]ContainerImageIdentityInventoryRow(nil), s.inventory...), nil
}

func TestContainerImageIdentityAggregateRoutesReturn503WhenStoreMissing(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/supply-chain/container-images/identities/count",
		"/api/v0/supply-chain/container-images/identities/inventory",
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

func TestContainerImageIdentityAggregateCountReturnsRollups(t *testing.T) {
	t.Parallel()

	store := &stubContainerImageIdentityAggregateStore{
		count: ContainerImageIdentityAggregateCount{
			TotalIdentities:    9,
			ByOutcome:          map[string]int{"exact_digest": 7, "tag_resolved": 2},
			ByIdentityStrength: map[string]int{"strong": 6, "weak": 3},
		},
	}
	handler := &SupplyChainHandler{ContainerImageAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/container-images/identities/count?repository_id=oci-registry://registry.example/team/api", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.countCalls != 1 {
		t.Fatalf("Count called %d times, want 1", store.countCalls)
	}
	if got, want := store.lastFilter.RepositoryID, "oci-registry://registry.example/team/api"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	var body struct {
		TotalIdentities    int            `json:"total_identities"`
		ByOutcome          map[string]int `json:"by_outcome"`
		ByIdentityStrength map[string]int `json:"by_identity_strength"`
		Scope              map[string]any `json:"scope"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	if body.TotalIdentities != 9 {
		t.Fatalf("total_identities = %d, want 9; body = %s", body.TotalIdentities, w.Body.String())
	}
	if body.ByOutcome["exact_digest"] != 7 {
		t.Fatalf("by_outcome[exact_digest] = %d, want 7", body.ByOutcome["exact_digest"])
	}
	if body.ByIdentityStrength["strong"] != 6 {
		t.Fatalf("by_identity_strength[strong] = %d, want 6", body.ByIdentityStrength["strong"])
	}
	if body.Scope["repository_id"] != "oci-registry://registry.example/team/api" {
		t.Fatalf("scope.repository_id = %v, want oci-registry://...", body.Scope["repository_id"])
	}
}

func TestContainerImageIdentityAggregateRoutesForwardSourceRepositoryScope(t *testing.T) {
	t.Parallel()

	sourceRepoID := "repo://example/payments-api"
	store := &stubContainerImageIdentityAggregateStore{
		count: ContainerImageIdentityAggregateCount{TotalIdentities: 2},
		inventory: []ContainerImageIdentityInventoryRow{
			{Dimension: ContainerImageIdentityInventoryByRepository, Value: "oci-registry://registry.example/team/payments-api", Count: 2},
		},
	}
	handler := &SupplyChainHandler{ContainerImageAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	countReq := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/container-images/identities/count?source_repository_id="+sourceRepoID,
		nil,
	)
	countW := httptest.NewRecorder()
	mux.ServeHTTP(countW, countReq)
	if got, want := countW.Code, http.StatusOK; got != want {
		t.Fatalf("count status = %d, want %d; body = %s", got, want, countW.Body.String())
	}
	if got, want := store.lastFilter.SourceRepositoryID, sourceRepoID; got != want {
		t.Fatalf("count SourceRepositoryID = %q, want %q", got, want)
	}
	var countBody struct {
		Scope map[string]any `json:"scope"`
	}
	if err := json.Unmarshal(countW.Body.Bytes(), &countBody); err != nil {
		t.Fatalf("decode count body: %v", err)
	}
	if got, want := countBody.Scope["source_repository_id"], sourceRepoID; got != want {
		t.Fatalf("count scope.source_repository_id = %#v, want %#v", got, want)
	}

	inventoryReq := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/container-images/identities/inventory?source_repository_id="+sourceRepoID+"&group_by=repository_id&limit=10",
		nil,
	)
	inventoryW := httptest.NewRecorder()
	mux.ServeHTTP(inventoryW, inventoryReq)
	if got, want := inventoryW.Code, http.StatusOK; got != want {
		t.Fatalf("inventory status = %d, want %d; body = %s", got, want, inventoryW.Body.String())
	}
	if got, want := store.lastFilter.SourceRepositoryID, sourceRepoID; got != want {
		t.Fatalf("inventory SourceRepositoryID = %q, want %q", got, want)
	}
}

func TestContainerImageIdentityAggregateInventoryReturnsBuckets(t *testing.T) {
	t.Parallel()

	store := &stubContainerImageIdentityAggregateStore{
		inventory: []ContainerImageIdentityInventoryRow{
			{Dimension: ContainerImageIdentityInventoryByOutcome, Value: "exact_digest", Count: 12},
			{Dimension: ContainerImageIdentityInventoryByOutcome, Value: "tag_resolved", Count: 3},
		},
	}
	handler := &SupplyChainHandler{ContainerImageAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/container-images/identities/inventory?group_by=outcome&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.lastDimension != ContainerImageIdentityInventoryByOutcome {
		t.Fatalf("dimension = %q, want outcome", store.lastDimension)
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
	if body.Count != 2 {
		t.Fatalf("count = %d, want 2", body.Count)
	}
	if body.GroupBy != "outcome" {
		t.Fatalf("group_by = %q, want outcome", body.GroupBy)
	}
	if body.Truncated {
		t.Fatalf("truncated = true, want false (only 2 buckets, limit 10)")
	}
}

func TestContainerImageIdentityAggregateInventoryReportsTruncated(t *testing.T) {
	t.Parallel()

	rows := make([]ContainerImageIdentityInventoryRow, 6)
	for i := range rows {
		rows[i] = ContainerImageIdentityInventoryRow{
			Dimension: ContainerImageIdentityInventoryByRepository,
			Value:     "repo",
			Count:     i,
		}
	}
	store := &stubContainerImageIdentityAggregateStore{inventory: rows}
	handler := &SupplyChainHandler{ContainerImageAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/container-images/identities/inventory?group_by=repository_id&limit=5", nil)
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

func TestContainerImageIdentityAggregateInventoryRejectsUnknownDimension(t *testing.T) {
	t.Parallel()

	store := &stubContainerImageIdentityAggregateStore{}
	handler := &SupplyChainHandler{ContainerImageAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/container-images/identities/inventory?group_by=registry", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.invCalls != 0 {
		t.Fatalf("store called for unknown dimension")
	}
}

func TestContainerImageIdentityAggregateInventoryRejectsOversizedLimit(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{ContainerImageAggregates: &stubContainerImageIdentityAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/container-images/identities/inventory?limit=9999", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestContainerImageIdentityAggregateInventoryRejectsNegativeOffset(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{ContainerImageAggregates: &stubContainerImageIdentityAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/container-images/identities/inventory?offset=-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestContainerImageIdentityAggregateRejectsUnknownOutcome(t *testing.T) {
	t.Parallel()

	store := &stubContainerImageIdentityAggregateStore{}
	handler := &SupplyChainHandler{ContainerImageAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// `exact-digest` (hyphen) is a typo — the OpenAPI enum and the list
	// endpoint accept only `exact_digest` / `tag_resolved`. Both aggregate
	// endpoints must surface that as a 400, not silently return zero
	// counts, so a caller bug is visible.
	for _, target := range []string{
		"/api/v0/supply-chain/container-images/identities/count?outcome=exact-digest",
		"/api/v0/supply-chain/container-images/identities/inventory?outcome=exact-digest",
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
		t.Fatalf("store called for unknown outcome (countCalls=%d invCalls=%d)",
			store.countCalls, store.invCalls)
	}
}

func TestContainerImageIdentityAggregateInventoryRejectsOversizedOffset(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{ContainerImageAggregates: &stubContainerImageIdentityAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/container-images/identities/inventory?offset=10001", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestContainerImageIdentityAggregateInventoryNullsNextOffsetAtCeiling(t *testing.T) {
	t.Parallel()

	rows := make([]ContainerImageIdentityInventoryRow, 6)
	for i := range rows {
		rows[i] = ContainerImageIdentityInventoryRow{
			Dimension: ContainerImageIdentityInventoryByRepository,
			Value:     "repo",
			Count:     i,
		}
	}
	store := &stubContainerImageIdentityAggregateStore{inventory: rows}
	handler := &SupplyChainHandler{ContainerImageAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/container-images/identities/inventory?group_by=repository_id&limit=5&offset=10000", nil)
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

func TestNextContainerImageIdentityAggregateOffsetBound(t *testing.T) {
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
			got := nextContainerImageIdentityAggregateOffset(tc.offset, tc.limit, tc.truncated)
			if got != tc.want {
				t.Fatalf("nextContainerImageIdentityAggregateOffset(%d, %d, %v) = %v, want %v",
					tc.offset, tc.limit, tc.truncated, got, tc.want)
			}
		})
	}
}

func TestContainerImageIdentityInventoryGroupExpressionEnumIsClosed(t *testing.T) {
	t.Parallel()

	cases := []ContainerImageIdentityInventoryDimension{
		ContainerImageIdentityInventoryByOutcome,
		ContainerImageIdentityInventoryByIdentityStrength,
		ContainerImageIdentityInventoryByRepository,
	}
	for _, dim := range cases {
		if _, err := containerImageIdentityInventoryGroupExpression(dim); err != nil {
			t.Fatalf("dimension %q must be supported: %v", dim, err)
		}
	}
	if _, err := containerImageIdentityInventoryGroupExpression("registry"); err == nil {
		t.Fatal("containerImageIdentityInventoryGroupExpression must reject unknown dimensions to keep SQL substitution safe")
	}
}

func TestContainerImageIdentityAggregateQueriesUseSourceRepositoryAnchor(t *testing.T) {
	t.Parallel()

	for _, query := range []string{
		containerImageIdentityAggregateTotalQuery,
		containerImageIdentityAggregateGroupQueryTemplate,
		containerImageIdentityInventoryQueryTemplate,
	} {
		if !strings.Contains(query, "fact.payload->'source_repository_ids' ? $3") {
			t.Fatalf("aggregate query missing source repository predicate:\n%s", query)
		}
		if !strings.Contains(query, "fact.payload->>'repository_id' = $4") {
			t.Fatalf("aggregate query must keep repository_id as OCI predicate:\n%s", query)
		}
	}
}
