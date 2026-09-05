// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
	"github.com/eshu-hq/eshu/go/internal/truth"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type stubKubernetesRuntimeGraph struct {
	mu       sync.Mutex
	rows     []map[string]any
	err      error
	cypher   string
	params   map[string]any
	runCalls int
	calls    []fairKubernetesRuntimeCall
}

func (s *stubKubernetesRuntimeGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runCalls++
	s.cypher = cypher
	s.params = params
	digests, _ := params["subject_digests"].([]string)
	if len(digests) == 1 {
		s.calls = append(s.calls, fairKubernetesRuntimeCall{Digest: digests[0], Limit: IntVal(params, "limit")})
	}
	if s.err != nil || len(digests) != 1 {
		return nil, s.err
	}
	filtered := make([]map[string]any, 0, len(s.rows))
	for _, row := range s.rows {
		if StringVal(row, "matched_digest") == digests[0] {
			filtered = append(filtered, row)
		}
	}
	if limit := IntVal(params, "limit"); len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func (s *stubKubernetesRuntimeGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func (s *stubKubernetesRuntimeGraph) snapshot() (int, []fairKubernetesRuntimeCall) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runCalls, append([]fairKubernetesRuntimeCall(nil), s.calls...)
}

type stubKubernetesWorkloadInventory struct {
	rows         []KubernetesRuntimeWorkloadMatch
	err          error
	candidates   []KubernetesRuntimeCandidate
	allScopes    bool
	repositories []string
	scopes       []string
}

func (s *stubKubernetesWorkloadInventory) CurrentAuthorizedKubernetesRuntimeWorkloads(
	_ context.Context,
	candidates []KubernetesRuntimeCandidate,
	allScopes bool,
	repositories []string,
	scopes []string,
) ([]KubernetesRuntimeWorkloadMatch, error) {
	s.candidates = append([]KubernetesRuntimeCandidate(nil), candidates...)
	s.allScopes = allScopes
	s.repositories = append([]string(nil), repositories...)
	s.scopes = append([]string(nil), scopes...)
	return append([]KubernetesRuntimeWorkloadMatch(nil), s.rows...), s.err
}

func TestKubernetesRuntimeProbeCypherIsPortableBoundedThreeArmQuery(t *testing.T) {
	t.Parallel()

	cypher := supplyChainKubernetesRuntimeProbeCypher
	if got := strings.Count(cypher, "UNWIND $subject_digests AS candidate_digest"); got != 3 {
		t.Fatalf("UNWIND count = %d, want 3; cypher=%s", got, cypher)
	}
	for _, label := range []string{"ContainerImage", "ContainerImageIndex", "ContainerImageDescriptor"} {
		if !strings.Contains(cypher, "(img:"+label+" {digest: candidate_digest})<-[rel:RUNS_IMAGE]-(w:KubernetesWorkload)") {
			t.Fatalf("cypher missing connected %s digest anchor: %s", label, cypher)
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(cypher), "CALL {") || strings.Count(cypher, "\n  UNION\n") != 2 {
		t.Fatalf("cypher must be one CALL-wrapped three-arm UNION: %s", cypher)
	}
	for _, want := range []string{
		"rel.evidence_source = $evidence_source",
		"rel.resolution_mode = $resolution_mode",
		"rel.source_digest = candidate_digest",
		"ORDER BY matched_digest, workload_uid, edge_scope_id, edge_generation_id",
		"LIMIT $limit",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("cypher missing %q: %s", want, cypher)
		}
	}
	if strings.Contains(cypher, ":ContainerImage|") {
		t.Fatalf("cypher regressed to NornicDB-broken node-label disjunction: %s", cypher)
	}
}

func TestApplySupplyChainKubernetesRuntimeEvidencePromotesExactDigest(t *testing.T) {
	t.Parallel()

	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	graph := &stubKubernetesRuntimeGraph{rows: []map[string]any{{
		"matched_digest": digest, "workload_uid": "kw-1", "edge_scope_id": "scope-edge", "edge_generation_id": "gen-edge",
	}}}
	inventory := &stubKubernetesWorkloadInventory{rows: []KubernetesRuntimeWorkloadMatch{{
		Digest: digest, WorkloadRef: impact.KubernetesRuntimeWorkloadRef{UID: "kw-1", ClusterID: "cluster-a", Namespace: "payments", Name: "api"},
	}}}
	handler := &SupplyChainHandler{Neo4j: graph, KubernetesWorkloadInventory: inventory}
	rows := []impact.SupplyChainImpactFindingRow{
		{FindingID: "running", SubjectDigest: digest},
		{FindingID: "other", SubjectDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
	}

	if err := handler.applySupplyChainKubernetesRuntimeEvidence(context.Background(), repositoryAccessFilter{AllScopes: true}, rows); err != nil {
		t.Fatalf("applySupplyChainKubernetesRuntimeEvidence() error = %v", err)
	}
	if runCalls, _ := graph.snapshot(); runCalls != 2 {
		t.Fatalf("graph Run calls = %d, want one per exact digest", runCalls)
	}
	if got := rows[0].KubernetesRuntimeWorkloadRefs; len(got) != 1 || got[0].UID != "kw-1" || got[0].Namespace != "payments" {
		t.Fatalf("runtime workload refs = %#v, want exact authorized kw-1", got)
	}
	if got := impact.BuildSupplyChainImpactFindingResult(&rows[0]).DeploymentTruthTier; got != string(truth.TierRuntimeConfirmed) {
		t.Fatalf("deployment truth tier = %q, want %q", got, truth.TierRuntimeConfirmed)
	}
	if len(rows[1].KubernetesRuntimeWorkloadRefs) != 0 {
		t.Fatalf("other digest refs = %#v, want empty", rows[1].KubernetesRuntimeWorkloadRefs)
	}
	if got := inventory.candidates; len(got) != 1 || got[0].EdgeScopeID != "scope-edge" || got[0].EdgeGenerationID != "gen-edge" {
		t.Fatalf("ledger candidates = %#v, want graph edge scope/generation preserved", got)
	}
}

func TestApplySupplyChainKubernetesRuntimeEvidenceExcludesDeniedOwnerOrEdge(t *testing.T) {
	t.Parallel()

	digest := "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	graph := &stubKubernetesRuntimeGraph{rows: []map[string]any{{
		"matched_digest": digest, "workload_uid": "kw-denied", "edge_scope_id": "scope-denied", "edge_generation_id": "gen-denied",
	}}}
	inventory := &stubKubernetesWorkloadInventory{}
	handler := &SupplyChainHandler{Neo4j: graph, KubernetesWorkloadInventory: inventory}
	rows := []impact.SupplyChainImpactFindingRow{{FindingID: "f", SubjectDigest: digest, EvidencePath: []string{cicdRunCorrelationFactKind}}}

	access := repositoryAccessFilter{AllowedRepositoryIDs: []string{"repository:r_allowed"}, AllowedScopeIDs: []string{"scope-allowed"}}
	if err := handler.applySupplyChainKubernetesRuntimeEvidence(context.Background(), access, rows); err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if len(rows[0].KubernetesRuntimeWorkloadRefs) != 0 {
		t.Fatalf("refs = %#v, want none when owner or edge gate rejects candidate", rows[0].KubernetesRuntimeWorkloadRefs)
	}
	if inventory.allScopes {
		t.Fatal("inventory allScopes = true for scoped caller")
	}
	if got := impact.BuildSupplyChainImpactFindingResult(&rows[0]).DeploymentTruthTier; got != string(truth.TierProvenanceCIDeclared) {
		t.Fatalf("tier = %q, want %q without authorized runtime evidence", got, truth.TierProvenanceCIDeclared)
	}
}

