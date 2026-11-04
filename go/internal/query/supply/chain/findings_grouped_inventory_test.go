// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// fakeGroupedInventoryStore is the in-memory GroupedInventoryStore the handler
// tests drive; it records the exact call so a test can prove what the handler
// handed to the store.
type fakeGroupedInventoryStore struct {
	page       impact.GroupedInventoryPage
	err        error
	calls      int
	lastFilter impact.AggregateFilter
	lastDim    impact.GroupedInventoryDimension
	lastLimit  int
	lastOffset int
}

func (s *fakeGroupedInventoryStore) SupplyChainImpactGroupedInventory(
	_ context.Context,
	filter impact.AggregateFilter,
	dimension impact.GroupedInventoryDimension,
	limit int,
	offset int,
) (impact.GroupedInventoryPage, error) {
	s.calls++
	s.lastFilter = filter
	s.lastDim = dimension
	s.lastLimit = limit
	s.lastOffset = offset
	if s.err != nil {
		return impact.GroupedInventoryPage{}, s.err
	}
	return s.page, nil
}

// failingAggregateStore proves the five single-valued dimensions are the only
// ones that read AggregateStore and the grouped ones never do.
type countingAggregateStore struct{ inventoryCalls int }

func (s *countingAggregateStore) CountSupplyChainImpactFindings(
	context.Context, impact.AggregateFilter,
) (impact.AggregateCount, error) {
	return impact.AggregateCount{}, nil
}

func (s *countingAggregateStore) SupplyChainImpactInventory(
	context.Context, impact.AggregateFilter, impact.InventoryDimension, int, int,
) ([]impact.InventoryRow, error) {
	s.inventoryCalls++
	return []impact.InventoryRow{{Dimension: impact.InventoryByStatus, Value: "affected_exact", Count: 1}}, nil
}

type groupedResponse struct {
	Buckets          []map[string]any  `json:"buckets"`
	Count            int               `json:"count"`
	Limit            int               `json:"limit"`
	Offset           int               `json:"offset"`
	GroupBy          string            `json:"group_by"`
	DetectionProfile string            `json:"detection_profile"`
	Truncated        bool              `json:"truncated"`
	NextOffset       any               `json:"next_offset"`
	DistinctFindings *int              `json:"distinct_findings"`
	Freshness        map[string]any    `json:"freshness"`
	Scope            map[string]string `json:"scope"`
}

func serveGrouped(t *testing.T, handler *Handler, target string, ctx context.Context) (*httptest.ResponseRecorder, groupedResponse) {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var body groupedResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v; body = %s", err, rec.Body.String())
		}
	}
	return rec, body
}

func namedRow(dim impact.GroupedInventoryDimension, value string, count int, c impact.GroupCompleteness) impact.GroupedInventoryRow {
	return impact.GroupedInventoryRow{
		Dimension:        dim,
		Value:            value,
		Count:            count,
		ByImpactStatus:   map[string]int{"affected_exact": count},
		BySeverity:       map[string]int{"high": count},
		MaxPriorityScore: 70,
		Completeness:     c,
	}
}

