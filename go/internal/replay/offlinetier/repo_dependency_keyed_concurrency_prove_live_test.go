// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// This opt-in theory shim checks whether repository-keyed concurrency is safe
// for the repo-dependency graph write boundary. It intentionally does not
// change the production runner. Each source repository keeps an atomic,
// ordered retract-then-write cycle while unrelated source keys run with one,
// two, or four workers against a real NornicDB.
//
// The fixture includes stale-edge cleanup, target fan-in, reciprocal edges,
// shared Environment identity, and duplicate full-cycle replay. Serial and
// concurrent cells must produce the same canonical graph with no duplicates.
//
// Skills active: golang-engineering, eshu-diagnostic-rigor,
// eshu-performance-rigor, eshu-correlation-truth, cypher-query-rigor,
// concurrency-deadlock-rigor.

package offlinetier_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	repoDependencyConcurrencyLiveEnv = "ESHU_REPO_DEPENDENCY_CONCURRENCY_PROVE_LIVE"
	repoDependencyConcurrencyMarker  = "replay-repodep-keyed-concurrency"
	repoDependencyConcurrencyEnv     = "replay-repodep-keyed-concurrency-env"
	repoDependencyConcurrencySource  = "resolver/cross-repo"
	repoDependencyConcurrencyShared  = "replay-repodep-keyed-concurrency:shared-target"
	repoDependencyConcurrencyStale   = "replay-repodep-keyed-concurrency:stale-target"
)

type repoDependencyConcurrencyFixture struct {
	sources       []string
	uniqueTargets map[string]string
	reciprocal    map[string]string
}

// TestRepoDependencyKeyedConcurrencyProveTheory is a disposable, real-backend
// proof for the repository-keyed concurrency theory. It is not a substitute
// for the eventual Ifa worker, fault-injection, and built-runner matrices.
func TestRepoDependencyKeyedConcurrencyProveTheory(t *testing.T) {
	if os.Getenv(repoDependencyConcurrencyLiveEnv) != "1" {
		t.Skipf("set %s=1 to run the repository-keyed concurrency theory proof", repoDependencyConcurrencyLiveEnv)
	}
	if !liveTierEnabled() {
		t.Skipf("set %s=1 and real NornicDB connection variables", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	exec, _ := openDeltaLiveBackend(ctx, t)
	acquireRepoDependencyIfaExclusiveBackend(ctx, t, exec, nil)
	fixture := newRepoDependencyConcurrencyFixture(8)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		cleanupRepoDependencyConcurrencyScope(cleanupCtx, t, exec, nil)
	})

	var baseline []string
	for _, workers := range []int{1, 2, 4} {
		cleanupRepoDependencyConcurrencyScope(ctx, t, exec, nil)
		seedRepoDependencyConcurrencyFixture(ctx, t, exec, fixture)
		seedRepoDependencyConcurrencyStaleEdges(ctx, t, exec, fixture)

		// Candidate shape: the existing production RetryingExecutor owns the
		// complete MERGE-shaped write group. This is the same bounded retry
		// mechanism intended for NornicDB commit-time UNIQUE races, not an
		// outer test-only retry of the repository cycle.
		retrying := &cypher.RetryingExecutor{Inner: exec}
		started := time.Now()
		if err := runRepoDependencyConcurrencyCycles(ctx, retrying, fixture, workers); err != nil {
			t.Fatalf("workers=%d keyed cycles: %v", workers, err)
		}
		elapsed := time.Since(started)

		got := readRepoDependencyConcurrencySnapshot(ctx, t, exec)
		assertRepoDependencyConcurrencyTruth(ctx, t, exec, fixture, got)
		if workers == 1 {
			baseline = got
			t.Logf("workers=%d elapsed=%s canonical_rows=%d baseline", workers, elapsed, len(got))
			continue
		}

		missing, extra := bidirectionalStringDiff(baseline, got)
		t.Logf(
			"workers=%d elapsed=%s canonical_rows=%d serial_to_concurrent_diff=%d/%d",
			workers,
			elapsed,
			len(got),
			len(missing),
			len(extra),
		)
		if len(missing) != 0 || len(extra) != 0 {
			t.Fatalf("workers=%d graph differs from serial: missing=%v extra=%v", workers, missing, extra)
		}
	}
}

