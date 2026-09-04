// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

func assertSuppressionAuthorityCloneDriftAndOrphan(
	t *testing.T,
	ctx context.Context,
	direct impact.PostgresSupplyChainImpactFindingStore,
	materialized impact.PostgresSupplyChainImpactFindingStore,
) {
	t.Helper()

	for name, store := range map[string]impact.PostgresSupplyChainImpactFindingStore{
		"direct":       direct,
		"materialized": materialized,
	} {
		t.Run(name+" clone drift", func(t *testing.T) {
			rows, err := store.ListSupplyChainImpactFindings(ctx, impact.SupplyChainImpactFindingFilter{
				CVEID:             suppressionAuthorityLiveCVE,
				Severity:          "high",
				DetectionProfile:  "comprehensive",
				IncludeSuppressed: true,
				Limit:             10,
			})
			if err != nil {
				t.Fatalf("list source-filtered drifted override: %v", err)
			}
			if len(rows) != 1 ||
				rows[0].FindingID != suppressionAuthorityLiveFinding ||
				rows[0].CVSSScore != 8.0 ||
				rows[0].ImageRef != suppressionAuthoritySecondImage ||
				rows[0].Suppression == nil ||
				rows[0].Suppression.State != "ignored" {
				t.Fatalf("source-filtered drifted override = %#v, want source severity with ignored overlay", rows)
			}
		})

		t.Run(name+" operator orphan", func(t *testing.T) {
			rows, err := store.ListSupplyChainImpactFindings(ctx, impact.SupplyChainImpactFindingFilter{
				CVEID:             suppressionAuthorityOrphanCVE,
				DetectionProfile:  "comprehensive",
				IncludeSuppressed: true,
				Limit:             10,
			})
			if err != nil {
				t.Fatalf("list operator-only canonical key: %v", err)
			}
			if len(rows) != 0 {
				t.Fatalf("operator-only canonical rows = %#v, want none", rows)
			}
		})
	}
}
