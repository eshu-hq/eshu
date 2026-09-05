// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

func assertScopedSuppressionAuthority(
	t *testing.T,
	ctx context.Context,
	direct impact.PostgresSupplyChainImpactFindingStore,
	materialized impact.PostgresSupplyChainImpactFindingStore,
	aggregates impact.PostgresSupplyChainImpactAggregateStore,
) {
	t.Helper()

	for name, store := range map[string]impact.PostgresSupplyChainImpactFindingStore{
		"direct":       direct,
		"materialized": materialized,
	} {
		t.Run(name, func(t *testing.T) {
			defaultRows, err := store.ListSupplyChainImpactFindings(ctx, impact.SupplyChainImpactFindingFilter{
				CVEID:             suppressionAuthorityLiveCVE,
				DetectionProfile:  "comprehensive",
				AllowedScopeIDs:   []string{suppressionAuthorityLiveSource},
				IncludeSuppressed: false,
				Limit:             10,
			})
			if err != nil {
				t.Fatalf("default scoped list: %v", err)
			}
			if len(defaultRows) != 0 {
				t.Fatalf("default scoped list = %#v, want suppressed finding hidden", defaultRows)
			}

			auditRows, err := store.ListSupplyChainImpactFindings(ctx, impact.SupplyChainImpactFindingFilter{
				CVEID:             suppressionAuthorityLiveCVE,
				DetectionProfile:  "comprehensive",
				SuppressionState:  "ignored",
				AllowedScopeIDs:   []string{suppressionAuthorityLiveSource},
				IncludeSuppressed: true,
				Limit:             10,
			})
			if err != nil {
				t.Fatalf("audit scoped list: %v", err)
			}
			if len(auditRows) != 1 ||
				auditRows[0].FindingID != suppressionAuthorityLiveFinding ||
				auditRows[0].ImageRef != suppressionAuthorityOriginalImage ||
				auditRows[0].Suppression == nil ||
				auditRows[0].Suppression.State != "ignored" {
				t.Fatalf("audit scoped list = %#v, want authorized source payload with ignored overlay", auditRows)
			}
		})
	}

	for _, tc := range []struct {
		name              string
		includeSuppressed bool
		suppressionState  string
		wantTotalFindings int
		allowedScopeIDs   []string
	}{
		{
			name:            "default hidden",
			allowedScopeIDs: []string{suppressionAuthorityLiveSource},
		},
		{
			name:              "audit visible",
			includeSuppressed: true,
			suppressionState:  "ignored",
			wantTotalFindings: 1,
			allowedScopeIDs:   []string{suppressionAuthorityLiveSource},
		},
		{
			name:              "unrelated grant cannot observe override",
			includeSuppressed: true,
			suppressionState:  "ignored",
			allowedScopeIDs:   []string{"scope:5465:unrelated"},
		},
	} {
		t.Run("aggregate "+tc.name, func(t *testing.T) {
			count, err := aggregates.CountSupplyChainImpactFindings(ctx, impact.SupplyChainImpactAggregateFilter{
				CVEID:             suppressionAuthorityLiveCVE,
				DetectionProfile:  "comprehensive",
				SuppressionState:  tc.suppressionState,
				AllowedScopeIDs:   tc.allowedScopeIDs,
				IncludeSuppressed: tc.includeSuppressed,
			})
			if err != nil {
				t.Fatalf("scoped aggregate: %v", err)
			}
			if count.TotalFindings != tc.wantTotalFindings {
				t.Fatalf("scoped aggregate total = %d, want %d", count.TotalFindings, tc.wantTotalFindings)
			}
		})
	}

	explanation, err := direct.ExplainSupplyChainImpact(ctx, impact.SupplyChainImpactExplanationFilter{
		FindingID:       suppressionAuthorityLiveFinding,
		AllowedScopeIDs: []string{suppressionAuthorityLiveSource},
	})
	if err != nil {
		t.Fatalf("scoped explain: %v", err)
	}
	if explanation.Finding.Suppression == nil || explanation.Finding.Suppression.State != "ignored" {
		t.Fatalf("scoped explain suppression = %#v, want ignored", explanation.Finding.Suppression)
	}

	if _, err := direct.ExplainSupplyChainImpact(ctx, impact.SupplyChainImpactExplanationFilter{
		FindingID:       suppressionAuthorityLiveFinding,
		AllowedScopeIDs: []string{"scope:5465:unrelated"},
	}); err == nil {
		t.Fatal("unrelated scoped explain error = nil, want finding hidden")
	}
}
