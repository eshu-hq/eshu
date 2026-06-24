// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

func TestSupplyChainImpactAggregateRoutesUseListProfileDefaults(t *testing.T) {
	t.Parallel()

	content := selectorAggregateContentStore()
	findings := &recordingSupplyChainImpactFindingStore{
		rows: []SupplyChainImpactFindingRow{{
			FindingID:        "finding-precise",
			RepositoryID:     "repo://example/api",
			ImpactStatus:     "affected_exact",
			DetectionProfile: SupplyChainImpactProfilePrecise,
		}},
	}
	aggregates := &stubSupplyChainImpactAggregateStore{
		count: SupplyChainImpactAggregateCount{
			TotalFindings:    1,
			AffectedFindings: 1,
			AffectedExact:    1,
			ByPriorityBucket: map[string]int{"high": 1},
			BySeverity:       map[string]int{"high": 1},
		},
		inventory: []SupplyChainImpactInventoryRow{{
			Dimension: SupplyChainImpactInventoryByImpactStatus,
			Value:     "affected_exact",
			Count:     1,
		}},
	}
	handler := &SupplyChainHandler{
		Content:          content,
		ImpactFindings:   findings,
		ImpactAggregates: aggregates,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	requestPaths := []string{
		"/api/v0/supply-chain/impact/findings?" + url.Values{
			"repository_id": []string{"payments-api"},
			"limit":         []string{"20"},
		}.Encode(),
		"/api/v0/supply-chain/impact/findings/count?" + url.Values{
			"repository_id": []string{"payments-api"},
		}.Encode(),
		"/api/v0/supply-chain/impact/inventory?" + url.Values{
			"repository_id": []string{"payments-api"},
			"group_by":      []string{"impact_status"},
			"limit":         []string{"20"},
		}.Encode(),
	}
	for _, path := range requestPaths {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if got, want := w.Code, http.StatusOK; got != want {
			t.Fatalf("%s status = %d, want %d; body = %s", path, got, want, w.Body.String())
		}
	}

	if got, want := findings.lastFilter.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("list RepositoryID = %q, want %q", got, want)
	}
	if got, want := findings.lastFilter.DetectionProfile, SupplyChainImpactProfilePrecise; got != want {
		t.Fatalf("list DetectionProfile = %q, want %q", got, want)
	}
	for route, filter := range map[string]SupplyChainImpactAggregateFilter{
		"count":     aggregates.lastCountFilter,
		"inventory": aggregates.lastInvFilter,
	} {
		requireAggregateFilterString(t, route, filter, "RepositoryID", "repo://example/api")
		requireAggregateFilterString(t, route, filter, "DetectionProfile", SupplyChainImpactProfilePrecise)
	}
}

