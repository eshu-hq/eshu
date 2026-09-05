// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package query

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const (
	kubernetesRuntimeLiveScopeID      = "cluster:kubernetes-runtime-fairness-live"
	kubernetesRuntimeLiveGenerationID = "generation:kubernetes-runtime-fairness-live"
)

// TestKubernetesRuntimeWorkloadGatePreservesDigestFairnessLive binds the
// per-digest graph budget to the production PostgreSQL schema and concrete
// current-inventory gate. The lexical-first hot digest must not consume the
// gate's whole all-scopes candidate allowance before the 199 cold digests.
func TestKubernetesRuntimeWorkloadGatePreservesDigestFairnessLive(t *testing.T) {
	ctx, db := openKubernetesRuntimeFairnessLiveDB(t)
	seedKubernetesRuntimeLiveScope(t, ctx, db)

	digests := make([]string, SupplyChainKubernetesRuntimeProbeMaxResults)
	findings := make([]impact.SupplyChainImpactFindingRow, len(digests))
	graphRows := make(map[string][]map[string]any, len(digests))
	allCandidates := make([]KubernetesRuntimeCandidate, 0, 400)
	for i := range digests {
		digest := fmt.Sprintf("sha256:%064x", i)
		digests[i] = digest
		findings[i] = impact.SupplyChainImpactFindingRow{
			FindingID:     fmt.Sprintf("finding-%03d", i),
			SubjectDigest: digest,
		}
		rowCount := 1
		if i == 0 {
			rowCount = SupplyChainKubernetesRuntimeProbeMaxResults + 1
		}
		for j := range rowCount {
			uid := fmt.Sprintf("fair-%03d-%03d", i, j)
			candidate := kubernetesRuntimeLiveCandidate(uid, digest)
			allCandidates = append(allCandidates, candidate)
			graphRows[digest] = append(graphRows[digest], kubernetesRuntimeLiveGraphRow(candidate))
		}
	}
	if got := len(allCandidates); got != supplychain.SupplyChainKubernetesRuntimeProbeMaxAllScopesCandidates {
		t.Fatalf("seed candidates = %d, want all-scopes bound %d", got, supplychain.SupplyChainKubernetesRuntimeProbeMaxAllScopesCandidates)
	}
	seedKubernetesRuntimeLiveCandidates(t, ctx, db, allCandidates)

	plans := supplychain.PlanKubernetesRuntimeProbeQueries(digests, true)
	plannedCandidates := 0
	for _, plan := range plans {
		plannedCandidates += plan.QueryLimit
	}
	if plannedCandidates != supplychain.SupplyChainKubernetesRuntimeProbeMaxAllScopesCandidates {
		t.Fatalf("planned candidates = %d, want bounded %d", plannedCandidates, supplychain.SupplyChainKubernetesRuntimeProbeMaxAllScopesCandidates)
	}

	handler := &SupplyChainHandler{
		Neo4j:                       &kubernetesRuntimeLiveGraph{rows: graphRows},
		KubernetesWorkloadInventory: NewPostgresKubernetesRuntimeWorkloadStore(db),
	}
	if err := handler.ApplySupplyChainKubernetesRuntimeEvidenceLive(
		ctx,
		querycontract.RepositoryAccessFilter{AllScopes: true},
		findings,
	); err != nil {
		t.Fatalf("apply Kubernetes runtime evidence: %v", err)
	}

	publicRefs := 0
	for i, finding := range findings {
		if got := len(finding.KubernetesRuntimeWorkloadRefs); got != 1 {
			t.Fatalf("digest %d (%s) workload refs = %d, want 1", i, finding.SubjectDigest, got)
		}
		publicRefs++
		metadata := finding.KubernetesRuntimeProbe
		if metadata == nil || metadata.CandidateLimit != 1 || metadata.WorkloadRefsTruncated == nil {
			t.Fatalf("digest %d metadata = %#v, want candidate_limit=1 and known truncation", i, metadata)
		}
		wantTruncated := i == 0
		if *metadata.WorkloadRefsTruncated != wantTruncated {
			t.Fatalf("digest %d truncated = %t, want %t", i, *metadata.WorkloadRefsTruncated, wantTruncated)
		}
	}
	if publicRefs != SupplyChainKubernetesRuntimeProbeMaxResults {
		t.Fatalf("public workload refs = %d, want bounded %d", publicRefs, SupplyChainKubernetesRuntimeProbeMaxResults)
	}
}

