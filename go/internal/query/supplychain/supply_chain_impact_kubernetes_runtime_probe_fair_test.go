// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

type fairKubernetesRuntimeGraph struct {
	mu          sync.Mutex
	rows        map[string][]map[string]any
	errorDigest string
	err         error
	calls       []fairKubernetesRuntimeCall
	active      atomic.Int32
	maximum     atomic.Int32
	barrier     chan struct{}
	barrierOnce sync.Once
}

type fairKubernetesRuntimeCall struct {
	Digest string
	Limit  int
}

func (g *fairKubernetesRuntimeGraph) Run(ctx context.Context, _ string, params map[string]any) ([]map[string]any, error) {
	digests, _ := params["subject_digests"].([]string)
	if len(digests) != 1 {
		return nil, fmt.Errorf("subject digest count = %d, want 1", len(digests))
	}
	digest := digests[0]
	limit := querycontract.IntVal(params, "limit")
	g.mu.Lock()
	g.calls = append(g.calls, fairKubernetesRuntimeCall{Digest: digest, Limit: limit})
	g.mu.Unlock()

	active := g.active.Add(1)
	defer g.active.Add(-1)
	for {
		maximum := g.maximum.Load()
		if active <= maximum || g.maximum.CompareAndSwap(maximum, active) {
			break
		}
	}
	if g.barrier != nil {
		if active >= 2 {
			g.barrierOnce.Do(func() { close(g.barrier) })
		}
		select {
		case <-g.barrier:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if digest == g.errorDigest {
		return nil, g.err
	}
	rows := append([]map[string]any(nil), g.rows[digest]...)
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (g *fairKubernetesRuntimeGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func (g *fairKubernetesRuntimeGraph) snapshotCalls() []fairKubernetesRuntimeCall {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]fairKubernetesRuntimeCall(nil), g.calls...)
}

func TestKubernetesRuntimeProbeBalancedQuotas(t *testing.T) {
	t.Parallel()

	for _, digestCount := range []int{1, 3, 199, 200} {
		t.Run(fmt.Sprintf("digests_%d", digestCount), func(t *testing.T) {
			digests := make([]string, digestCount)
			for i := range digests {
				digests[i] = fmt.Sprintf("sha256:%064x", digestCount-i)
			}
			plans := planKubernetesRuntimeProbeQueries(digests, true)
			if len(plans) != digestCount {
				t.Fatalf("plan count = %d, want %d", len(plans), digestCount)
			}
			total := 0
			for i, plan := range plans {
				if plan.Digest == "" || plan.Quota < 1 || plan.QueryLimit != plan.Quota+1 {
					t.Fatalf("plan[%d] = %#v, want digest, quota >= 1, query limit quota+1", i, plan)
				}
				if i > 0 && plans[i-1].Digest >= plan.Digest {
					t.Fatalf("plans not strictly sorted: %#v", plans)
				}
				total += plan.Quota
			}
			if total != SupplyChainKubernetesRuntimeProbeMaxResults {
				t.Fatalf("quota total = %d, want %d", total, SupplyChainKubernetesRuntimeProbeMaxResults)
			}
		})
	}
}

func TestApplyKubernetesRuntimeEvidenceHotDigestCannotStarveColdDigests(t *testing.T) {
	t.Parallel()

	digests := make([]string, SupplyChainKubernetesRuntimeProbeMaxResults)
	rows := make([]impact.SupplyChainImpactFindingRow, len(digests))
	graphRows := make(map[string][]map[string]any, len(digests))
	matches := make([]KubernetesRuntimeWorkloadMatch, 0, len(digests)+1)
	for i := range digests {
		digest := fmt.Sprintf("sha256:%064x", i)
		digests[i] = digest
		rows[i] = impact.SupplyChainImpactFindingRow{FindingID: fmt.Sprintf("finding-%03d", i), SubjectDigest: digest}
		count := 1
		if i == 0 {
			count = SupplyChainKubernetesRuntimeProbeMaxResults + 1
		}
		for j := 0; j < count; j++ {
			uid := fmt.Sprintf("workload-%03d-%03d", i, j)
			graphRows[digest] = append(graphRows[digest], kubernetesRuntimeGraphRow(digest, uid))
			matches = append(matches, KubernetesRuntimeWorkloadMatch{Digest: digest, WorkloadRef: impact.KubernetesRuntimeWorkloadRef{UID: uid}})
		}
	}
	graph := &fairKubernetesRuntimeGraph{rows: graphRows, barrier: make(chan struct{})}
	inventory := &stubKubernetesWorkloadInventory{rows: matches}
	handler := &SupplyChainHandler{Neo4j: graph, KubernetesWorkloadInventory: inventory}

	if err := handler.applySupplyChainKubernetesRuntimeEvidence(context.Background(), querycontract.RepositoryAccessFilter{AllScopes: true}, rows); err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if got := graph.maximum.Load(); got <= 1 || got > SupplyChainKubernetesRuntimeProbeMaxConcurrency {
		t.Fatalf("maximum graph concurrency = %d, want > 1 and <= %d", got, SupplyChainKubernetesRuntimeProbeMaxConcurrency)
	}
	if got := len(graph.snapshotCalls()); got != len(digests) {
		t.Fatalf("graph calls = %d, want %d", got, len(digests))
	}
	for i := range rows {
		if len(rows[i].KubernetesRuntimeWorkloadRefs) != 1 {
			t.Fatalf("row %d refs = %#v, want one fair result", i, rows[i].KubernetesRuntimeWorkloadRefs)
		}
		metadata := rows[i].KubernetesRuntimeProbe
		if metadata == nil || metadata.CandidateLimit != 1 {
			t.Fatalf("row %d metadata = %#v, want candidate_limit=1", i, metadata)
		}
		if i == 0 {
			if metadata.WorkloadRefsTruncated == nil || !*metadata.WorkloadRefsTruncated {
				t.Fatalf("hot row metadata = %#v, want truncated=true", metadata)
			}
		} else if metadata.WorkloadRefsTruncated == nil || *metadata.WorkloadRefsTruncated {
			t.Fatalf("cold row %d metadata = %#v, want truncated=false", i, metadata)
		}
	}
	if len(inventory.candidates) > SupplyChainKubernetesRuntimeProbeMaxAllScopesCandidates {
		t.Fatalf("Postgres candidates = %d, want <= %d", len(inventory.candidates), SupplyChainKubernetesRuntimeProbeMaxAllScopesCandidates)
	}
}

func TestApplyKubernetesRuntimeEvidenceBoundsRepeatedDigestRefsAcrossPage(t *testing.T) {
	t.Parallel()

	const findingCount = 50
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	rows := make([]impact.SupplyChainImpactFindingRow, findingCount)
	for i := range rows {
		rows[i] = impact.SupplyChainImpactFindingRow{FindingID: fmt.Sprintf("finding-%02d", i), SubjectDigest: digest}
	}
	graphRows := make([]map[string]any, SupplyChainKubernetesRuntimeProbeMaxResults+1)
	matches := make([]KubernetesRuntimeWorkloadMatch, len(graphRows))
	for i := range graphRows {
		uid := fmt.Sprintf("workload-%03d", i)
		graphRows[i] = kubernetesRuntimeGraphRow(digest, uid)
		matches[i] = KubernetesRuntimeWorkloadMatch{Digest: digest, WorkloadRef: impact.KubernetesRuntimeWorkloadRef{UID: uid}}
	}
	graph := &fairKubernetesRuntimeGraph{rows: map[string][]map[string]any{digest: graphRows}}
	inventory := &stubKubernetesWorkloadInventory{rows: matches}
	if err := (&SupplyChainHandler{Neo4j: graph, KubernetesWorkloadInventory: inventory}).applySupplyChainKubernetesRuntimeEvidence(
		context.Background(), querycontract.RepositoryAccessFilter{AllScopes: true}, rows,
	); err != nil {
		t.Fatalf("apply error = %v", err)
	}

	totalRefs := 0
	for i, row := range rows {
		totalRefs += len(row.KubernetesRuntimeWorkloadRefs)
		if len(row.KubernetesRuntimeWorkloadRefs) == 0 {
			t.Fatalf("row %d has no runtime ref; every finding must retain non-vacuous evidence", i)
		}
		if row.KubernetesRuntimeProbe == nil || row.KubernetesRuntimeProbe.CandidateLimit != 4 {
			t.Fatalf("row %d metadata = %#v, want repeated-digest candidate_limit=4", i, row.KubernetesRuntimeProbe)
		}
		if row.KubernetesRuntimeProbe.WorkloadRefsTruncated == nil || !*row.KubernetesRuntimeProbe.WorkloadRefsTruncated {
			t.Fatalf("row %d metadata = %#v, want truncated=true", i, row.KubernetesRuntimeProbe)
		}
	}
	if totalRefs > SupplyChainKubernetesRuntimeProbeMaxResults {
		t.Fatalf("serialized page refs = %d, want <= %d", totalRefs, SupplyChainKubernetesRuntimeProbeMaxResults)
	}
	calls := graph.snapshotCalls()
	if len(calls) != 1 || calls[0].Limit != 5 {
		t.Fatalf("graph calls = %#v, want one quota+sentinel read with limit 5", calls)
	}
}

func TestApplyKubernetesRuntimeEvidenceMaxPageRetainsOneRefPerFinding(t *testing.T) {
	t.Parallel()

	digest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	rows := make([]impact.SupplyChainImpactFindingRow, SupplyChainKubernetesRuntimeProbeMaxResults)
	for i := range rows {
		rows[i] = impact.SupplyChainImpactFindingRow{FindingID: fmt.Sprintf("finding-%03d", i), SubjectDigest: digest}
	}
	graphRows := []map[string]any{
		kubernetesRuntimeGraphRow(digest, "workload-000"),
		kubernetesRuntimeGraphRow(digest, "workload-001"),
	}
	matches := []KubernetesRuntimeWorkloadMatch{
		{Digest: digest, WorkloadRef: impact.KubernetesRuntimeWorkloadRef{UID: "workload-000"}},
		{Digest: digest, WorkloadRef: impact.KubernetesRuntimeWorkloadRef{UID: "workload-001"}},
	}
	graph := &fairKubernetesRuntimeGraph{rows: map[string][]map[string]any{digest: graphRows}}
	inventory := &stubKubernetesWorkloadInventory{rows: matches}
	if err := (&SupplyChainHandler{Neo4j: graph, KubernetesWorkloadInventory: inventory}).applySupplyChainKubernetesRuntimeEvidence(
		context.Background(), querycontract.RepositoryAccessFilter{AllScopes: true}, rows,
	); err != nil {
		t.Fatalf("apply error = %v", err)
	}
	for i, row := range rows {
		if len(row.KubernetesRuntimeWorkloadRefs) != 1 {
			t.Fatalf("row %d refs = %#v, want one exact runtime ref", i, row.KubernetesRuntimeWorkloadRefs)
		}
		if row.KubernetesRuntimeProbe == nil || row.KubernetesRuntimeProbe.CandidateLimit != 1 ||
			row.KubernetesRuntimeProbe.WorkloadRefsTruncated == nil || !*row.KubernetesRuntimeProbe.WorkloadRefsTruncated {
			t.Fatalf("row %d metadata = %#v, want candidate_limit=1 truncated=true", i, row.KubernetesRuntimeProbe)
		}
	}
	calls := graph.snapshotCalls()
	if len(calls) != 1 || calls[0].Limit != 2 {
		t.Fatalf("graph calls = %#v, want one quota+sentinel read with limit 2", calls)
	}
}

func TestApplyKubernetesRuntimeEvidenceScopedMetadataDoesNotDiscloseTruncation(t *testing.T) {
	t.Parallel()

	digests := []string{
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}
	rows := make([]impact.SupplyChainImpactFindingRow, len(digests))
	graphRows := make(map[string][]map[string]any, len(digests))
	for i, digest := range digests {
		rows[i] = impact.SupplyChainImpactFindingRow{FindingID: digest, SubjectDigest: digest}
		for j := 0; j < 100; j++ {
			graphRows[digest] = append(graphRows[digest], kubernetesRuntimeGraphRow(digest, fmt.Sprintf("w-%d-%03d", i, j)))
		}
	}
	graph := &fairKubernetesRuntimeGraph{rows: graphRows}
	inventory := &stubKubernetesWorkloadInventory{}
	access := querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repository:allowed"}, AllowedScopeIDs: []string{"scope:allowed"}}
	if err := (&SupplyChainHandler{Neo4j: graph, KubernetesWorkloadInventory: inventory}).applySupplyChainKubernetesRuntimeEvidence(context.Background(), access, rows); err != nil {
		t.Fatalf("apply error = %v", err)
	}
	candidateCount := 0
	for i, row := range rows {
		if row.KubernetesRuntimeProbe == nil || row.KubernetesRuntimeProbe.CandidateLimit < 1 {
			t.Fatalf("row %d metadata = %#v", i, row.KubernetesRuntimeProbe)
		}
		if row.KubernetesRuntimeProbe.WorkloadRefsTruncated != nil {
			t.Fatalf("row %d disclosed scoped truncation = %#v", i, row.KubernetesRuntimeProbe)
		}
		candidateCount += row.KubernetesRuntimeProbe.CandidateLimit
	}
	if candidateCount != SupplyChainKubernetesRuntimeProbeMaxResults || len(inventory.candidates) > SupplyChainKubernetesRuntimeProbeMaxResults {
		t.Fatalf("candidate limits sum=%d Postgres candidates=%d, want sum=200 and candidates<=200", candidateCount, len(inventory.candidates))
	}
	for _, call := range graph.snapshotCalls() {
		plans := planKubernetesRuntimeProbeQueries(digests, false)
		var want int
		for _, plan := range plans {
			if plan.Digest == call.Digest {
				want = plan.Quota
			}
		}
		if call.Limit != want {
			t.Fatalf("scoped call %#v limit = %d, want quota %d", call, call.Limit, want)
		}
	}
}

func TestApplyKubernetesRuntimeEvidenceSingleDigestKeepsAllScopesSentinel(t *testing.T) {
	t.Parallel()

	digest := "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	graphRows := make([]map[string]any, SupplyChainKubernetesRuntimeProbeMaxResults+1)
	matches := make([]KubernetesRuntimeWorkloadMatch, len(graphRows))
	for i := range graphRows {
		uid := fmt.Sprintf("workload-%03d", i)
		graphRows[i] = kubernetesRuntimeGraphRow(digest, uid)
		matches[i] = KubernetesRuntimeWorkloadMatch{Digest: digest, WorkloadRef: impact.KubernetesRuntimeWorkloadRef{UID: uid}}
	}
	graph := &fairKubernetesRuntimeGraph{rows: map[string][]map[string]any{digest: graphRows}}
	inventory := &stubKubernetesWorkloadInventory{rows: matches}
	rows := []impact.SupplyChainImpactFindingRow{{FindingID: "finding", SubjectDigest: digest}}
	if err := (&SupplyChainHandler{Neo4j: graph, KubernetesWorkloadInventory: inventory}).applySupplyChainKubernetesRuntimeEvidence(context.Background(), querycontract.RepositoryAccessFilter{AllScopes: true}, rows); err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if got := len(inventory.candidates); got != SupplyChainKubernetesRuntimeProbeMaxResults+1 {
		t.Fatalf("Postgres candidates = %d, want %d including sentinel", got, SupplyChainKubernetesRuntimeProbeMaxResults+1)
	}
	if got := len(rows[0].KubernetesRuntimeWorkloadRefs); got != SupplyChainKubernetesRuntimeProbeMaxResults {
		t.Fatalf("public refs = %d, want %d", got, SupplyChainKubernetesRuntimeProbeMaxResults)
	}
	metadata := rows[0].KubernetesRuntimeProbe
	if metadata == nil || metadata.CandidateLimit != SupplyChainKubernetesRuntimeProbeMaxResults || metadata.WorkloadRefsTruncated == nil || !*metadata.WorkloadRefsTruncated {
		t.Fatalf("metadata = %#v, want candidate_limit=200 truncated=true", metadata)
	}
}

func TestApplyKubernetesRuntimeEvidenceFirstErrorCancelsWithoutPartialAttachment(t *testing.T) {
	t.Parallel()

	digests := make([]string, 40)
	rows := make([]impact.SupplyChainImpactFindingRow, len(digests))
	graphRows := make(map[string][]map[string]any, len(digests))
	for i := range digests {
		digest := fmt.Sprintf("sha256:%064x", i)
		digests[i] = digest
		rows[i] = impact.SupplyChainImpactFindingRow{FindingID: digest, SubjectDigest: digest}
		graphRows[digest] = []map[string]any{kubernetesRuntimeGraphRow(digest, fmt.Sprintf("w-%03d", i))}
	}
	wantErr := errors.New("graph unavailable")
	graph := &fairKubernetesRuntimeGraph{rows: graphRows, errorDigest: digests[0], err: wantErr}
	inventory := &stubKubernetesWorkloadInventory{}
	err := (&SupplyChainHandler{Neo4j: graph, KubernetesWorkloadInventory: inventory}).applySupplyChainKubernetesRuntimeEvidence(context.Background(), querycontract.RepositoryAccessFilter{AllScopes: true}, rows)
	if !errors.Is(err, wantErr) {
		t.Fatalf("apply error = %v, want %v", err, wantErr)
	}
	if graph.active.Load() != 0 {
		t.Fatalf("active graph workers = %d, want 0 after return", graph.active.Load())
	}
	if inventory.candidates != nil {
		t.Fatalf("inventory called with %#v after graph error", inventory.candidates)
	}
	for i, row := range rows {
		if row.KubernetesRuntimeProbe != nil || len(row.KubernetesRuntimeWorkloadRefs) != 0 {
			t.Fatalf("row %d partially attached probe state: %#v", i, row)
		}
	}
}

func TestApplyKubernetesRuntimeEvidenceCanceledParentAttachesNothing(t *testing.T) {
	t.Parallel()

	digest := "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	graph := &fairKubernetesRuntimeGraph{rows: map[string][]map[string]any{
		digest: {kubernetesRuntimeGraphRow(digest, "workload-1")},
	}}
	inventory := &stubKubernetesWorkloadInventory{}
	rows := []impact.SupplyChainImpactFindingRow{{FindingID: "finding", SubjectDigest: digest}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (&SupplyChainHandler{Neo4j: graph, KubernetesWorkloadInventory: inventory}).applySupplyChainKubernetesRuntimeEvidence(ctx, querycontract.RepositoryAccessFilter{AllScopes: true}, rows)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("apply error = %v, want context.Canceled", err)
	}
	if graph.active.Load() != 0 || inventory.candidates != nil || rows[0].KubernetesRuntimeProbe != nil || len(rows[0].KubernetesRuntimeWorkloadRefs) != 0 {
		t.Fatalf("canceled call leaked work: active=%d candidates=%#v row=%#v", graph.active.Load(), inventory.candidates, rows[0])
	}
}

