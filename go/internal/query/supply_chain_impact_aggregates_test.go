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

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

type stubSupplyChainImpactAggregateStore struct {
	count           impact.SupplyChainImpactAggregateCount
	countErr        error
	inventory       []impact.SupplyChainImpactInventoryRow
	inventoryErr    error
	lastFilter      impact.SupplyChainImpactAggregateFilter
	lastCountFilter impact.SupplyChainImpactAggregateFilter
	lastInvFilter   impact.SupplyChainImpactAggregateFilter
	lastDimension   impact.SupplyChainImpactInventoryDimension
	lastLimit       int
	lastOffset      int
	callCountCount  int
	callInvCount    int
}

func (s *stubSupplyChainImpactAggregateStore) CountSupplyChainImpactFindings(
	_ context.Context,
	filter impact.SupplyChainImpactAggregateFilter,
) (impact.SupplyChainImpactAggregateCount, error) {
	s.callCountCount++
	s.lastFilter = filter
	s.lastCountFilter = filter
	if s.countErr != nil {
		return impact.SupplyChainImpactAggregateCount{}, s.countErr
	}
	return s.count, nil
}

func (s *stubSupplyChainImpactAggregateStore) SupplyChainImpactInventory(
	_ context.Context,
	filter impact.SupplyChainImpactAggregateFilter,
	dim impact.SupplyChainImpactInventoryDimension,
	limit int,
	offset int,
) ([]impact.SupplyChainImpactInventoryRow, error) {
	s.callInvCount++
	s.lastFilter = filter
	s.lastInvFilter = filter
	s.lastDimension = dim
	s.lastLimit = limit
	s.lastOffset = offset
	if s.inventoryErr != nil {
		return nil, s.inventoryErr
	}
	return append([]impact.SupplyChainImpactInventoryRow(nil), s.inventory...), nil
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
		count: impact.SupplyChainImpactAggregateCount{
			TotalFindings:    7,
			AffectedFindings: 4,
			AffectedExact:    3,
			AffectedDerived:  1,
			PossiblyAffected: 0,
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
		AffectedExact    int            `json:"affected_exact"`
		AffectedDerived  int            `json:"affected_derived"`
		PossiblyAffected int            `json:"possibly_affected"`
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
	if body.AffectedExact != 3 {
		t.Fatalf("affected_exact = %d, want 3", body.AffectedExact)
	}
	if body.AffectedDerived != 1 {
		t.Fatalf("affected_derived = %d, want 1", body.AffectedDerived)
	}
	if body.PossiblyAffected != 0 {
		t.Fatalf("possibly_affected = %d, want 0", body.PossiblyAffected)
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

func TestSupplyChainImpactAggregatePriorityQueryQualifiesBucket(t *testing.T) {
	t.Parallel()

	if strings.Contains(impact.SupplyChainImpactAggregatePriorityCountQuery, "NULLIF(priority_bucket") {
		t.Fatalf("priority aggregate query must qualify bucket references:\n%s", impact.SupplyChainImpactAggregatePriorityCountQuery)
	}
	if !strings.Contains(impact.SupplyChainImpactAggregatePriorityCountQuery, "NULLIF(fact.priority_bucket") {
		t.Fatalf("priority aggregate query missing qualified priority bucket reference:\n%s", impact.SupplyChainImpactAggregatePriorityCountQuery)
	}
}

func TestSupplyChainImpactAggregateQueriesCountCanonicalFindings(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"totals":    impact.SupplyChainImpactAggregateCountQuery,
		"priority":  impact.SupplyChainImpactAggregatePriorityCountQuery,
		"severity":  impact.SupplyChainImpactAggregateSeverityCountQuery,
		"inventory": impact.SupplyChainImpactInventoryQueryTemplate,
	} {
		if !strings.Contains(query, "canonical_key") {
			t.Fatalf("%s aggregate query missing canonical_key dedupe:\n%s", name, query)
		}
		if !strings.Contains(query, "canonical_facts") {
			t.Fatalf("%s aggregate query missing canonical_facts CTE:\n%s", name, query)
		}
		if !strings.Contains(query, "has_payload_finding_id") {
			t.Fatalf("%s aggregate query missing payload finding-id row preference:\n%s", name, query)
		}
		if !strings.Contains(query, "source_winners AS") ||
			!strings.Contains(query, "joined_winners AS") ||
			!strings.Contains(query, "expires_at") {
			t.Fatalf("%s aggregate query missing post-rank expiry overlay:\n%s", name, query)
		}
		if !strings.Contains(query, "scope_id = 'operator:vulnerability_suppressions'") ||
			!strings.Contains(query, "<> 'active'") ||
			!strings.Contains(query, "LEFT JOIN operator_overrides AS override") ||
			!strings.Contains(query, "override.canonical_key = source.canonical_key") {
			t.Fatalf("%s aggregate query missing split suppression authority shape:\n%s", name, query)
		}
		if !strings.Contains(query, "priority_score DESC") ||
			!strings.Contains(query, "has_payload_finding_id DESC") ||
			!strings.Contains(query, "fact_id ASC") {
			t.Fatalf("%s aggregate query missing deterministic canonical row ranking:\n%s", name, query)
		}
	}
}

// TestSupplyChainImpactAggregateQueriesKeepActiveScanAnchor pins the bounded
// source/operator scans that #3389 relies on. Each query must keep its single
// fact kind, tombstone predicate, and active-generation join so the canonical
// sort stays index-bounded. If those drift, the whole-table scan returns.
func TestSupplyChainImpactAggregateQueriesKeepActiveScanAnchor(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"totals":    impact.SupplyChainImpactAggregateCountQuery,
		"priority":  impact.SupplyChainImpactAggregatePriorityCountQuery,
		"severity":  impact.SupplyChainImpactAggregateSeverityCountQuery,
		"inventory": impact.SupplyChainImpactInventoryQueryTemplate,
	} {
		for _, want := range []string{
			"WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'",
			"AND fact.is_tombstone = FALSE",
			"scope.active_generation_id = fact.generation_id",
			"AND generation.status = 'active'",
		} {
			if !strings.Contains(query, want) {
				t.Fatalf("%s aggregate query missing #3389 bounded-scan anchor %q:\n%s", name, want, query)
			}
		}
	}
}

func TestSupplyChainImpactAggregateQueriesUseListProfileAndSuppressionPredicates(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"totals":    impact.SupplyChainImpactAggregateCountQuery,
		"priority":  impact.SupplyChainImpactAggregatePriorityCountQuery,
		"severity":  impact.SupplyChainImpactAggregateSeverityCountQuery,
		"inventory": impact.SupplyChainImpactInventoryQueryTemplate,
	} {
		for _, want := range []string{
			"fact.payload->>'detection_profile' = $12",
			"$12 = 'precise'",
			"$12 = 'comprehensive'",
			"nuget_semver_affected_range",
			"cargo_semver_known_fixed",
			"hex_semver_affected_range",
			"swift_semver_affected_range",
			"swift_semver_known_fixed",
			"fact.payload->>'priority_bucket' = $13",
			"COALESCE(NULLIF(fact.payload->>'priority_score', '')::int, 0) >= $14",
			"$15 = ''",
			"fact.payload #>> '{suppression,expires_at}' AS expires_at",
			"'expired'",
			"fact.suppression_state = $15",
			"$16::boolean",
			"NOT IN ('not_affected','accepted_risk','false_positive','ignored')",
			"fact.payload->>'image_ref' = $17",
		} {
			if !strings.Contains(query, want) {
				t.Fatalf("%s aggregate query missing %q:\n%s", name, want, query)
			}
		}
	}
	// Scoped-token grant arrays occupy $18/$19 and the bound suppression
	// evaluation clock occupies $20, so inventory limit/offset use $21/$22.
	if !strings.Contains(impact.SupplyChainImpactInventoryQueryTemplate, "LIMIT $21 OFFSET $22") {
		t.Fatalf("inventory query must keep limit/offset after filter parameters:\n%s", impact.SupplyChainImpactInventoryQueryTemplate)
	}
}