func TestApplySupplyChainKubernetesRuntimeEvidenceBoundsAndDeduplicatesDigests(t *testing.T) {
	t.Parallel()

	digests := make([]string, 0, supplyChainCloudRuntimeProbeMaxDigests+2)
	rows := make([]impact.SupplyChainImpactFindingRow, 0, supplyChainCloudRuntimeProbeMaxDigests+2)
	for i := supplyChainCloudRuntimeProbeMaxDigests + 1; i >= 0; i-- {
		digest := fmt.Sprintf("sha256:%064x", i+1)
		digests = append(digests, digest)
		rows = append(rows, impact.SupplyChainImpactFindingRow{FindingID: digest, SubjectDigest: digest})
	}
	rows = append(rows, impact.SupplyChainImpactFindingRow{FindingID: "duplicate", SubjectDigest: digests[0]})
	graphRows := make([]map[string]any, supplyChainKubernetesRuntimeProbeMaxResults+2)
	for i := range graphRows {
		graphRows[i] = map[string]any{
			"matched_digest": digests[0], "workload_uid": fmt.Sprintf("workload-%03d", i),
			"edge_scope_id": "scope-edge", "edge_generation_id": "generation-edge",
		}
	}
	graph := &stubKubernetesRuntimeGraph{rows: graphRows}
	inventory := &stubKubernetesWorkloadInventory{}
	handler := &SupplyChainHandler{Neo4j: graph, KubernetesWorkloadInventory: inventory}

	if err := handler.applySupplyChainKubernetesRuntimeEvidence(context.Background(), repositoryAccessFilter{AllScopes: true}, rows); err != nil {
		t.Fatalf("apply error = %v", err)
	}
	runCalls, calls := graph.snapshot()
	if runCalls != supplyChainCloudRuntimeProbeMaxDigests {
		t.Fatalf("graph calls = %d, want %d", runCalls, supplyChainCloudRuntimeProbeMaxDigests)
	}
	queryLimitTotal := 0
	for _, call := range calls {
		if call.Digest == "" || call.Limit < 1 {
			t.Fatalf("graph call = %#v, want one digest and positive limit", call)
		}
		queryLimitTotal += call.Limit
	}
	if queryLimitTotal > supplyChainKubernetesRuntimeProbeMaxAllScopesCandidates {
		t.Fatalf("graph query limits total = %d, want <= %d", queryLimitTotal, supplyChainKubernetesRuntimeProbeMaxAllScopesCandidates)
	}
	if len(inventory.candidates) > supplyChainKubernetesRuntimeProbeMaxAllScopesCandidates {
		t.Fatalf("inventory candidate count = %d, want <= %d", len(inventory.candidates), supplyChainKubernetesRuntimeProbeMaxAllScopesCandidates)
	}
}