func TestGroupedInventoryMultiServiceFindingCountsInEveryGroup(t *testing.T) {
	t.Parallel()

	// One finding resolves to both svc-a and svc-b, another to svc-a only:
	// two distinct findings, three bucket memberships.
	store := &fakeGroupedInventoryStore{page: impact.GroupedInventoryPage{
		Rows: []impact.GroupedInventoryRow{
			namedRow(impact.GroupedInventoryByService, "svc-a", 2, impact.GroupCompletenessComplete),
			namedRow(impact.GroupedInventoryByService, "svc-b", 1, impact.GroupCompletenessComplete),
		},
		DistinctFindings: 2,
		Freshness:        impact.GroupedInventoryFreshness{State: impact.FreshnessLabelFresh},
	}}
	rec, body := serveGrouped(t, &Handler{ImpactGroupedInventory: store},
		"/api/v0/supply-chain/impact/inventory?group_by=service_id&limit=10", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if store.calls != 1 || store.lastDim != impact.GroupedInventoryByService {
		t.Fatalf("store calls = %d, dimension = %q; want 1, service_id", store.calls, store.lastDim)
	}
	if got, want := store.lastLimit, 11; got != want {
		t.Fatalf("store limit = %d, want %d (limit+1 truncation probe)", got, want)
	}
	if body.DistinctFindings == nil || *body.DistinctFindings != 2 {
		t.Fatalf("distinct_findings = %v, want 2; body = %s", body.DistinctFindings, rec.Body.String())
	}
	sum := 0
	for _, b := range body.Buckets {
		sum += int(b["count"].(float64))
	}
	if sum != 3 || sum < *body.DistinctFindings {
		t.Fatalf("sum of bucket counts = %d, want 3 and >= distinct_findings", sum)
	}
	if body.GroupBy != "service_id" || body.Count != 2 || body.Truncated || body.NextOffset != nil {
		t.Fatalf("envelope = %+v, want group_by service_id, 2 buckets, not truncated", body)
	}
	if body.Freshness["state"] != impact.FreshnessLabelFresh {
		t.Fatalf("freshness = %v, want state fresh", body.Freshness)
	}
	for _, b := range body.Buckets {
		for _, key := range []string{"by_impact_status", "by_severity", "max_priority_score", "completeness", "missing_evidence", "unattributed"} {
			if _, ok := b[key]; !ok {
				t.Errorf("bucket lacks %q: %v", key, b)
			}
		}
		if b["completeness"] != "complete" {
			t.Errorf("bucket completeness = %v, want complete", b["completeness"])
		}
	}
}

func TestGroupedInventoryUnattributedBucketIsNeverComplete(t *testing.T) {
	t.Parallel()

	// A hostile or buggy store labels the unattributed bucket complete, mixes
	// an empty value with unattributed=false, and omits the missing-evidence
	// reason. The handler pins the invariant instead of trusting the store.
	store := &fakeGroupedInventoryStore{page: impact.GroupedInventoryPage{
		Rows: []impact.GroupedInventoryRow{
			namedRow(impact.GroupedInventoryByService, "svc-a", 3, impact.GroupCompletenessComplete),
			{
				Dimension:    impact.GroupedInventoryByService,
				Value:        "",
				Unattributed: false,
				Count:        4,
				Completeness: impact.GroupCompletenessComplete,
			},
		},
		DistinctFindings: 7,
	}}
	rec, body := serveGrouped(t, &Handler{ImpactGroupedInventory: store},
		"/api/v0/supply-chain/impact/inventory?group_by=service_id", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var unattributed map[string]any
	for _, b := range body.Buckets {
		if b["unattributed"] == true {
			unattributed = b
		}
		if b["value"] == "svc-a" && b["unattributed"] == true {
			t.Fatalf("named bucket marked unattributed: %v", b)
		}
	}
	if unattributed == nil {
		t.Fatalf("no unattributed bucket in %s", rec.Body.String())
	}
	if unattributed["value"] != "" {
		t.Errorf("unattributed value = %v, want empty string", unattributed["value"])
	}
	if unattributed["completeness"] != "unattributed" {
		t.Errorf("unattributed completeness = %v, want unattributed (never complete)", unattributed["completeness"])
	}
	reasons, _ := unattributed["missing_evidence"].([]any)
	if len(reasons) == 0 || reasons[0] != "service_mapping" {
		t.Errorf("unattributed missing_evidence = %v, want service_mapping", unattributed["missing_evidence"])
	}
}

func TestGroupedInventoryUnattributedFlagOnNamedValueIsNormalized(t *testing.T) {
	t.Parallel()

	// unattributed=true is authoritative: the value is cleared so the wire
	// never carries a named unattributed bucket.
	store := &fakeGroupedInventoryStore{page: impact.GroupedInventoryPage{
		Rows: []impact.GroupedInventoryRow{{
			Dimension: impact.GroupedInventoryByEnvironment, Value: "prod", Unattributed: true,
			Count: 1, Completeness: impact.GroupCompletenessPartial,
		}},
		DistinctFindings: 1,
	}}
	_, body := serveGrouped(t, &Handler{ImpactGroupedInventory: store},
		"/api/v0/supply-chain/impact/inventory?group_by=environment", nil)
	b := body.Buckets[0]
	if b["value"] != "" || b["completeness"] != "unattributed" {
		t.Fatalf("bucket = %v, want value \"\" and completeness unattributed", b)
	}
	if reasons, _ := b["missing_evidence"].([]any); len(reasons) == 0 || reasons[0] != "current_deploy_evidence" {
		t.Fatalf("missing_evidence = %v, want current_deploy_evidence for environment", b["missing_evidence"])
	}
}

func TestGroupedInventoryTruncatedPageReadsPartial(t *testing.T) {
	t.Parallel()

	rows := []impact.GroupedInventoryRow{
		namedRow(impact.GroupedInventoryByWorkload, "wl-a", 3, impact.GroupCompletenessComplete),
		namedRow(impact.GroupedInventoryByWorkload, "wl-b", 2, impact.GroupCompletenessComplete),
		namedRow(impact.GroupedInventoryByWorkload, "wl-c", 1, impact.GroupCompletenessComplete),
	}
	store := &fakeGroupedInventoryStore{page: impact.GroupedInventoryPage{Rows: rows, DistinctFindings: 6}}
	rec, body := serveGrouped(t, &Handler{ImpactGroupedInventory: store},
		"/api/v0/supply-chain/impact/inventory?group_by=workload_id&limit=2&offset=4", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if store.lastOffset != 4 || store.lastLimit != 3 {
		t.Fatalf("store offset/limit = %d/%d, want 4/3", store.lastOffset, store.lastLimit)
	}
	if !body.Truncated || body.Count != 2 || len(body.Buckets) != 2 {
		t.Fatalf("truncated = %v count = %d buckets = %d; want truncated 2-bucket page", body.Truncated, body.Count, len(body.Buckets))
	}
	if body.NextOffset != float64(6) {
		t.Fatalf("next_offset = %v, want 6", body.NextOffset)
	}
	// distinct_findings is whole-set: the store's value passes through and
	// truncating the page to two buckets (five findings) never lowers it.
	if body.DistinctFindings == nil || *body.DistinctFindings != 6 {
		t.Fatalf("distinct_findings = %v, want the store's whole-set 6 on a truncated page", body.DistinctFindings)
	}
	for _, b := range body.Buckets {
		if b["completeness"] != "partial" {
			t.Errorf("bucket %v completeness = %v, want partial on a truncated page", b["value"], b["completeness"])
		}
	}
}

func TestGroupedInventoryPassesStoreVerdictsThroughAndNormalizesUnknown(t *testing.T) {
	t.Parallel()

	store := &fakeGroupedInventoryStore{page: impact.GroupedInventoryPage{
		Rows: []impact.GroupedInventoryRow{
			namedRow(impact.GroupedInventoryByService, "svc-hidden", 1, impact.GroupCompletenessPartial),
			namedRow(impact.GroupedInventoryByService, "svc-dark", 1, impact.GroupCompletenessReadModelUnavailable),
			namedRow(impact.GroupedInventoryByService, "svc-unknown", 1, "definitely-complete"),
			namedRow(impact.GroupedInventoryByService, "svc-blank", 1, ""),
		},
		DistinctFindings: 4,
	}}
	_, body := serveGrouped(t, &Handler{ImpactGroupedInventory: store},
		"/api/v0/supply-chain/impact/inventory?group_by=service_id", nil)
	want := map[string]string{
		"svc-hidden":  "partial",
		"svc-dark":    "read_model_unavailable",
		"svc-unknown": "partial",
		"svc-blank":   "partial",
	}
	for _, b := range body.Buckets {
		if got := b["completeness"]; got != want[b["value"].(string)] {
			t.Errorf("%v completeness = %v, want %s", b["value"], got, want[b["value"].(string)])
		}
	}
}

// TestGroupedInventoryTrimsOnlySpacesLikeSQLBTRIM pins that the handler trims
// the same characters the store's SQL BTRIM trims (spaces only). A value made
// of a tab is a named bucket in SQL; trimming all Unicode whitespace would
// turn it into a second unattributed bucket.
func TestGroupedInventoryTrimsOnlySpacesLikeSQLBTRIM(t *testing.T) {
	t.Parallel()

	store := &fakeGroupedInventoryStore{page: impact.GroupedInventoryPage{
		Rows: []impact.GroupedInventoryRow{
			{Dimension: impact.GroupedInventoryByService, Value: "\t", Count: 1, Completeness: impact.GroupCompletenessComplete},
			{Dimension: impact.GroupedInventoryByService, Value: " svc ", Count: 1, Completeness: impact.GroupCompletenessComplete},
			{Dimension: impact.GroupedInventoryByService, Value: "", Unattributed: true, Count: 1, Completeness: impact.GroupCompletenessUnattributed},
		},
		DistinctFindings: 3,
	}}
	_, body := serveGrouped(t, &Handler{ImpactGroupedInventory: store},
		"/api/v0/supply-chain/impact/inventory?group_by=service_id", nil)
	unattributed := 0
	for _, b := range body.Buckets {
		if b["unattributed"] == true {
			unattributed++
		}
	}
	if unattributed != 1 {
		t.Fatalf("unattributed buckets = %d, want 1; buckets = %v", unattributed, body.Buckets)
	}
	if body.Buckets[0]["value"] != "\t" || body.Buckets[1]["value"] != "svc" {
		t.Fatalf("values = %q, %q; want the tab kept and spaces trimmed", body.Buckets[0]["value"], body.Buckets[1]["value"])
	}
}

// TestGroupedInventoryFreshnessWindowMatchesFindingsHandler pins one freshness
// window for the winners read model: the grouped store's stale verdict and the
// findings list's stale envelope must agree on the same watermark age.
func TestGroupedInventoryFreshnessWindowMatchesFindingsHandler(t *testing.T) {
	t.Parallel()

	if supplyChainImpactWinnersFreshnessWindow != impact.WinnersFreshnessWindow {
		t.Fatalf("findings window = %s, grouped window = %s, want one value", supplyChainImpactWinnersFreshnessWindow, impact.WinnersFreshnessWindow)
	}
	if impact.WinnersFreshnessWindow != 2*time.Minute {
		t.Fatalf("WinnersFreshnessWindow = %s, want 2m", impact.WinnersFreshnessWindow)
	}
}
