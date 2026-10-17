// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness-helper live-postgres-readiness
//eshu:readiness-helper:reason #7533 helper-only source with no Test root; exercised by the enrolled TestSupplyChainSuppressionPathsPerformanceLive proof in the preimplementation main run at 271d54e5 (readiness run 38997937360, shard 1 job 117294524860: 24.070s PASS); source dependency pinned in the live-postgres-readiness helper inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
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

// explainSuppressionQueryPlanAndTypes runs one EXPLAIN and returns both
// the top-level buffer proof and the sorted whole-tree node-type multiset,
// so each gated run observes a single plan execution (#7553).
func explainSuppressionQueryPlanAndTypes(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	query string,
	args ...any,
) (suppressionQueryPlanProof, []string) {
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
	var docs []struct {
		Plan suppressionPlanTree `json:"Plan"`
	}
	if err := json.Unmarshal(payload, &docs); err != nil || len(docs) != 1 {
		t.Fatalf("decode suppression query plan: plans=%d err=%v", len(docs), err)
	}
	types := docs[0].Plan.appendTypes(nil)
	sort.Strings(types)
	return plans[0], types
}

// suppressionPlanTree is one EXPLAIN JSON plan node with its children.
type suppressionPlanTree struct {
	NodeType string                `json:"Node Type"`
	Plans    []suppressionPlanTree `json:"Plans"`
}

func (n suppressionPlanTree) appendTypes(into []string) []string {
	into = append(into, n.NodeType)
	for _, child := range n.Plans {
		into = child.appendTypes(into)
	}
	return into
}

func suppressionListPlanArgs(
	filter FindingFilter,
	readAt time.Time,
) []any {
	return []any{
		FindingFactKind,
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.Status,
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
		NormalizeSort(filter.Sort),
		filter.Limit,
		filter.SuppressionState,
		filter.IncludeSuppressed,
		array.Of(filter.AllowedRepositoryIDs),
		array.Of(filter.AllowedScopeIDs),
		readAt,
	}
}

func suppressionAggregatePlanArgs(
	filter AggregateFilter,
	readAt time.Time,
) []any {
	return []any{
		filter.CVEID,
		filter.PackageID,
		filter.RepositoryID,
		filter.SubjectDigest,
		filter.Status,
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
		array.Of(filter.AllowedRepositoryIDs),
		array.Of(filter.AllowedScopeIDs),
		readAt,
	}
}

func suppressionExplainPlanArgs(readAt time.Time) []any {
	return []any{
		FindingFactKind,
		"finding:000501",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		array.Of([]string(nil)),
		array.Of([]string(nil)),
		readAt,
	}
}