func TestApplySupplyChainKubernetesRuntimeEvidencePropagatesErrorsAndEmptyIsNoOp(t *testing.T) {
	t.Parallel()

	digest := "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	rows := []impact.SupplyChainImpactFindingRow{{FindingID: "f", SubjectDigest: digest}}
	graphErr := errors.New("graph unavailable")
	if err := (&SupplyChainHandler{Neo4j: &stubKubernetesRuntimeGraph{err: graphErr}, KubernetesWorkloadInventory: &stubKubernetesWorkloadInventory{}}).
		applySupplyChainKubernetesRuntimeEvidence(context.Background(), repositoryAccessFilter{AllScopes: true}, rows); !errors.Is(err, graphErr) {
		t.Fatalf("graph error = %v, want %v", err, graphErr)
	}
	inventoryErr := errors.New("owner ledger unavailable")
	graphWithCandidate := &stubKubernetesRuntimeGraph{rows: []map[string]any{{
		"matched_digest": digest, "workload_uid": "kw-1", "edge_scope_id": "scope-1", "edge_generation_id": "gen-1",
	}}}
	if err := (&SupplyChainHandler{Neo4j: graphWithCandidate, KubernetesWorkloadInventory: &stubKubernetesWorkloadInventory{err: inventoryErr}}).
		applySupplyChainKubernetesRuntimeEvidence(context.Background(), repositoryAccessFilter{AllScopes: true}, rows); !errors.Is(err, inventoryErr) {
		t.Fatalf("inventory error = %v, want %v", err, inventoryErr)
	}

	graph := &stubKubernetesRuntimeGraph{}
	if err := (&SupplyChainHandler{Neo4j: graph, KubernetesWorkloadInventory: &stubKubernetesWorkloadInventory{}}).
		applySupplyChainKubernetesRuntimeEvidence(context.Background(), repositoryAccessFilter{AllScopes: true}, nil); err != nil {
		t.Fatalf("empty rows error = %v", err)
	}
	if runCalls, _ := graph.snapshot(); runCalls != 0 {
		t.Fatalf("empty rows graph calls = %d, want 0", runCalls)
	}
}

func TestApplySupplyChainKubernetesRuntimeEvidenceRejectsMalformedAndDeduplicatesDualLabelRows(t *testing.T) {
	t.Parallel()

	digest := "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	valid := map[string]any{
		"matched_digest": digest, "workload_uid": "kw-dual", "edge_scope_id": "scope-edge", "edge_generation_id": "gen-edge",
	}
	graph := &stubKubernetesRuntimeGraph{rows: []map[string]any{
		valid,
		valid,
		{"matched_digest": digest, "workload_uid": "kw-missing-edge"},
	}}
	inventory := &stubKubernetesWorkloadInventory{rows: []KubernetesRuntimeWorkloadMatch{{
		Digest: digest, WorkloadRef: impact.KubernetesRuntimeWorkloadRef{UID: "kw-dual", ClusterID: "cluster-a", Namespace: "default", Name: "api"},
	}}}
	rows := []impact.SupplyChainImpactFindingRow{{FindingID: "f", SubjectDigest: digest}}
	if err := (&SupplyChainHandler{Neo4j: graph, KubernetesWorkloadInventory: inventory}).
		applySupplyChainKubernetesRuntimeEvidence(context.Background(), repositoryAccessFilter{AllScopes: true}, rows); err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if got := len(inventory.candidates); got != 1 {
		t.Fatalf("inventory candidates = %#v, want one valid deduplicated candidate", inventory.candidates)
	}
	if got := len(rows[0].KubernetesRuntimeWorkloadRefs); got != 1 {
		t.Fatalf("public workload refs = %#v, want one", rows[0].KubernetesRuntimeWorkloadRefs)
	}
}