func newRepoDependencyConcurrencyFixture(sourceCount int) repoDependencyConcurrencyFixture {
	fixture := repoDependencyConcurrencyFixture{
		sources:       make([]string, 0, sourceCount),
		uniqueTargets: make(map[string]string, sourceCount),
		reciprocal:    make(map[string]string, sourceCount),
	}
	for i := 0; i < sourceCount; i++ {
		source := fmt.Sprintf("%s:source-%02d", repoDependencyConcurrencyMarker, i)
		fixture.sources = append(fixture.sources, source)
		fixture.uniqueTargets[source] = fmt.Sprintf("%s:unique-target-%02d", repoDependencyConcurrencyMarker, i)
	}
	for i, source := range fixture.sources {
		if i%2 == 0 {
			fixture.reciprocal[source] = fixture.sources[i+1]
		} else {
			fixture.reciprocal[source] = fixture.sources[i-1]
		}
	}
	return fixture
}

func runRepoDependencyConcurrencyCycles(
	ctx context.Context,
	exec cypher.Executor,
	fixture repoDependencyConcurrencyFixture,
	workers int,
) error {
	jobs := make(chan string)
	errs := make(chan error, len(fixture.sources))
	var workerGroup sync.WaitGroup

	for i := 0; i < workers; i++ {
		workerGroup.Add(1)
		go func() {
			defer workerGroup.Done()
			for source := range jobs {
				if err := runRepoDependencySourceCycle(ctx, exec, fixture, source); err != nil {
					errs <- fmt.Errorf("source %s: %w", source, err)
				}
			}
		}()
	}

	for _, source := range fixture.sources {
		jobs <- source
	}
	close(jobs)
	workerGroup.Wait()
	close(errs)

	var messages []string
	for err := range errs {
		messages = append(messages, err.Error())
	}
	if len(messages) != 0 {
		sort.Strings(messages)
		return fmt.Errorf("%d cycle errors: %s", len(messages), strings.Join(messages, "; "))
	}
	return nil
}

func runRepoDependencySourceCycle(
	ctx context.Context,
	exec cypher.Executor,
	fixture repoDependencyConcurrencyFixture,
	source string,
) error {
	writer := cypher.NewEdgeWriter(exec, 0)
	retractRows := []reducer.SharedProjectionIntentRow{{
		IntentID:     "retract-" + source,
		RepositoryID: source,
		Payload:      map[string]any{"repo_id": source},
	}}
	writes := repoDependencyConcurrencyRows(fixture, source)

	// Repeat the complete same-source cycle in FIFO order. This catches stale
	// cleanup and idempotent duplicate replay without permitting two workers to
	// mutate the same source repository concurrently.
	for replay := 0; replay < 2; replay++ {
		if err := writer.RetractEdges(ctx, reducer.DomainRepoDependency, retractRows, repoDependencyConcurrencySource); err != nil {
			return fmt.Errorf("replay %d retract: %w", replay, err)
		}
		if err := writer.WriteEdges(ctx, reducer.DomainRepoDependency, writes, repoDependencyConcurrencySource); err != nil {
			return fmt.Errorf("replay %d write: %w", replay, err)
		}
	}
	return nil
}

func repoDependencyConcurrencyRows(
	fixture repoDependencyConcurrencyFixture,
	source string,
) []reducer.SharedProjectionIntentRow {
	return []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "shared-" + source,
			RepositoryID: source,
			GenerationID: "generation-current",
			Payload: map[string]any{
				"repo_id": source, "target_repo_id": repoDependencyConcurrencyShared,
				"relationship_type": "DEPENDS_ON", "resolved_id": "shared-" + source,
				"evidence_artifacts": []any{map[string]any{
					"evidence_kind": "helm_values", "path": "deploy/values.yaml",
					"matched_value": source, "environment": repoDependencyConcurrencyEnv,
					"confidence": 0.91,
				}},
			},
		},
		{
			IntentID:     "reciprocal-" + source,
			RepositoryID: source,
			Payload: map[string]any{
				"repo_id": source, "target_repo_id": fixture.reciprocal[source],
				"relationship_type": "USES_MODULE", "resolved_id": "reciprocal-" + source,
			},
		},
		{
			IntentID:     "unique-" + source,
			RepositoryID: source,
			Payload: map[string]any{
				"repo_id": source, "target_repo_id": fixture.uniqueTargets[source],
				"relationship_type": "DISCOVERS_CONFIG_IN", "resolved_id": "unique-" + source,
			},
		},
	}
}

