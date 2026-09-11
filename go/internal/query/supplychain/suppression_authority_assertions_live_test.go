// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

func assertSuppressionAuthorityFilter(
	t *testing.T,
	ctx context.Context,
	store impact.PostgresSupplyChainImpactFindingStore,
	aggregates impact.PostgresSupplyChainImpactAggregateStore,
	suppressionState string,
	includeSuppressed bool,
	wantCount int,
) {
	t.Helper()
	rows, err := store.ListSupplyChainImpactFindings(ctx, impact.SupplyChainImpactFindingFilter{
		CVEID:             suppressionAuthorityLiveCVE,
		DetectionProfile:  "comprehensive",
		SuppressionState:  suppressionState,
		IncludeSuppressed: includeSuppressed,
		Limit:             10,
	})
	if err != nil {
		t.Fatalf("list suppression_state=%q: %v", suppressionState, err)
	}
	if len(rows) != wantCount {
		t.Fatalf("list suppression_state=%q count = %d, want %d", suppressionState, len(rows), wantCount)
	}

	count, err := aggregates.CountSupplyChainImpactFindings(ctx, impact.SupplyChainImpactAggregateFilter{
		CVEID:             suppressionAuthorityLiveCVE,
		DetectionProfile:  "comprehensive",
		SuppressionState:  suppressionState,
		IncludeSuppressed: includeSuppressed,
	})
	if err != nil {
		t.Fatalf("count suppression_state=%q: %v", suppressionState, err)
	}
	if count.TotalFindings != wantCount {
		t.Fatalf(
			"aggregate suppression_state=%q count = %d, want %d",
			suppressionState,
			count.TotalFindings,
			wantCount,
		)
	}
}

func assertSuppressionAuthorityState(
	t *testing.T,
	ctx context.Context,
	store impact.PostgresSupplyChainImpactFindingStore,
	aggregates impact.PostgresSupplyChainImpactAggregateStore,
	includeSuppressed bool,
	wantCount int,
	wantState string,
) {
	t.Helper()
	filter := impact.SupplyChainImpactFindingFilter{
		CVEID:             suppressionAuthorityLiveCVE,
		DetectionProfile:  "comprehensive",
		IncludeSuppressed: includeSuppressed,
		Limit:             10,
	}
	rows, err := store.ListSupplyChainImpactFindings(ctx, filter)
	if err != nil {
		t.Fatalf("list include_suppressed=%t: %v", includeSuppressed, err)
	}
	if len(rows) != wantCount {
		t.Fatalf("list include_suppressed=%t count = %d, want %d", includeSuppressed, len(rows), wantCount)
	}
	if wantCount == 1 {
		if got := rows[0].FindingID; got != suppressionAuthorityLiveFinding {
			t.Fatalf("list finding_id = %q, want %q", got, suppressionAuthorityLiveFinding)
		}
		if rows[0].Suppression == nil {
			t.Fatal("list suppression = nil")
		}
		if got := rows[0].Suppression.State; got != wantState {
			t.Fatalf("list suppression state = %q, want %q", got, wantState)
		}
	}

	count, err := aggregates.CountSupplyChainImpactFindings(ctx, impact.SupplyChainImpactAggregateFilter{
		CVEID:             suppressionAuthorityLiveCVE,
		DetectionProfile:  "comprehensive",
		IncludeSuppressed: includeSuppressed,
	})
	if err != nil {
		t.Fatalf("count include_suppressed=%t: %v", includeSuppressed, err)
	}
	if count.TotalFindings != wantCount {
		t.Fatalf("aggregate include_suppressed=%t count = %d, want %d", includeSuppressed, count.TotalFindings, wantCount)
	}
	assertSuppressionAuthorityBucketMap(t, "priority", count.ByPriorityBucket, wantCount)
	assertSuppressionAuthorityBucketMap(t, "severity", count.BySeverity, wantCount)
	for _, dimension := range []impact.SupplyChainImpactInventoryDimension{
		impact.SupplyChainImpactInventoryByPriorityBucket,
		impact.SupplyChainImpactInventoryBySeverity,
	} {
		inventory, err := aggregates.SupplyChainImpactInventory(
			ctx,
			impact.SupplyChainImpactAggregateFilter{
				CVEID:             suppressionAuthorityLiveCVE,
				DetectionProfile:  "comprehensive",
				IncludeSuppressed: includeSuppressed,
			},
			dimension,
			10,
			0,
		)
		if err != nil {
			t.Fatalf("inventory %s include_suppressed=%t: %v", dimension, includeSuppressed, err)
		}
		if len(inventory) != wantCount {
			t.Fatalf(
				"inventory %s include_suppressed=%t rows = %d, want %d",
				dimension,
				includeSuppressed,
				len(inventory),
				wantCount,
			)
		}
		if wantCount == 1 && (inventory[0].Value != "high" || inventory[0].Count != 1) {
			t.Fatalf("inventory %s row = %#v, want high/1", dimension, inventory[0])
		}
	}
}