func TestApplySupplyChainKubernetesRuntimeEvidenceSkipsUnconfiguredGraphWithoutSubjectDigests(t *testing.T) {
	t.Parallel()

	inventory := &stubKubernetesWorkloadInventory{}
	handler := &SupplyChainHandler{KubernetesWorkloadInventory: inventory}
	rows := []impact.SupplyChainImpactFindingRow{
		{FindingID: "empty"},
		{FindingID: "whitespace", SubjectDigest: "  \t"},
	}

	if err := handler.applySupplyChainKubernetesRuntimeEvidence(context.Background(), repositoryAccessFilter{AllScopes: true}, rows); err != nil {
		t.Fatalf("applySupplyChainKubernetesRuntimeEvidence() error = %v, want nil", err)
	}
	if len(inventory.candidates) != 0 {
		t.Fatalf("inventory candidates = %v, want none", inventory.candidates)
	}
}

func TestApplySupplyChainKubernetesRuntimeEvidenceRejectsStoreCrossFindingMismatch(t *testing.T) {
	t.Parallel()

	digestA := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestB := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	graph := &stubKubernetesRuntimeGraph{rows: []map[string]any{{
		"matched_digest": digestA, "workload_uid": "kw-a", "edge_scope_id": "scope-a", "edge_generation_id": "gen-a",
	}}}
	inventory := &stubKubernetesWorkloadInventory{rows: []KubernetesRuntimeWorkloadMatch{{
		Digest: digestB, WorkloadRef: impact.KubernetesRuntimeWorkloadRef{UID: "kw-a", ClusterID: "cluster-a", Namespace: "default", Name: "api"},
	}}}
	rows := []impact.SupplyChainImpactFindingRow{
		{FindingID: "a", SubjectDigest: digestA},
		{FindingID: "b", SubjectDigest: digestB},
	}
	if err := (&SupplyChainHandler{Neo4j: graph, KubernetesWorkloadInventory: inventory}).
		applySupplyChainKubernetesRuntimeEvidence(context.Background(), repositoryAccessFilter{AllScopes: true}, rows); err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if len(rows[0].KubernetesRuntimeWorkloadRefs) != 0 || len(rows[1].KubernetesRuntimeWorkloadRefs) != 0 {
		t.Fatalf("cross-finding store mismatch attached refs: a=%#v b=%#v", rows[0].KubernetesRuntimeWorkloadRefs, rows[1].KubernetesRuntimeWorkloadRefs)
	}
}

