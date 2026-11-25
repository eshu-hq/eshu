// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package costcounting_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestCostBudget_SupplyChainImpact_WritesDoNotGrowWithFindings proves the
// impact writer's write observations are constant per pass at any finding
// count: #10880 PR-C2 removed the fact bulk insert, so no statement chunks by
// finding count anymore (the unnest supports upsert is one Query-side
// statement at any N). The flat 4 across 1 to 2,000 findings proves no
// per-finding Exec-side write; the N+1 companion
// (TestCostBudget_SupplyChainImpact_N1_ExceedsBudget) proves per-call growth
// still exceeds the budget.
func TestCostBudget_SupplyChainImpact_WritesDoNotGrowWithFindings(t *testing.T) {
	t.Parallel()

	budget := loadBudgetFrom(t, supplyChainImpactBudgetRelPath)
	maxWrites := checkedBudgetLimit(t, budget, "eshu_dp_postgres_query_duration_seconds")
	for _, tc := range []struct {
		findings int
		want     uint64
	}{
		{findings: 1, want: maxWrites},
		{findings: 2, want: maxWrites},
		{findings: 10, want: maxWrites},
		{findings: 200, want: maxWrites},
		{findings: 1000, want: maxWrites},
		{findings: 1001, want: maxWrites},
		{findings: 2000, want: maxWrites},
	} {
		t.Run(fmt.Sprintf("%d findings", tc.findings), func(t *testing.T) {
			t.Parallel()
			fake := &postgresExecCountingQueryer{}
			db, reader := newSupplyChainImpactInstrumentedDB(t, fake)
			writer := supplychaincore.PostgresSupplyChainImpactWriter{
				DB:  postgres.SupplyChainImpactBeginner{Beginner: db},
				Now: func() time.Time { return time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC) },
			}
			findings := make([]supplychaincore.SupplyChainImpactFinding, 0, tc.findings)
			for i := 0; i < tc.findings; i++ {
				id := fmt.Sprint(i)
				findings = append(findings, supplychaincore.SupplyChainImpactFinding{
					CVEID:           "CVE-2026-" + id,
					AdvisoryID:      "GHSA-" + id,
					PackageID:       "npm:left-pad@" + id,
					Ecosystem:       "npm",
					PackageName:     "left-pad",
					PURL:            "pkg:npm/left-pad@" + id,
					ObservedVersion: "1.0." + id,
					Status:          supplychaincore.SupplyChainImpactAffectedExact,
					RepositoryID:    "repo:team-api",
					CanonicalWrites: 1,
				})
			}
			if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), supplychaincore.SupplyChainImpactWrite{
				IntentID:     supplyChainImpactCostIntentID,
				ScopeID:      "repo:team-api",
				GenerationID: "generation-supply-chain-impact-cost",
				SourceSystem: "github_dependabot",
				Cause:        "reducer/supply_chain_impact",
				FencingToken: 1,
				Findings:     findings,
			}); err != nil {
				t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
			}
			var rm metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &rm); err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			got := collectAttributedHistogramCount(rm, "eshu_dp_postgres_query_duration_seconds", "operation", "write")
			if uint64(got) != tc.want {
				t.Fatalf("write observations at %d findings = %d, want %d (budget %d, flat per pass since PR-C2)",
					tc.findings, got, tc.want, maxWrites)
			}
		})
	}
}
