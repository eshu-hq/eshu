// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package query

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain"
	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const (
	kubernetesRuntimePerformanceLiveEnv = "ESHU_KUBERNETES_RUNTIME_PROBE_PERFORMANCE_LIVE"
	kubernetesRuntimeCypherSHA256       = "1900bbe55bc2f6e63da43ec9f8a6c1ed67e416702ab718aebd4db6ea2c794fee"
	kubernetesRuntimePerformanceScope   = "cluster:kubernetes-runtime-performance-live"
	kubernetesRuntimePerformanceGen     = "generation:kubernetes-runtime-performance-live"
)

func TestLiveKubernetesRuntimeProbePerformance(t *testing.T) {
	if strings.TrimSpace(os.Getenv(kubernetesRuntimePerformanceLiveEnv)) != "1" {
		t.Skip("set ESHU_KUBERNETES_RUNTIME_PROBE_PERFORMANCE_LIVE=1 to run the live performance proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db := openKubernetesRuntimePerformancePostgres(t)
	driver := openKubernetesRuntimePerformanceGraph(t, ctx)

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previousProvider := otel.GetTracerProvider()
	previousQueryTracer := queryHandlerTracer
	otel.SetTracerProvider(provider)
	queryHandlerTracer = provider.Tracer("kubernetes-runtime-performance-live")
	t.Cleanup(func() {
		queryHandlerTracer = previousQueryTracer
		otel.SetTracerProvider(previousProvider)
		_ = provider.Shutdown(context.Background())
	})

	reader := NewNeo4jReader(driver, "nornic")
	store := NewPostgresKubernetesRuntimeWorkloadStore(db)
	digests, candidates := seedKubernetesRuntimePerformanceDataset(t, ctx, driver, db)
	sum := sha256.Sum256([]byte(supplychain.SupplyChainKubernetesRuntimeProbeCypher))
	if got := hex.EncodeToString(sum[:]); got != kubernetesRuntimeCypherSHA256 {
		t.Fatalf("production Kubernetes runtime Cypher SHA-256 = %s, want %s", got, kubernetesRuntimeCypherSHA256)
	}
	seeded, err := reader.Run(ctx, `MATCH (img:ContainerImage)<-[rel:RUNS_IMAGE]-(w:KubernetesWorkload)
RETURN img.digest AS matched_digest, w.uid AS workload_uid,
       rel.scope_id AS edge_scope_id, rel.generation_id AS edge_generation_id,
       rel.evidence_source AS evidence_source, rel.resolution_mode AS resolution_mode,
       rel.source_digest AS source_digest
ORDER BY matched_digest, workload_uid LIMIT 1`, nil)
	if err != nil || len(seeded) != 1 {
		t.Fatalf("read seeded production graph shape: rows=%d error=%v", len(seeded), err)
	}
	t.Logf("seeded production graph row: %#v", seeded[0])
	probeParams := map[string]any{
		"subject_digests": []string{digests[0]},
		"evidence_source": supplychain.SupplyChainKubernetesRuntimeEvidenceSource,
		"resolution_mode": supplychain.SupplyChainKubernetesRuntimeResolutionMode,
		"limit":           2,
	}
	filtered, err := reader.Run(ctx, `UNWIND $subject_digests AS candidate_digest
MATCH (img:ContainerImage {digest: candidate_digest})<-[rel:RUNS_IMAGE]-(w:KubernetesWorkload)
WHERE rel.evidence_source = $evidence_source
  AND rel.resolution_mode = $resolution_mode
  AND rel.source_digest = candidate_digest
RETURN candidate_digest AS matched_digest, w.uid AS workload_uid,
       rel.scope_id AS edge_scope_id, rel.generation_id AS edge_generation_id
ORDER BY matched_digest, workload_uid LIMIT $limit`, probeParams)
	if err != nil || len(filtered) != 2 {
		t.Fatalf("read filtered single-arm graph shape: rows=%d error=%v", len(filtered), err)
	}
	exact, err := reader.Run(ctx, supplychain.SupplyChainKubernetesRuntimeProbeCypher, probeParams)
	if err != nil || len(exact) != 2 {
		t.Fatalf("read exact production graph shape: rows=%d error=%v", len(exact), err)
	}

	plans := supplychain.PlanKubernetesRuntimeProbeQueries(digests, true)
	fanout, err := supplychain.QueryKubernetesRuntimeCandidates(ctx, reader, plans)
	if err != nil {
		t.Fatalf("run real balanced fanout: %v", err)
	}
	if fanout.MaxConcurrency() < 2 || fanout.MaxConcurrency() > supplychain.SupplyChainKubernetesRuntimeProbeMaxConcurrency {
		t.Fatalf("real fanout max concurrency = %d, want 2..%d", fanout.MaxConcurrency(), supplychain.SupplyChainKubernetesRuntimeProbeMaxConcurrency)
	}
	if got := len(fanout.Candidates()); got != supplychain.SupplyChainKubernetesRuntimeProbeMaxAllScopesCandidates {
		t.Fatalf("real fanout candidates = %d, want %d", got, supplychain.SupplyChainKubernetesRuntimeProbeMaxAllScopesCandidates)
	}
	t.Logf("real driver fanout: queries=%d candidate_limit=%d max_concurrency=%d", len(plans), fanout.PlannedCandidateLimit(), fanout.MaxConcurrency())

	legacy := func(runCtx context.Context) kubernetesRuntimePerformanceResult {
		return runLegacyKubernetesRuntimePerformance(t, runCtx, reader, store, digests)
	}
	balanced := func(runCtx context.Context) kubernetesRuntimePerformanceResult {
		return runBalancedKubernetesRuntimePerformance(t, runCtx, reader, store, digests)
	}
	legacySingle := measureKubernetesRuntimePerformance(t, ctx, 3, 15, legacy)
	balancedSingle := measureKubernetesRuntimePerformance(t, ctx, 3, 15, balanced)
	legacyConcurrent := measureConcurrentKubernetesRuntimePerformance(t, ctx, 4, 2, 8, legacy)
	balancedConcurrent := measureConcurrentKubernetesRuntimePerformance(t, ctx, 4, 2, 8, balanced)
	t.Logf("single request: legacy p50=%s p95=%s; balanced p50=%s p95=%s", legacySingle.p50, legacySingle.p95, balancedSingle.p50, balancedSingle.p95)
	t.Logf("four concurrent requests: legacy p50=%s p95=%s; balanced p50=%s p95=%s", legacyConcurrent.p50, legacyConcurrent.p95, balancedConcurrent.p50, balancedConcurrent.p95)

	if legacySingle.last.representedDigests != 1 || legacySingle.last.refs != 200 {
		t.Fatalf("legacy truth = %#v, want 1 represented digest and 200 refs", legacySingle.last)
	}
	if balancedSingle.last.representedDigests != len(digests) || balancedSingle.last.refs != 200 {
		t.Fatalf("balanced truth = %#v, want %d represented digests and 200 refs", balancedSingle.last, len(digests))
	}

	if !legacyConcurrent.consistent || legacyConcurrent.last.representedDigests != 1 || legacyConcurrent.last.refs != 200 {
		t.Fatalf("concurrent legacy truth varied: %#v", legacyConcurrent)
	}
	if !balancedConcurrent.consistent || balancedConcurrent.last.representedDigests != len(digests) || balancedConcurrent.last.refs != 200 {
		t.Fatalf("concurrent balanced truth varied: %#v", balancedConcurrent)
	}
	assertKubernetesRuntimeGraphReadSingleAttemptTelemetry(t, recorder.Ended())
	assertKubernetesRuntimePostgresPlans(t, ctx, db, candidates)
	assertKubernetesRuntimeCancellationRecovery(t, ctx, reader, plans)
}

type kubernetesRuntimePerformanceResult struct {
	duration           time.Duration
	representedDigests int
	refs               int
}

type kubernetesRuntimePerformanceSummary struct {
	p50        time.Duration
	p95        time.Duration
	last       kubernetesRuntimePerformanceResult
	consistent bool
}

func measureKubernetesRuntimePerformance(
	t *testing.T,
	ctx context.Context,
	warmups int,
	runs int,
	operation func(context.Context) kubernetesRuntimePerformanceResult,
) kubernetesRuntimePerformanceSummary {
	t.Helper()
	for range warmups {
		operation(ctx)
	}
	results := make([]kubernetesRuntimePerformanceResult, runs)
	for i := range results {
		results[i] = operation(ctx)
	}
	return summarizeKubernetesRuntimePerformance(results)
}

func measureConcurrentKubernetesRuntimePerformance(
	t *testing.T,
	ctx context.Context,
	concurrency int,
	warmups int,
	runs int,
	operation func(context.Context) kubernetesRuntimePerformanceResult,
) kubernetesRuntimePerformanceSummary {
	t.Helper()
	for range warmups {
		runConcurrentKubernetesRuntimePerformance(t, ctx, concurrency, operation)
	}
	results := make([]kubernetesRuntimePerformanceResult, 0, concurrency*runs)
	for range runs {
		results = append(results, runConcurrentKubernetesRuntimePerformance(t, ctx, concurrency, operation)...)
	}
	return summarizeKubernetesRuntimePerformance(results)
}

func runConcurrentKubernetesRuntimePerformance(
	t *testing.T,
	ctx context.Context,
	concurrency int,
	operation func(context.Context) kubernetesRuntimePerformanceResult,
) []kubernetesRuntimePerformanceResult {
	t.Helper()
	results := make([]kubernetesRuntimePerformanceResult, concurrency)
	var workers sync.WaitGroup
	workers.Add(concurrency)
	for i := range results {
		go func() {
			defer workers.Done()
			results[i] = operation(ctx)
		}()
	}
	workers.Wait()
	return results
}

func summarizeKubernetesRuntimePerformance(results []kubernetesRuntimePerformanceResult) kubernetesRuntimePerformanceSummary {
	durations := make([]time.Duration, len(results))
	consistent := true
	for i, result := range results {
		durations[i] = result.duration
		if i > 0 && (result.representedDigests != results[0].representedDigests || result.refs != results[0].refs) {
			consistent = false
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	percentile := func(value int) time.Duration {
		index := (len(durations)*value + 99) / 100
		return durations[max(index-1, 0)]
	}
	return kubernetesRuntimePerformanceSummary{
		p50: percentile(50), p95: percentile(95), last: results[len(results)-1], consistent: consistent,
	}
}

func runLegacyKubernetesRuntimePerformance(
	t *testing.T,
	ctx context.Context,
	reader *Neo4jReader,
	store *PostgresKubernetesRuntimeWorkloadStore,
	digests []string,
) kubernetesRuntimePerformanceResult {
	t.Helper()
	started := time.Now()
	rows, err := reader.Run(ctx, supplychain.SupplyChainKubernetesRuntimeProbeCypher, map[string]any{
		"subject_digests": digests,
		"evidence_source": supplychain.SupplyChainKubernetesRuntimeEvidenceSource,
		"resolution_mode": supplychain.SupplyChainKubernetesRuntimeResolutionMode,
		"limit":           supplychain.SupplyChainKubernetesRuntimeProbeMaxResults,
	})
	if err != nil {
		t.Fatalf("run legacy global graph probe: %v", err)
	}
	matches, err := store.CurrentAuthorizedKubernetesRuntimeWorkloads(ctx, supplychain.KubernetesRuntimeCandidates(rows), true, nil, nil)
	if err != nil {
		t.Fatalf("run legacy global Postgres gate: %v", err)
	}
	digestSet := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		digestSet[match.Digest] = struct{}{}
	}
	return kubernetesRuntimePerformanceResult{duration: time.Since(started), representedDigests: len(digestSet), refs: len(matches)}
}

func runBalancedKubernetesRuntimePerformance(
	t *testing.T,
	ctx context.Context,
	reader *Neo4jReader,
	store *PostgresKubernetesRuntimeWorkloadStore,
	digests []string,
) kubernetesRuntimePerformanceResult {
	t.Helper()
	findings := make([]impact.SupplyChainImpactFindingRow, len(digests))
	for i, digest := range digests {
		findings[i] = impact.SupplyChainImpactFindingRow{FindingID: fmt.Sprintf("performance-%03d", i), SubjectDigest: digest}
	}
	started := time.Now()
	err := (&SupplyChainHandler{Neo4j: reader, KubernetesWorkloadInventory: store}).
		ApplySupplyChainKubernetesRuntimeEvidenceLive(ctx, querycontract.RepositoryAccessFilter{AllScopes: true}, findings)
	if err != nil {
		t.Fatalf("run balanced production probe: %v", err)
	}
	refs := 0
	represented := 0
	for _, finding := range findings {
		refs += len(finding.KubernetesRuntimeWorkloadRefs)
		if len(finding.KubernetesRuntimeWorkloadRefs) > 0 {
			represented++
		}
	}
	return kubernetesRuntimePerformanceResult{duration: time.Since(started), representedDigests: represented, refs: refs}
}

func openKubernetesRuntimePerformanceGraph(
	t *testing.T,
	ctx context.Context,
) neo4jdriver.DriverWithContext {
	t.Helper()
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required for the live performance proof")
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth(), func(config *neo4jdriver.Config) {
		config.MaxConnectionPoolSize = supplychain.SupplyChainKubernetesRuntimeProbeMaxConcurrency
	})
	if err != nil {
		t.Fatalf("open NornicDB driver: %v", err)
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(context.Background())
		t.Fatalf("verify NornicDB connectivity: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	return driver
}

func seedKubernetesRuntimePerformanceDataset(
	t *testing.T,
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	db *sql.DB,
) ([]string, []KubernetesRuntimeCandidate) {
	t.Helper()
	digests := make([]string, supplychain.SupplyChainKubernetesRuntimeProbeMaxResults)
	candidates := make([]KubernetesRuntimeCandidate, 0, 1398)
	graphRows := make([]map[string]any, 0, 1398)
	for i := range digests {
		digest := fmt.Sprintf("sha256:%064x", i)
		digests[i] = digest
		count := 2
		if i == 0 {
			count = 1000
		}
		for j := range count {
			candidate := KubernetesRuntimeCandidate{
				WorkloadUID: fmt.Sprintf("performance-%03d-%04d", i, j), Digest: digest,
				EdgeScopeID: kubernetesRuntimePerformanceScope, EdgeGenerationID: kubernetesRuntimePerformanceGen,
			}
			candidates = append(candidates, candidate)
			graphRows = append(graphRows, map[string]any{
				"digest": digest, "uid": candidate.WorkloadUID, "source_uid": "image:" + candidate.WorkloadUID,
			})
		}
	}
	seedKubernetesRuntimePerformancePostgres(t, ctx, db, candidates)
	writeKubernetesRuntimePerformanceGraph(t, ctx, driver, `
UNWIND $rows AS row
CREATE (img:OciImageManifest:ContainerImage {
  uid: row.source_uid, digest: row.digest, performance_proof: true
})
CREATE (workload:KubernetesWorkload {uid: row.uid, performance_proof: true})`, map[string]any{"rows": graphRows})
	// Use the production writer's MATCH/MATCH/MERGE/SET shape. NornicDB does
	// not persist relationship properties when CREATE and SET share the node-
	// creation statement, which would make the fixture unlike reducer output.
	writeKubernetesRuntimePerformanceGraph(t, ctx, driver, `
UNWIND $rows AS row
MATCH (workload:KubernetesWorkload {uid: row.uid})
MATCH (img:OciImageManifest {uid: row.source_uid})
MERGE (workload)-[runtime:RUNS_IMAGE]->(img)
SET runtime.evidence_source = $evidence_source,
    runtime.resolution_mode = $resolution_mode,
    runtime.source_digest = row.digest,
    runtime.scope_id = $scope_id,
    runtime.generation_id = $generation_id`, map[string]any{
		"rows": graphRows, "evidence_source": supplychain.SupplyChainKubernetesRuntimeEvidenceSource,
		"resolution_mode": supplychain.SupplyChainKubernetesRuntimeResolutionMode,
		"scope_id":        kubernetesRuntimePerformanceScope, "generation_id": kubernetesRuntimePerformanceGen,
	})
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		writeKubernetesRuntimePerformanceGraph(t, cleanupCtx, driver,
			`MATCH (w:KubernetesWorkload {performance_proof: true}) DETACH DELETE w`, nil)
		writeKubernetesRuntimePerformanceGraph(t, cleanupCtx, driver,
			`MATCH (img:ContainerImage {performance_proof: true}) DETACH DELETE img`, nil)
	})
	return digests, candidates
}

func writeKubernetesRuntimePerformanceGraph(
	t *testing.T,
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	cypher string,
	params map[string]any,
) {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: "nornic"})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err == nil {
		_, err = result.Consume(ctx)
	}
	if err != nil {
		t.Fatalf("write NornicDB performance fixture: %v", err)
	}
}