func seedRepoDependencyConcurrencyFixture(
	ctx context.Context,
	t *testing.T,
	exec liveExecutor,
	fixture repoDependencyConcurrencyFixture,
) {
	t.Helper()
	ids := append([]string{}, fixture.sources...)
	ids = append(ids, repoDependencyConcurrencyShared, repoDependencyConcurrencyStale)
	for _, source := range fixture.sources {
		ids = append(ids, fixture.uniqueTargets[source])
	}
	for _, id := range ids {
		if err := exec.Execute(ctx, cypher.Statement{
			Cypher: `MERGE (repo:Repository {id: $id})
				ON CREATE SET repo.marker = $marker`,
			Parameters: map[string]any{"id": id, "marker": repoDependencyConcurrencyMarker},
		}); err != nil {
			t.Fatalf("seed repository %s: %v", id, err)
		}
		owned, err := exec.count(
			ctx,
			`MATCH (repo:Repository {id: $id, marker: $marker}) RETURN count(repo)`,
			map[string]any{"id": id, "marker": repoDependencyConcurrencyMarker},
		)
		if err != nil {
			t.Fatalf("verify proof-owned repository %s: %v", id, err)
		}
		if owned != 1 {
			t.Fatalf("repository %s is not owned by the repo-dependency proof", id)
		}
	}
}

func seedRepoDependencyConcurrencyStaleEdges(
	ctx context.Context,
	t *testing.T,
	exec liveExecutor,
	fixture repoDependencyConcurrencyFixture,
) {
	t.Helper()
	rows := make([]reducer.SharedProjectionIntentRow, 0, len(fixture.sources))
	for _, source := range fixture.sources {
		rows = append(rows, reducer.SharedProjectionIntentRow{
			IntentID: "stale-" + source, RepositoryID: source,
			Payload: map[string]any{
				"repo_id": source, "target_repo_id": repoDependencyConcurrencyStale,
				"relationship_type": "DEPENDS_ON", "resolved_id": "stale-" + source,
			},
		})
	}
	if err := cypher.NewEdgeWriter(exec, 0).WriteEdges(
		ctx,
		reducer.DomainRepoDependency,
		rows,
		repoDependencyConcurrencySource,
	); err != nil {
		t.Fatalf("seed stale repo-dependency edges: %v", err)
	}
}

func cleanupRepoDependencyConcurrencyScope(
	ctx context.Context,
	t *testing.T,
	exec liveExecutor,
	artifactIDs []string,
) {
	t.Helper()
	statements := []cypher.Statement{
		{
			Cypher: `MATCH (source:Repository {marker: $marker})-[:HAS_DEPLOYMENT_EVIDENCE]->(artifact:EvidenceArtifact)
				DETACH DELETE artifact`,
			Parameters: map[string]any{"marker": repoDependencyConcurrencyMarker},
		},
		{
			Cypher:     `MATCH (n {marker: $marker}) DETACH DELETE n`,
			Parameters: map[string]any{"marker": repoDependencyConcurrencyMarker},
		},
		{
			Cypher:     `MATCH (env:Environment {name: $environment}) DETACH DELETE env`,
			Parameters: map[string]any{"environment": "ifa-prod-proof"},
		},
		{
			Cypher:     `MATCH (env:Environment {name: $environment}) DETACH DELETE env`,
			Parameters: map[string]any{"environment": repoDependencyConcurrencyEnv},
		},
	}
	for _, artifactID := range artifactIDs {
		parameters := map[string]any{"artifact_id": artifactID}
		statements = append(
			statements,
			cypher.Statement{
				Cypher:     `MATCH (artifact:EvidenceArtifact {id: $artifact_id}) DETACH DELETE artifact`,
				Parameters: parameters,
			},
		)
	}
	for _, stmt := range statements {
		if err := exec.Execute(ctx, stmt); err != nil {
			t.Fatalf("cleanup repository concurrency scope: %v", err)
		}
	}
}

