// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/lib/pq"
)

type suppressionQueryPlanProof struct {
	ExecutionTime float64 `json:"Execution Time"`
	PlanningTime  float64 `json:"Planning Time"`
	Plan          struct {
		NodeType          string  `json:"Node Type"`
		ActualRows        float64 `json:"Actual Rows"`
		SharedHitBlocks   int64   `json:"Shared Hit Blocks"`
		SharedReadBlocks  int64   `json:"Shared Read Blocks"`
		TempReadBlocks    int64   `json:"Temp Read Blocks"`
		TempWrittenBlocks int64   `json:"Temp Written Blocks"`
	} `json:"Plan"`
}

func explainSuppressionQueryPlan(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	query string,
	args ...any,
) suppressionQueryPlanProof {
	t.Helper()
	var payload []byte
	if err := db.QueryRowContext(
		ctx,
		"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query,
		args...,
	).Scan(&payload); err != nil {
		t.Fatalf("explain suppression query: %v", err)
	}
	var plans []suppressionQueryPlanProof
	if err := json.Unmarshal(payload, &plans); err != nil {
		t.Fatalf("decode suppression query plan: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("suppression query plans = %d, want 1", len(plans))
	}
	return plans[0]
}

func suppressionListPlanArgs(
	filter SupplyChainImpactFindingFilter,
	readAt time.Time,
) []any {
	return []any{
		supplyChainImpactFindingFactKind,
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.ImpactStatus,
		filter.AdvisoryID,
		filter.Ecosystem,
		filter.ServiceID,
		filter.WorkloadID,
		filter.Environment,
		filter.Severity,
		filter.DetectionProfile,
		filter.PriorityBucket,
		filter.MinPriorityScore,
		filter.ImageRef,
		filter.AfterFindingID,
		normalizeSupplyChainImpactSort(filter.Sort),
		filter.Limit,
		filter.SuppressionState,
		filter.IncludeSuppressed,
		pq.Array(filter.AllowedRepositoryIDs),
		pq.Array(filter.AllowedScopeIDs),
		readAt,
	}
}

func suppressionAggregatePlanArgs(
	filter SupplyChainImpactAggregateFilter,
	readAt time.Time,
) []any {
	return []any{
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.ImpactStatus,
		filter.AdvisoryID,
		filter.Ecosystem,
		filter.ServiceID,
		filter.WorkloadID,
		filter.Environment,
		filter.Severity,
		filter.DetectionProfile,
		filter.PriorityBucket,
		filter.MinPriorityScore,
		filter.SuppressionState,
		filter.IncludeSuppressed,
		filter.ImageRef,
		pq.Array(filter.AllowedRepositoryIDs),
		pq.Array(filter.AllowedScopeIDs),
		readAt,
	}
}

func suppressionExplainPlanArgs(readAt time.Time) []any {
	return []any{
		supplyChainImpactFindingFactKind,
		"finding:000501",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		pq.Array([]string(nil)),
		pq.Array([]string(nil)),
		readAt,
	}
}