func TestApplyKubernetesRuntimeEvidenceDeterministicAuthorizedTrim(t *testing.T) {
	t.Parallel()

	digests := []string{
		"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	rows := make([]impact.SupplyChainImpactFindingRow, len(digests))
	graphRows := make(map[string][]map[string]any, len(digests))
	matches := make([]KubernetesRuntimeWorkloadMatch, 0, 204)
	for i, digest := range digests {
		rows[i] = impact.SupplyChainImpactFindingRow{FindingID: digest, SubjectDigest: digest}
		for j := 67; j >= 0; j-- {
			uid := fmt.Sprintf("workload-%03d", j)
			graphRows[digest] = append(graphRows[digest], kubernetesRuntimeGraphRow(digest, uid))
			matches = append(matches, KubernetesRuntimeWorkloadMatch{Digest: digest, WorkloadRef: impact.KubernetesRuntimeWorkloadRef{UID: uid}})
		}
	}
	graph := &fairKubernetesRuntimeGraph{rows: graphRows}
	inventory := &stubKubernetesWorkloadInventory{rows: matches}
	if err := (&SupplyChainHandler{Neo4j: graph, KubernetesWorkloadInventory: inventory}).applySupplyChainKubernetesRuntimeEvidence(context.Background(), querycontract.RepositoryAccessFilter{AllScopes: true}, rows); err != nil {
		t.Fatalf("apply error = %v", err)
	}
	for i, row := range rows {
		quota := row.KubernetesRuntimeProbe.CandidateLimit
		if len(row.KubernetesRuntimeWorkloadRefs) != quota {
			t.Fatalf("row %d refs=%d quota=%d", i, len(row.KubernetesRuntimeWorkloadRefs), quota)
		}
		if !sort.SliceIsSorted(row.KubernetesRuntimeWorkloadRefs, func(i, j int) bool {
			return row.KubernetesRuntimeWorkloadRefs[i].UID < row.KubernetesRuntimeWorkloadRefs[j].UID
		}) {
			t.Fatalf("row %d refs not deterministic: %#v", i, row.KubernetesRuntimeWorkloadRefs)
		}
	}
	got := []int{rows[0].KubernetesRuntimeProbe.CandidateLimit, rows[1].KubernetesRuntimeProbe.CandidateLimit, rows[2].KubernetesRuntimeProbe.CandidateLimit}
	if want := []int{66, 67, 67}; !reflect.DeepEqual(got, want) {
		t.Fatalf("input-order quotas = %#v, want %#v (lexical remainder)", got, want)
	}
}

func kubernetesRuntimeGraphRow(digest, uid string) map[string]any {
	return map[string]any{
		"matched_digest": digest, "workload_uid": uid,
		"edge_scope_id": "scope-edge", "edge_generation_id": "generation-edge",
	}
}