func readRepoDependencyConcurrencySnapshot(ctx context.Context, t *testing.T, exec liveExecutor) []string {
	t.Helper()
	queries := []cypher.Statement{
		{
			Cypher: `MATCH (source:Repository {marker: $marker})-[rel:DEPENDS_ON|USES_MODULE|DISCOVERS_CONFIG_IN]->(target:Repository)
RETURN source.id, type(rel), target.id, rel.evidence_source, count(rel)`,
			Parameters: map[string]any{"marker": repoDependencyConcurrencyMarker},
		},
		{
			Cypher: `MATCH (source:Repository {marker: $marker})-[has:HAS_DEPLOYMENT_EVIDENCE]->(artifact:EvidenceArtifact)
MATCH (artifact)-[evidences:EVIDENCES_REPOSITORY_RELATIONSHIP]->(target:Repository)
MATCH (artifact)-[environment:TARGETS_ENVIRONMENT]->(env:Environment)
RETURN source.id, artifact.matched_value, target.id, env.name, count(has), count(evidences), count(environment)`,
			Parameters: map[string]any{"marker": repoDependencyConcurrencyMarker},
		},
	}

	var rows []string
	for queryIndex, stmt := range queries {
		session := exec.driver.NewSession(ctx, exec.sessionConfig(neo4jdriver.AccessModeRead))
		result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
		if err != nil {
			_ = session.Close(ctx)
			t.Fatalf("snapshot query %d: %v", queryIndex, err)
		}
		for result.Next(ctx) {
			values := result.Record().Values
			parts := make([]string, 0, len(values)+1)
			parts = append(parts, fmt.Sprintf("query-%d", queryIndex))
			for _, value := range values {
				parts = append(parts, fmt.Sprint(value))
			}
			rows = append(rows, strings.Join(parts, "|"))
		}
		if err := result.Err(); err != nil {
			_ = session.Close(ctx)
			t.Fatalf("iterate snapshot query %d: %v", queryIndex, err)
		}
		if _, err := result.Consume(ctx); err != nil {
			_ = session.Close(ctx)
			t.Fatalf("consume snapshot query %d: %v", queryIndex, err)
		}
		if err := session.Close(ctx); err != nil {
			t.Fatalf("close snapshot query %d session: %v", queryIndex, err)
		}
	}
	sort.Strings(rows)
	return rows
}

func assertRepoDependencyConcurrencyTruth(
	ctx context.Context,
	t *testing.T,
	exec liveExecutor,
	fixture repoDependencyConcurrencyFixture,
	snapshot []string,
) {
	t.Helper()
	wantRows := len(fixture.sources) * 4 // Three typed edges and one evidence row per source.
	if len(snapshot) != wantRows {
		t.Fatalf("canonical snapshot rows=%d, want %d: %v", len(snapshot), wantRows, snapshot)
	}
	for _, row := range snapshot {
		parts := strings.Split(row, "|")
		for _, copies := range parts[len(parts)-1:] {
			if copies != "1" {
				t.Fatalf("duplicate or missing canonical relationship count in %q", row)
			}
		}
		if strings.HasPrefix(row, "query-1|") {
			for _, copies := range parts[len(parts)-3:] {
				if copies != "1" {
					t.Fatalf("duplicate evidence relationship count in %q", row)
				}
			}
		}
	}

	stale, err := exec.count(
		ctx,
		`MATCH (:Repository {marker: $marker})-[rel:DEPENDS_ON]->(:Repository {id: $stale}) RETURN count(rel)`,
		map[string]any{"marker": repoDependencyConcurrencyMarker, "stale": repoDependencyConcurrencyStale},
	)
	if err != nil {
		t.Fatalf("count stale edges: %v", err)
	}
	if stale != 0 {
		t.Fatalf("stale edge count=%d, want 0", stale)
	}
}

func bidirectionalStringDiff(want, got []string) (missing, extra []string) {
	wantCounts := make(map[string]int, len(want))
	gotCounts := make(map[string]int, len(got))
	for _, row := range want {
		wantCounts[row]++
	}
	for _, row := range got {
		gotCounts[row]++
	}
	for row, count := range wantCounts {
		for i := gotCounts[row]; i < count; i++ {
			missing = append(missing, row)
		}
	}
	for row, count := range gotCounts {
		for i := wantCounts[row]; i < count; i++ {
			extra = append(extra, row)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}
