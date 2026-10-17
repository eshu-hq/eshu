// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// boltProbeTestExecutor wires a live Bolt runner into the EdgeWriter seams:
// writes through runCypherSingle and target-existence probes through
// runCypher, so the guard exercises the real probe statement shape against
// the backend instead of a scripted boolean.
type boltProbeTestExecutor struct {
	runner *boltRetractTestRunner
}

func (e *boltProbeTestExecutor) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	return e.runner.runCypherSingle(ctx, stmt)
}

func (e *boltProbeTestExecutor) ExecuteProbe(ctx context.Context, stmt sourcecypher.Statement) (bool, error) {
	rows, err := e.runner.runCypher(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

func seedTargetPresenceLiveNodes(t *testing.T, ctx context.Context, runner *boltRetractTestRunner, uid, repo, path string) {
	t.Helper()
	// One MERGE per statement: the backend has known multi-clause quirks, so
	// seeds stay single-clause like the other live tests in this package.
	if err := boltWriteStatement(ctx, runner,
		`MERGE (:Function {uid: $uid})`,
		map[string]any{"uid": uid},
	); err != nil {
		t.Fatalf("seed live function node: %v", err)
	}
	if err := boltWriteStatement(ctx, runner,
		`MERGE (:Endpoint {repo_id: $repo, path: $path})`,
		map[string]any{"repo": repo, "path": path},
	); err != nil {
		t.Fatalf("seed live endpoint node: %v", err)
	}
}

func cleanupTargetPresenceLiveNodes(t *testing.T, runner *boltRetractTestRunner, uid, repo string) {
	t.Helper()
	if err := boltWriteStatement(context.Background(), runner,
		`MATCH (n) WHERE n.uid = $uid OR n.repo_id = $repo DETACH DELETE n`,
		map[string]any{"uid": uid, "repo": repo},
	); err != nil {
		t.Errorf("cleanup live target nodes: %v", err)
	}
}

func countLiveHandlesRouteEdges(t *testing.T, ctx context.Context, runner *boltRetractTestRunner, uid, repo, path string) int64 {
	t.Helper()
	n, err := boltCount(ctx, runner,
		`MATCH (:Function {uid: $uid})-[r:HANDLES_ROUTE]->(:Endpoint {repo_id: $repo, path: $path}) RETURN count(r) AS count`,
		map[string]any{"uid": uid, "repo": repo, "path": path},
	)
	if err != nil {
		t.Fatalf("count live handles_route edges: %v", err)
	}
	return n
}

// TestBoltWriteEdgesHandlesRouteAbsentTargetFailsClosed is the live half of
// the #6184 interrupted-rebuild regression: against the real backend, a
// batch whose endpoint target does not exist must fail retryably instead of
// completing a silent zero-edge write.
func TestBoltWriteEdgesHandlesRouteAbsentTargetFailsClosed(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	const (
		uid  = "content-entity:target-miss-live-absent"
		repo = "repository:target-miss-live-absent"
		path = "/absent"
	)
	t.Cleanup(func() { cleanupTargetPresenceLiveNodes(t, runner, uid, repo) })

	writer := NewEdgeWriter(&boltProbeTestExecutor{runner: runner}, 0)
	row := reducer.SharedProjectionIntentRow{
		IntentID:     "target-miss-live-absent",
		RepositoryID: repo,
		Payload: map[string]any{
			"function_entity_id": uid,
			"repo_id":            repo,
			"path":               path,
			"http_method":        "GET",
			"framework":          "express",
			"resolution_method":  "same_file",
			"confidence":         0.95,
			"reason":             "live target-miss proof",
		},
	}
	if _, err := writer.WriteEdges(ctx, reducer.DomainHandlesRoute, []reducer.SharedProjectionIntentRow{row}, "parser/framework-routes"); err == nil {
		t.Fatal("WriteEdges() with an absent endpoint target succeeded silently, want a retryable error")
	} else if !reducer.IsRetryable(err) {
		t.Fatalf("WriteEdges() error = %v, want retryable", err)
	}
	if got := countLiveHandlesRouteEdges(t, ctx, runner, uid, repo, path); got != 0 {
		t.Fatalf("live HANDLES_ROUTE edges = %d, want 0: no statement may run on a target miss", got)
	}
}

// TestBoltWriteEdgesHandlesRoutePresentTargetWrites is the live happy path:
// with both endpoint and function committed, the guarded batch writes the
// edge exactly once.
func TestBoltWriteEdgesHandlesRoutePresentTargetWrites(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	const (
		uid  = "content-entity:target-miss-live-present"
		repo = "repository:target-miss-live-present"
		path = "/present"
	)
	seedTargetPresenceLiveNodes(t, ctx, runner, uid, repo, path)
	t.Cleanup(func() { cleanupTargetPresenceLiveNodes(t, runner, uid, repo) })

	writer := NewEdgeWriter(&boltProbeTestExecutor{runner: runner}, 0)
	row := reducer.SharedProjectionIntentRow{
		IntentID:     "target-miss-live-present",
		RepositoryID: repo,
		Payload: map[string]any{
			"function_entity_id": uid,
			"repo_id":            repo,
			"path":               path,
			"http_method":        "GET",
			"framework":          "express",
			"resolution_method":  "same_file",
			"confidence":         0.95,
			"reason":             "live target-miss proof",
		},
	}
	if _, err := writer.WriteEdges(ctx, reducer.DomainHandlesRoute, []reducer.SharedProjectionIntentRow{row}, "parser/framework-routes"); err != nil {
		t.Fatalf("WriteEdges() with all targets present error = %v", err)
	}
	if got := countLiveHandlesRouteEdges(t, ctx, runner, uid, repo, path); got != 1 {
		t.Fatalf("live HANDLES_ROUTE edges = %d, want 1", got)
	}
}

func seedDeployableUnitLiveRepos(t *testing.T, ctx context.Context, runner *boltRetractTestRunner, source, target string, seedTarget bool) {
	t.Helper()
	// One MERGE per statement: the backend has known multi-clause quirks, so
	// seeds stay single-clause like the other live tests in this package.
	if err := boltWriteStatement(ctx, runner,
		`MERGE (:Repository {id: $id})`,
		map[string]any{"id": source},
	); err != nil {
		t.Fatalf("seed live source repository: %v", err)
	}
	if seedTarget {
		if err := boltWriteStatement(ctx, runner,
			`MERGE (:Repository {id: $id})`,
			map[string]any{"id": target},
		); err != nil {
			t.Fatalf("seed live target repository: %v", err)
		}
	}
}

func cleanupDeployableUnitLiveRepos(t *testing.T, runner *boltRetractTestRunner, source, target string) {
	t.Helper()
	if err := boltWriteStatement(context.Background(), runner,
		`MATCH (r:Repository) WHERE r.id IN [$source, $target] DETACH DELETE r`,
		map[string]any{"source": source, "target": target},
	); err != nil {
		t.Errorf("cleanup live deployable-unit repositories: %v", err)
	}
}

func countLiveDeployableUnitEdges(t *testing.T, ctx context.Context, runner *boltRetractTestRunner, source, target string) int64 {
	t.Helper()
	n, err := boltCount(ctx, runner,
		`MATCH (:Repository {id: $source})-[r:CORRELATES_DEPLOYABLE_UNIT]->(:Repository {id: $target}) RETURN count(r) AS count`,
		map[string]any{"source": source, "target": target},
	)
	if err != nil {
		t.Fatalf("count live deployable-unit edges: %v", err)
	}
	return n
}

func liveDeployableUnitRow(source, target, intentID string) reducer.SharedProjectionIntentRow {
	return reducer.SharedProjectionIntentRow{
		IntentID:     intentID,
		RepositoryID: source,
		GenerationID: "gen-8315",
		Payload: map[string]any{
			"repo_id":             source,
			"deployment_repo_id":  target,
			"deployable_unit_key": "du-8315",
			"correlation_key":     "corr-8315",
			"confidence":          0.9,
			"reason":              "live target-miss proof",
		},
	}
}

// TestBoltWriteEdgesDeployableUnitAbsentTargetFailsClosed is the #8315 live
// regression: source Repository present, deployment Repository absent. The
// pre-write target probe must report the batch not ready (retryable error)
// instead of passing a guard that then writes nothing while reporting
// success. Runs on NornicDB and on Neo4j via the live-backend CI lanes.
func TestBoltWriteEdgesDeployableUnitAbsentTargetFailsClosed(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	const (
		source = "repository:du-target-miss-live-absent-app"
		target = "repository:du-target-miss-live-absent-deploy"
	)
	seedDeployableUnitLiveRepos(t, ctx, runner, source, target, false)
	t.Cleanup(func() { cleanupDeployableUnitLiveRepos(t, runner, source, target) })

	writer := NewEdgeWriter(&boltProbeTestExecutor{runner: runner}, 0)
	row := liveDeployableUnitRow(source, target, "du-target-miss-live-absent")
	if _, err := writer.WriteEdges(ctx, reducer.DomainDeployableUnitEdges, []reducer.SharedProjectionIntentRow{row}, "reducer/deployable-unit-correlation"); err == nil {
		t.Fatal("WriteEdges() with an absent deployment repository succeeded silently, want a retryable error")
	} else if !reducer.IsRetryable(err) {
		t.Fatalf("WriteEdges() error = %v, want retryable", err)
	}
	if got := countLiveDeployableUnitEdges(t, ctx, runner, source, target); got != 0 {
		t.Fatalf("live CORRELATES_DEPLOYABLE_UNIT edges = %d, want 0: no statement may run on a target miss", got)
	}
}

// TestBoltWriteEdgesDeployableUnitPresentTargetsWrite is the live happy path:
// with both endpoint Repositories committed, the guarded batch writes the
// edge exactly once.
func TestBoltWriteEdgesDeployableUnitPresentTargetsWrite(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	const (
		source = "repository:du-target-miss-live-present-app"
		target = "repository:du-target-miss-live-present-deploy"
	)
	seedDeployableUnitLiveRepos(t, ctx, runner, source, target, true)
	t.Cleanup(func() { cleanupDeployableUnitLiveRepos(t, runner, source, target) })

	writer := NewEdgeWriter(&boltProbeTestExecutor{runner: runner}, 0)
	row := liveDeployableUnitRow(source, target, "du-target-miss-live-present")
	if _, err := writer.WriteEdges(ctx, reducer.DomainDeployableUnitEdges, []reducer.SharedProjectionIntentRow{row}, "reducer/deployable-unit-correlation"); err != nil {
		t.Fatalf("WriteEdges() with all targets present error = %v", err)
	}
	if got := countLiveDeployableUnitEdges(t, ctx, runner, source, target); got != 1 {
		t.Fatalf("live CORRELATES_DEPLOYABLE_UNIT edges = %d, want 1", got)
	}
}

// TestBoltWriteEdgesHandlesRouteAbsentFunctionFailsClosed is the #6730
// mixed-presence pin under the #8315 single-pattern probe: Endpoint present,
// Function absent. The guard must anchor both and defer retryably instead of
// passing on the present anchor and writing nothing while reporting success.
func TestBoltWriteEdgesHandlesRouteAbsentFunctionFailsClosed(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	const (
		uid  = "content-entity:function-absent-live"
		repo = "repository:function-absent-live"
		path = "/endpoint-only"
	)
	// Seed the Endpoint only; the Function node stays absent.
	if err := boltWriteStatement(ctx, runner,
		`MERGE (:Endpoint {repo_id: $repo, path: $path})`,
		map[string]any{"repo": repo, "path": path},
	); err != nil {
		t.Fatalf("seed live endpoint node: %v", err)
	}
	t.Cleanup(func() { cleanupTargetPresenceLiveNodes(t, runner, uid, repo) })

	writer := NewEdgeWriter(&boltProbeTestExecutor{runner: runner}, 0)
	row := reducer.SharedProjectionIntentRow{
		IntentID:     "function-absent-live",
		RepositoryID: repo,
		Payload: map[string]any{
			"function_entity_id": uid,
			"repo_id":            repo,
			"path":               path,
			"http_method":        "GET",
			"framework":          "express",
			"resolution_method":  "same_file",
			"confidence":         0.95,
			"reason":             "live mixed-presence proof",
		},
	}
	if _, err := writer.WriteEdges(ctx, reducer.DomainHandlesRoute, []reducer.SharedProjectionIntentRow{row}, "parser/framework-routes"); err == nil {
		t.Fatal("WriteEdges() with an absent Function node succeeded silently, want a retryable error")
	} else if !reducer.IsRetryable(err) {
		t.Fatalf("WriteEdges() error = %v, want retryable", err)
	}
	if got := countLiveHandlesRouteEdges(t, ctx, runner, uid, repo, path); got != 0 {
		t.Fatalf("live HANDLES_ROUTE edges = %d, want 0: no statement may run on a target miss", got)
	}
}