func TestSupplyChainImpactAggregateRoutesComprehensiveProfileIncludesPossiblyAffected(t *testing.T) {
	t.Parallel()

	findings := &recordingSupplyChainImpactFindingStore{
		rows: []SupplyChainImpactFindingRow{
			{
				FindingID:        "finding-precise",
				CVEID:            "CVE-2026-9001",
				ImpactStatus:     "affected_exact",
				DetectionProfile: SupplyChainImpactProfilePrecise,
			},
			{
				FindingID:        "finding-comprehensive",
				CVEID:            "CVE-2026-9001",
				ImpactStatus:     "possibly_affected",
				MatchReason:      "range_only_manifest",
				DetectionProfile: SupplyChainImpactProfileComprehensive,
			},
		},
	}
	aggregates := &stubSupplyChainImpactAggregateStore{
		count: SupplyChainImpactAggregateCount{
			TotalFindings:    2,
			AffectedFindings: 2,
			AffectedExact:    1,
			PossiblyAffected: 1,
			ByPriorityBucket: map[string]int{"high": 1, "medium": 1},
			BySeverity:       map[string]int{"high": 1, "medium": 1},
		},
		inventory: []SupplyChainImpactInventoryRow{
			{Dimension: SupplyChainImpactInventoryByImpactStatus, Value: "affected_exact", Count: 1},
			{Dimension: SupplyChainImpactInventoryByImpactStatus, Value: "possibly_affected", Count: 1},
		},
	}
	handler := &SupplyChainHandler{
		ImpactFindings:   findings,
		ImpactAggregates: aggregates,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	listBody := requestImpactBody(t, mux, "/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-9001&limit=20&profile=comprehensive")
	countBody := requestImpactBody(t, mux, "/api/v0/supply-chain/impact/findings/count?cve_id=CVE-2026-9001&profile=comprehensive")
	inventoryBody := requestImpactBody(t, mux, "/api/v0/supply-chain/impact/inventory?cve_id=CVE-2026-9001&group_by=impact_status&limit=20&profile=comprehensive")

	if got, want := findings.lastFilter.DetectionProfile, ""; got != want {
		t.Fatalf("list DetectionProfile = %q, want blank comprehensive filter", got)
	}
	requireAggregateFilterString(t, "count", aggregates.lastCountFilter, "DetectionProfile", "")
	requireAggregateFilterString(t, "inventory", aggregates.lastInvFilter, "DetectionProfile", "")
	if got, want := intFromBody(t, listBody, "count"), 2; got != want {
		t.Fatalf("list count = %d, want %d; body = %#v", got, want, listBody)
	}
	if got, want := intFromBody(t, countBody, "total_findings"), 2; got != want {
		t.Fatalf("count total_findings = %d, want %d; body = %#v", got, want, countBody)
	}
	if got, want := intFromBody(t, countBody, "possibly_affected"), 1; got != want {
		t.Fatalf("count possibly_affected = %d, want %d; body = %#v", got, want, countBody)
	}
	requireInventoryBucket(t, inventoryBody, "possibly_affected", 1)
}

func TestSupplyChainImpactAggregateRoutesCanonicalAndNameSelectorsShareProfileSemantics(t *testing.T) {
	t.Parallel()

	for _, selector := range []string{"repo://example/api", "payments-api"} {
		selector := selector
		t.Run(selector, func(t *testing.T) {
			t.Parallel()

			content := selectorAggregateContentStore()
			aggregates := &stubSupplyChainImpactAggregateStore{
				count: SupplyChainImpactAggregateCount{
					TotalFindings:    1,
					AffectedFindings: 1,
					AffectedExact:    1,
					ByPriorityBucket: map[string]int{"high": 1},
					BySeverity:       map[string]int{"high": 1},
				},
			}
			handler := &SupplyChainHandler{
				Content:          content,
				ImpactAggregates: aggregates,
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			path := "/api/v0/supply-chain/impact/findings/count?" + url.Values{
				"repository_id": []string{selector},
			}.Encode()
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			requireAggregateFilterString(t, "count", aggregates.lastCountFilter, "RepositoryID", "repo://example/api")
			requireAggregateFilterString(t, "count", aggregates.lastCountFilter, "DetectionProfile", SupplyChainImpactProfilePrecise)
		})
	}
}

func TestSupplyChainImpactAggregateRoutesKeepSuppressionSeparateFromProfile(t *testing.T) {
	t.Parallel()

	aggregates := &stubSupplyChainImpactAggregateStore{
		count: SupplyChainImpactAggregateCount{
			TotalFindings:    1,
			AffectedFindings: 1,
			AffectedExact:    1,
			ByPriorityBucket: map[string]int{"high": 1},
			BySeverity:       map[string]int{"high": 1},
		},
		inventory: []SupplyChainImpactInventoryRow{{
			Dimension: SupplyChainImpactInventoryByImpactStatus,
			Value:     "affected_exact",
			Count:     1,
		}},
	}
	handler := &SupplyChainHandler{ImpactAggregates: aggregates}
	mux := http.NewServeMux()
	handler.Mount(mux)

	path := "/api/v0/supply-chain/impact/inventory?" + url.Values{
		"cve_id":             []string{"CVE-2026-9001"},
		"group_by":           []string{"impact_status"},
		"limit":              []string{"20"},
		"profile":            []string{"comprehensive"},
		"suppression_state":  []string{"not_affected"},
		"include_suppressed": []string{"true"},
	}.Encode()
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	requireAggregateFilterString(t, "inventory", aggregates.lastInvFilter, "DetectionProfile", "")
	requireAggregateFilterString(t, "inventory", aggregates.lastInvFilter, "SuppressionState", "not_affected")
	requireAggregateFilterBool(t, "inventory", aggregates.lastInvFilter, "IncludeSuppressed", true)
}

func requestImpactBody(t *testing.T, mux *http.ServeMux, target string) map[string]any {
	t.Helper()

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("%s status = %d, want %d; body = %s", target, got, want, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v; body = %s", target, err, w.Body.String())
	}
	return body
}

func intFromBody(t *testing.T, body map[string]any, key string) int {
	t.Helper()

	value, ok := body[key].(float64)
	if !ok {
		t.Fatalf("body[%q] = %#v, want number", key, body[key])
	}
	return int(value)
}

func requireInventoryBucket(t *testing.T, body map[string]any, value string, want int) {
	t.Helper()

	rawBuckets, ok := body["buckets"].([]any)
	if !ok {
		t.Fatalf("buckets = %#v, want array", body["buckets"])
	}
	for _, raw := range rawBuckets {
		bucket, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("bucket = %#v, want object", raw)
		}
		if bucket["value"] != value {
			continue
		}
		if got := intFromBody(t, bucket, "count"); got != want {
			t.Fatalf("bucket %q count = %d, want %d", value, got, want)
		}
		return
	}
	t.Fatalf("bucket %q not found in %#v", value, rawBuckets)
}

func requireAggregateFilterString(t *testing.T, route string, filter SupplyChainImpactAggregateFilter, field string, want string) {
	t.Helper()

	value := reflect.ValueOf(filter).FieldByName(field)
	if !value.IsValid() {
		t.Fatalf("SupplyChainImpactAggregateFilter missing %s field", field)
	}
	if value.Kind() != reflect.String {
		t.Fatalf("SupplyChainImpactAggregateFilter.%s kind = %s, want string", field, value.Kind())
	}
	if got := value.String(); got != want {
		t.Fatalf("%s SupplyChainImpactAggregateFilter.%s = %q, want %q", route, field, got, want)
	}
}

func requireAggregateFilterBool(t *testing.T, route string, filter SupplyChainImpactAggregateFilter, field string, want bool) {
	t.Helper()

	value := reflect.ValueOf(filter).FieldByName(field)
	if !value.IsValid() {
		t.Fatalf("SupplyChainImpactAggregateFilter missing %s field", field)
	}
	if value.Kind() != reflect.Bool {
		t.Fatalf("SupplyChainImpactAggregateFilter.%s kind = %s, want bool", field, value.Kind())
	}
	if got := value.Bool(); got != want {
		t.Fatalf("%s SupplyChainImpactAggregateFilter.%s = %t, want %t", route, field, got, want)
	}
}