func TestSupplyChainImpactAggregateInventoryReturnsBuckets(t *testing.T) {
	t.Parallel()

	store := &stubSupplyChainImpactAggregateStore{
		inventory: []impact.SupplyChainImpactInventoryRow{
			{Dimension: impact.SupplyChainImpactInventoryByImpactStatus, Value: "affected_exact", Count: 12},
			{Dimension: impact.SupplyChainImpactInventoryByImpactStatus, Value: "affected_derived", Count: 3},
			{Dimension: impact.SupplyChainImpactInventoryByImpactStatus, Value: "not_affected_known_fixed", Count: 1},
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
	if store.lastDimension != impact.SupplyChainImpactInventoryByImpactStatus {
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

	rows := make([]impact.SupplyChainImpactInventoryRow, 6)
	for i := range rows {
		rows[i] = impact.SupplyChainImpactInventoryRow{
			Dimension: impact.SupplyChainImpactInventoryByRepository,
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

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/inventory?group_by=language", nil)
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

func TestSupplyChainImpactAggregateInventoryRejectsOversizedOffset(t *testing.T) {
	t.Parallel()

	handler := &SupplyChainHandler{ImpactAggregates: &stubSupplyChainImpactAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/inventory?offset=10001", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestSupplyChainImpactAggregateInventoryNullsNextOffsetAtOffsetCeiling(t *testing.T) {
	t.Parallel()

	rows := make([]impact.SupplyChainImpactInventoryRow, 6)
	for i := range rows {
		rows[i] = impact.SupplyChainImpactInventoryRow{
			Dimension: impact.SupplyChainImpactInventoryByRepository,
			Value:     "repo",
			Count:     i,
		}
	}
	store := &stubSupplyChainImpactAggregateStore{inventory: rows}
	handler := &SupplyChainHandler{ImpactAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// offset=10000, limit=5 → truncated=true but offset+limit=10005 > max(10000).
	// The handler must serialize next_offset as JSON null instead of a value
	// callers cannot re-request without violating the documented offset bound.
	req := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/inventory?group_by=repository_id&limit=5&offset=10000", nil)
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

func TestNextSupplyChainImpactAggregateOffsetBound(t *testing.T) {
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
		{"already past ceiling returns nil", 10001, 100, true, nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := nextSupplyChainImpactAggregateOffset(tc.offset, tc.limit, tc.truncated)
			if got != tc.want {
				t.Fatalf("nextSupplyChainImpactAggregateOffset(%d, %d, %v) = %v, want %v",
					tc.offset, tc.limit, tc.truncated, got, tc.want)
			}
		})
	}
}
