// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestServiceInvestigationRepositoryCountIsPreCap is the #8802 regression.
//
// coverage_summary.repository_count used to be len(repositories_considered)
// taken after the 50-row cap and after the "__truncated__" marker row was
// appended, so a 120-repository service reported 51 and a cut list and an
// exactly-50 list could not be told apart by the count. The count must be the
// true number of repositories before the cap, never include the marker, and a
// boolean must say the list was cut.
func TestServiceInvestigationRepositoryCountIsPreCap(t *testing.T) {
	t.Parallel()

	// The service repository itself is one row, so total repositories = 1 + extra.
	build := func(extra int) map[string]any {
		dependents := make([]map[string]any, 0, extra)
		for i := 0; i < extra; i++ {
			dependents = append(dependents, map[string]any{
				"repo_id":    fmt.Sprintf("repository:dependent-%03d", i),
				"repository": fmt.Sprintf("dependent-%03d", i),
			})
		}
		packet := BuildServiceInvestigationPacket("orders-api", map[string]any{
			"name":       "orders-api",
			"repo_id":    "repository:orders",
			"repo_name":  "orders-api",
			"dependents": dependents,
		}, InvestigationOptions{})
		return packet
	}

	cases := []struct {
		name         string
		extra        int
		wantCount    int
		wantCut      bool
		wantRowCount int
	}{
		{name: "120 repositories", extra: 119, wantCount: 120, wantCut: true, wantRowCount: serviceStoryItemLimit + 1},
		{name: "51 repositories", extra: 50, wantCount: 51, wantCut: true, wantRowCount: serviceStoryItemLimit + 1},
		{name: "exactly 50 repositories", extra: 49, wantCount: 50, wantCut: false, wantRowCount: serviceStoryItemLimit},
		{name: "one repository", extra: 0, wantCount: 1, wantCut: false, wantRowCount: 1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			packet := build(tc.extra)
			coverage := querycontract.MapValue(packet, "coverage_summary")
			if got := querycontract.IntVal(coverage, "repository_count"); got != tc.wantCount {
				t.Fatalf("coverage_summary[repository_count] = %d, want %d", got, tc.wantCount)
			}
			got, ok := coverage["repositories_truncated"].(bool)
			if !ok {
				t.Fatalf("coverage_summary[repositories_truncated] = %#v, want a bool", coverage["repositories_truncated"])
			}
			if got != tc.wantCut {
				t.Fatalf("coverage_summary[repositories_truncated] = %v, want %v", got, tc.wantCut)
			}
			rows := querycontract.MapSliceValue(packet, "repositories_considered")
			if len(rows) != tc.wantRowCount {
				t.Fatalf("len(repositories_considered) = %d, want %d", len(rows), tc.wantRowCount)
			}
			// repositories_with_evidence never carries the marker row.
			for _, row := range querycontract.MapSliceValue(packet, "repositories_with_evidence") {
				if querycontract.StringVal(row, "repo_id") == serviceInvestigationTruncationMarkerID {
					t.Fatalf("repositories_with_evidence carries the truncation marker row")
				}
			}
			// A cut list still reports the overall truncated flag.
			if tc.wantCut && !querycontract.BoolVal(coverage, "truncated") {
				t.Fatalf("coverage_summary[truncated] = false, want true when the list was cut")
			}
		})
	}
}
