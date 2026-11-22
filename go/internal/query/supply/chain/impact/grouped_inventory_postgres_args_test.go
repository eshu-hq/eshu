// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestGroupedInventoryStoreBindsAggregateArgumentsInOrder(t *testing.T) {
	q := &groupedFakeQueryer{watermark: []time.Time{groupedTestNow.Add(-time.Minute)}}
	q.page = [][]any{groupedRow("service:a", false, 1, 10, `{"affected_exact":1}`, `{"high":1}`, false, 1)}
	filter := AggregateFilter{
		CVEID: "CVE-1", PackageID: "pkg", RepositoryID: "repository:r", SubjectDigest: "sha256:d",
		Status: "affected_exact", AdvisoryID: "GHSA-1", Ecosystem: "npm", ServiceID: "service:a",
		WorkloadID: "workload:w", Environment: "prod", Severity: "high", DetectionProfile: "",
		PriorityBucket: "high", MinPriorityScore: 7, SuppressionState: "active", IncludeSuppressed: true,
		// ImageRef and DetectionProfile stay empty: a non-empty image_ref or the
		// precise profile answers unavailable before any query
		// (TestGroupedInventoryImageRefAnswersUnavailable and
		// TestGroupedInventoryPreciseProfileAnswersUnavailable), so the served
		// call binds the empty $12 and $17.
		AllowedRepositoryIDs: []string{"repository:r"}, AllowedScopeIDs: []string{"scope:s"},
	}
	if _, err := groupedTestStore(q).SupplyChainImpactGroupedInventory(context.Background(), filter, GroupedInventoryByService, 11, 20); err != nil {
		t.Fatalf("SupplyChainImpactGroupedInventory() error = %v", err)
	}
	if len(q.calls) != 2 || q.calls[0].query != SelectWinnersWatermarkQuery {
		t.Fatalf("calls = %d, want the watermark probe then the grouped query", len(q.calls))
	}
	args := q.calls[1].args
	want := []any{
		"CVE-1", "pkg", "repository:r", "sha256:d", "affected_exact", "GHSA-1", "npm", "service:a",
		"workload:w", "prod", "high", "", "high", 7, "active", true, "",
	}
	if len(args) != groupedInventoryArgCount {
		t.Fatalf("args = %d, want %d", len(args), groupedInventoryArgCount)
	}
	for i, value := range want {
		if args[i] != value {
			t.Fatalf("arg $%d = %#v, want %#v", i+1, args[i], value)
		}
	}
	if got, ok := args[19].(time.Time); !ok || !got.Equal(groupedTestNow) {
		t.Fatalf("arg $20 = %#v, want the single UTC read clock %v", args[19], groupedTestNow)
	}
	if args[20] != 11 || args[21] != 20 {
		t.Fatalf("limit/offset args = %#v/%#v, want 11/20", args[20], args[21])
	}
	assertGroupedPlaceholdersBound(t, q.calls[1].query, len(args))
}

func assertGroupedPlaceholdersBound(t *testing.T, query string, argCount int) {
	t.Helper()
	for n := 1; n <= argCount; n++ {
		if !strings.Contains(query, fmt.Sprintf("$%d", n)) {
			t.Fatalf("query never references $%d", n)
		}
	}
	if strings.Contains(query, fmt.Sprintf("$%d", argCount+1)) {
		t.Fatalf("query references $%d beyond the %d bound args", argCount+1, argCount)
	}
}