func TestApplySupplyChainKubernetesRuntimeEvidenceRecordsZeroInitializedAndFinalCounts(t *testing.T) {
	digest := "sha256:abababababababababababababababababababababababababababababababab"
	tests := []struct {
		name         string
		findingCount int
		graphRows    []map[string]any
		matches      []KubernetesRuntimeWorkloadMatch
		want         map[string]int64
	}{
		{
			name: "zero candidates",
			want: map[string]int64{
				"eshu.subject_digest_count": 1, "eshu.graph_candidate_count": 0,
				"eshu.authorized_current_workload_count": 0, "eshu.runtime_confirmed_digest_count": 0,
				"eshu.runtime_workload_count":         0,
				"eshu.kubernetes_runtime_query_count": 1, "eshu.kubernetes_runtime_concurrency_limit": 1,
				"eshu.kubernetes_runtime_max_concurrency": 1, "eshu.kubernetes_runtime_candidate_limit": 201,
				"eshu.kubernetes_runtime_truncated_digest_count": 0, "eshu.kubernetes_runtime_unknown_digest_count": 0,
			},
		},
		{
			name: "one authorized current workload",
			graphRows: []map[string]any{{
				"matched_digest": digest, "workload_uid": "kw-1", "edge_scope_id": "scope-1", "edge_generation_id": "gen-1",
			}},
			matches: []KubernetesRuntimeWorkloadMatch{{
				Digest: digest, WorkloadRef: impact.KubernetesRuntimeWorkloadRef{UID: "kw-1", ClusterID: "cluster-a", Namespace: "default", Name: "api"},
			}},
			want: map[string]int64{
				"eshu.subject_digest_count": 1, "eshu.graph_candidate_count": 1,
				"eshu.authorized_current_workload_count": 1, "eshu.runtime_confirmed_digest_count": 1,
				"eshu.runtime_workload_count":         1,
				"eshu.kubernetes_runtime_query_count": 1, "eshu.kubernetes_runtime_concurrency_limit": 1,
				"eshu.kubernetes_runtime_max_concurrency": 1, "eshu.kubernetes_runtime_candidate_limit": 201,
				"eshu.kubernetes_runtime_truncated_digest_count": 0, "eshu.kubernetes_runtime_unknown_digest_count": 0,
			},
		},
		{
			name:         "duplicate findings count one digest and workload",
			findingCount: 2,
			graphRows: []map[string]any{{
				"matched_digest": digest, "workload_uid": "kw-1", "edge_scope_id": "scope-1", "edge_generation_id": "gen-1",
			}},
			matches: []KubernetesRuntimeWorkloadMatch{{
				Digest: digest, WorkloadRef: impact.KubernetesRuntimeWorkloadRef{UID: "kw-1"},
			}},
			want: map[string]int64{
				"eshu.subject_digest_count": 1, "eshu.graph_candidate_count": 1,
				"eshu.authorized_current_workload_count": 1, "eshu.runtime_confirmed_digest_count": 1,
				"eshu.runtime_workload_count": 1,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
			previousTracer := queryHandlerTracer
			queryHandlerTracer = provider.Tracer("supply-chain-kubernetes-runtime-probe-test")
			t.Cleanup(func() { queryHandlerTracer = previousTracer })

			findingCount := tt.findingCount
			if findingCount == 0 {
				findingCount = 1
			}
			rows := make([]impact.SupplyChainImpactFindingRow, findingCount)
			for i := range rows {
				rows[i] = impact.SupplyChainImpactFindingRow{FindingID: fmt.Sprintf("f-%d", i), SubjectDigest: digest}
			}
			handler := &SupplyChainHandler{
				Neo4j:                       &stubKubernetesRuntimeGraph{rows: tt.graphRows},
				KubernetesWorkloadInventory: &stubKubernetesWorkloadInventory{rows: tt.matches},
			}
			if err := handler.applySupplyChainKubernetesRuntimeEvidence(context.Background(), repositoryAccessFilter{AllScopes: true}, rows); err != nil {
				t.Fatalf("apply error = %v", err)
			}
			spans := recorder.Ended()
			if len(spans) != 1 || spans[0].Name() != "supply_chain.kubernetes_runtime_probe" {
				t.Fatalf("ended spans = %#v, want one kubernetes runtime child span", spans)
			}
			attributes := make(map[string]any)
			for _, item := range spans[0].Attributes() {
				attributes[string(item.Key)] = item.Value.AsInterface()
			}
			for key, want := range tt.want {
				if got := attributes[key]; got != want {
					t.Fatalf("span attribute %s = %#v, want %#v; attributes=%#v", key, got, want, attributes)
				}
			}
		})
	}
}

func TestApplySupplyChainKubernetesRuntimeEvidenceErrorSpanKeepsPlannedBounds(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previousTracer := queryHandlerTracer
	queryHandlerTracer = provider.Tracer("supply-chain-kubernetes-runtime-probe-error-test")
	t.Cleanup(func() { queryHandlerTracer = previousTracer })

	digests := []string{
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}
	rows := make([]impact.SupplyChainImpactFindingRow, len(digests))
	for i, digest := range digests {
		rows[i] = impact.SupplyChainImpactFindingRow{FindingID: digest, SubjectDigest: digest}
	}
	wantErr := errors.New("graph unavailable")
	graph := &fairKubernetesRuntimeGraph{errorDigest: digests[0], err: wantErr}
	err := (&SupplyChainHandler{Neo4j: graph, KubernetesWorkloadInventory: &stubKubernetesWorkloadInventory{}}).
		applySupplyChainKubernetesRuntimeEvidence(context.Background(), repositoryAccessFilter{AllScopes: true}, rows)
	if !errors.Is(err, wantErr) {
		t.Fatalf("apply error = %v, want %v", err, wantErr)
	}
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	attributes := make(map[string]any)
	for _, item := range spans[0].Attributes() {
		attributes[string(item.Key)] = item.Value.AsInterface()
	}
	want := map[string]int64{
		"eshu.kubernetes_runtime_query_count":       3,
		"eshu.kubernetes_runtime_concurrency_limit": 3,
		"eshu.kubernetes_runtime_candidate_limit":   203,
	}
	for key, value := range want {
		if got := attributes[key]; got != value {
			t.Fatalf("span attribute %s = %#v, want %#v; attributes=%#v", key, got, value, attributes)
		}
	}
}