// TestKubernetesRuntimeWorkloadGatePreservesSingleDigestSentinelLive proves
// the concrete gate returns the 201st authorized current candidate so the
// handler can trim to 200 and publish workload_refs_truncated=true.
func TestKubernetesRuntimeWorkloadGatePreservesSingleDigestSentinelLive(t *testing.T) {
	ctx, db := openKubernetesRuntimeFairnessLiveDB(t)
	seedKubernetesRuntimeLiveScope(t, ctx, db)

	digest := fmt.Sprintf("sha256:%064x", 999)
	candidates := make([]KubernetesRuntimeCandidate, SupplyChainKubernetesRuntimeProbeMaxResults+1)
	graphRows := make([]map[string]any, len(candidates))
	for i := range candidates {
		candidates[i] = kubernetesRuntimeLiveCandidate(fmt.Sprintf("sentinel-%03d", i), digest)
		graphRows[i] = kubernetesRuntimeLiveGraphRow(candidates[i])
	}
	seedKubernetesRuntimeLiveCandidates(t, ctx, db, candidates)

	findings := []impact.SupplyChainImpactFindingRow{{
		FindingID:     "finding-single-digest-sentinel",
		SubjectDigest: digest,
	}}
	handler := &SupplyChainHandler{
		Neo4j: &kubernetesRuntimeLiveGraph{rows: map[string][]map[string]any{
			digest: graphRows,
		}},
		KubernetesWorkloadInventory: NewPostgresKubernetesRuntimeWorkloadStore(db),
	}
	if err := handler.ApplySupplyChainKubernetesRuntimeEvidenceLive(
		ctx,
		querycontract.RepositoryAccessFilter{AllScopes: true},
		findings,
	); err != nil {
		t.Fatalf("apply Kubernetes runtime evidence: %v", err)
	}

	if got := len(findings[0].KubernetesRuntimeWorkloadRefs); got != SupplyChainKubernetesRuntimeProbeMaxResults {
		t.Fatalf("public workload refs = %d, want %d", got, SupplyChainKubernetesRuntimeProbeMaxResults)
	}
	metadata := findings[0].KubernetesRuntimeProbe
	if metadata == nil || metadata.CandidateLimit != SupplyChainKubernetesRuntimeProbeMaxResults ||
		metadata.WorkloadRefsTruncated == nil || !*metadata.WorkloadRefsTruncated {
		t.Fatalf("metadata = %#v, want candidate_limit=200 and workload_refs_truncated=true", metadata)
	}

	storeRows, err := NewPostgresKubernetesRuntimeWorkloadStore(db).
		CurrentAuthorizedKubernetesRuntimeWorkloads(ctx, candidates, true, nil, nil)
	if err != nil {
		t.Fatalf("read concrete sentinel candidates: %v", err)
	}
	if got := len(storeRows); got != SupplyChainKubernetesRuntimeProbeMaxResults+1 {
		t.Fatalf("concrete gate rows = %d, want 201 including truncation sentinel", got)
	}
}

type kubernetesRuntimeLiveGraph struct {
	rows map[string][]map[string]any
}

func (g *kubernetesRuntimeLiveGraph) Run(
	_ context.Context,
	_ string,
	params map[string]any,
) ([]map[string]any, error) {
	digests, _ := params["subject_digests"].([]string)
	limit, _ := params["limit"].(int)
	if len(digests) != 1 || limit < 1 {
		return nil, fmt.Errorf("expected one digest and positive limit, got digests=%v limit=%d", digests, limit)
	}
	rows := g.rows[digests[0]]
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return append([]map[string]any(nil), rows...), nil
}