func assertKubernetesRuntimeGraphReadSingleAttemptTelemetry(t *testing.T, spans []sdktrace.ReadOnlySpan) {
	t.Helper()
	graphReads := 0
	for _, span := range spans {
		if span.Name() != "neo4j.query" {
			continue
		}
		graphReads++
		attributes := make(map[string]any)
		for _, item := range span.Attributes() {
			attributes[string(item.Key)] = item.Value.AsInterface()
		}
		if attributes[telemetry.SpanAttrGraphReadOutcome] != "success" || attributes[telemetry.SpanAttrGraphReadAttempts] != int64(1) {
			t.Fatalf("graph read used retry/fallback path: %#v", attributes)
		}
	}
	if graphReads == 0 {
		t.Fatal("no Neo4jReader spans captured")
	}
	t.Logf("Neo4jReader reads: count=%d attempts=1 outcome=success", graphReads)
}

func assertKubernetesRuntimeCancellationRecovery(
	t *testing.T,
	ctx context.Context,
	reader *Neo4jReader,
	plans []supplychain.KubernetesRuntimeProbePlan,
) {
	t.Helper()
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := supplychain.QueryKubernetesRuntimeCandidates(canceled, reader, plans); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled fanout error = %v, want context canceled", err)
	}
	recovered, err := supplychain.QueryKubernetesRuntimeCandidates(ctx, reader, plans)
	if err != nil {
		t.Fatalf("fanout after cancellation: %v", err)
	}
	if len(recovered.Candidates()) != supplychain.SupplyChainKubernetesRuntimeProbeMaxAllScopesCandidates {
		t.Fatalf("fanout after cancellation candidates = %d, want %d", len(recovered.Candidates()), supplychain.SupplyChainKubernetesRuntimeProbeMaxAllScopesCandidates)
	}
	t.Logf("cancellation recovery: canceled request drained; next request candidates=%d", len(recovered.Candidates()))
}