func assertSuppressionAuthorityBucketMap(
	t *testing.T,
	name string,
	got map[string]int,
	wantCount int,
) {
	t.Helper()
	if wantCount == 0 {
		if len(got) != 0 {
			t.Fatalf("%s buckets = %#v, want empty", name, got)
		}
		return
	}
	if len(got) != 1 || got["high"] != 1 {
		t.Fatalf("%s buckets = %#v, want map[high:1]", name, got)
	}
}

func assertSuppressionAuthorityCursor(
	t *testing.T,
	ctx context.Context,
	store impact.PostgresSupplyChainImpactFindingStore,
) {
	t.Helper()
	first, err := store.ListSupplyChainImpactFindings(ctx, impact.SupplyChainImpactFindingFilter{
		CVEID:            suppressionAuthorityLiveCVE,
		DetectionProfile: "comprehensive",
		Limit:            1,
	})
	if err != nil {
		t.Fatalf("list expired first cursor page: %v", err)
	}
	if len(first) != 1 || first[0].FindingID != suppressionAuthorityLiveFinding {
		t.Fatalf("expired first cursor page = %#v, want retained finding identity", first)
	}
	next, err := store.ListSupplyChainImpactFindings(ctx, impact.SupplyChainImpactFindingFilter{
		CVEID:            suppressionAuthorityLiveCVE,
		DetectionProfile: "comprehensive",
		AfterFindingID:   first[0].FindingID,
		Limit:            1,
	})
	if err != nil {
		t.Fatalf("list expired next cursor page: %v", err)
	}
	if len(next) != 0 {
		t.Fatalf("expired next cursor page = %#v, want empty", next)
	}
}

func assertSuppressionExpiryEdgeCases(
	t *testing.T,
	ctx context.Context,
	store impact.PostgresSupplyChainImpactFindingStore,
) {
	t.Helper()
	for _, tc := range []struct {
		name               string
		cveID              string
		findingID          string
		includeSuppressed  bool
		wantCount          int
		wantEffectiveState string
	}{
		{
			name:              "timeless default hidden",
			cveID:             suppressionAuthorityTimelessCVE,
			findingID:         suppressionAuthorityTimelessFinding,
			wantCount:         0,
			includeSuppressed: false,
		},
		{
			name:               "timeless audit remains ignored",
			cveID:              suppressionAuthorityTimelessCVE,
			findingID:          suppressionAuthorityTimelessFinding,
			wantCount:          1,
			includeSuppressed:  true,
			wantEffectiveState: "ignored",
		},
		{
			name:               "malformed expiry fails visible",
			cveID:              suppressionAuthorityMalformedCVE,
			findingID:          suppressionAuthorityMalformedFinding,
			wantCount:          1,
			includeSuppressed:  false,
			wantEffectiveState: "expired",
		},
	} {
		rows, err := store.ListSupplyChainImpactFindings(ctx, impact.SupplyChainImpactFindingFilter{
			CVEID:             tc.cveID,
			DetectionProfile:  "comprehensive",
			IncludeSuppressed: tc.includeSuppressed,
			Limit:             10,
		})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if len(rows) != tc.wantCount {
			t.Fatalf("%s rows = %d, want %d", tc.name, len(rows), tc.wantCount)
		}
		if tc.wantCount == 1 {
			if rows[0].FindingID != tc.findingID {
				t.Fatalf("%s finding_id = %q, want %q", tc.name, rows[0].FindingID, tc.findingID)
			}
			if rows[0].Suppression == nil || rows[0].Suppression.State != tc.wantEffectiveState {
				t.Fatalf(
					"%s suppression = %#v, want state %q",
					tc.name,
					rows[0].Suppression,
					tc.wantEffectiveState,
				)
			}
		}
	}
}