func (*kubernetesRuntimeLiveGraph) RunSingle(
	context.Context,
	string,
	map[string]any,
) (map[string]any, error) {
	return nil, nil
}

func openKubernetesRuntimeFairnessLiveDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_KUBERNETES_RUNTIME_PROBE_POSTGRES_DSN"),
		os.Getenv("ESHU_KUBERNETES_RUNTIME_PROBE_POSTGRES_DISPOSABLE"),
		5*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply production bootstrap schema: %v", err)
	}
	return ctx, db
}

func seedKubernetesRuntimeLiveScope(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin Kubernetes runtime live scope seed: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	statements := []struct {
		query string
		args  []any
	}{{query: `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'cluster', 'kubernetes', $1, 'kubernetes', $1,
          clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb)
`, args: []any{kubernetesRuntimeLiveScopeID}}, {query: `
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload
) VALUES ($2, $1, 'test', clock_timestamp(), clock_timestamp(), 'active', clock_timestamp(), '{}'::jsonb);

`, args: []any{kubernetesRuntimeLiveScopeID, kubernetesRuntimeLiveGenerationID}}, {
		query: `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`,
		args:  []any{kubernetesRuntimeLiveScopeID, kubernetesRuntimeLiveGenerationID},
	}}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed Kubernetes runtime live scope: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit Kubernetes runtime live scope: %v", err)
	}
}

func seedKubernetesRuntimeLiveCandidates(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	candidates []KubernetesRuntimeCandidate,
) {
	t.Helper()
	uids := make([]string, len(candidates))
	factIDs := make([]string, len(candidates))
	for i, candidate := range candidates {
		uids[i] = candidate.WorkloadUID
		factIDs[i] = "fact:" + candidate.WorkloadUID
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin Kubernetes runtime live candidate seed: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  collector_kind, source_confidence, source_system, source_fact_key,
  observed_at, ingested_at, is_tombstone, payload
)
SELECT fact_id, $3, $4, 'kubernetes_workload', uid,
       'kubernetes', 'observed', 'kubernetes', uid,
       clock_timestamp(), clock_timestamp(), FALSE, '{}'::jsonb
FROM UNNEST($1::text[], $2::text[]) AS input(uid, fact_id)
`, uids, factIDs, kubernetesRuntimeLiveScopeID, kubernetesRuntimeLiveGenerationID); err != nil {
		t.Fatalf("seed Kubernetes runtime live facts: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO graph_node_owner (uid, source_order_key, winning_row, updated_at)
SELECT uid,
       '2026-08-10T00:00:00Z|' || fact_id,
       jsonb_build_object(
         'source_fact_id', fact_id,
         'cluster_id', 'cluster-live',
         'namespace', 'payments',
         'name', uid
       ),
       clock_timestamp()
FROM UNNEST($1::text[], $2::text[]) AS input(uid, fact_id)
`, uids, factIDs); err != nil {
		t.Fatalf("seed Kubernetes runtime live owners: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit Kubernetes runtime live candidates: %v", err)
	}
}

func kubernetesRuntimeLiveCandidate(uid, digest string) KubernetesRuntimeCandidate {
	return KubernetesRuntimeCandidate{
		WorkloadUID:      uid,
		Digest:           digest,
		EdgeScopeID:      kubernetesRuntimeLiveScopeID,
		EdgeGenerationID: kubernetesRuntimeLiveGenerationID,
	}
}

func kubernetesRuntimeLiveGraphRow(candidate KubernetesRuntimeCandidate) map[string]any {
	return map[string]any{
		"workload_uid":       candidate.WorkloadUID,
		"matched_digest":     candidate.Digest,
		"edge_scope_id":      candidate.EdgeScopeID,
		"edge_generation_id": candidate.EdgeGenerationID,
	}
}
