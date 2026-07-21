// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// reducerBoltLiveRunner is a minimal Bolt harness for proving the actual
// batchWorkloadInstanceRetractCypher statement against a real graph backend.
// It mirrors storage/cypher's boltRetractTestRunner (code_evidence_bolt_retract_test.go):
// same ESHU_CYPHER_BOLT_DSN gate, same NoAuth dial, same autocommit
// session.Run dispatch shape cmd/reducer's reducerCypherExecutor.ExecuteCypher
// uses in production (via RetryingExecutor -> cypherRunnerStatementExecutor ->
// session.Run). Its ExecuteCypher method implements this package's
// CypherExecutor interface directly, so *WorkloadMaterializer can be driven
// against a real backend with the exact same statement text and parameter
// shapes production issues — no reimplementation of DETACH DELETE semantics.
type reducerBoltLiveRunner struct {
	driver       neo4jdriver.DriverWithContext
	databaseName string
}

// openReducerBoltLiveRunner connects to the bolt DSN from ESHU_CYPHER_BOLT_DSN.
// Gate: ESHU_CYPHER_BOLT_DSN must be set (e.g. bolt://127.0.0.1:27689). When
// unset the test skips, matching the sibling live tests in storage/cypher.
func openReducerBoltLiveRunner(tb testing.TB) *reducerBoltLiveRunner {
	tb.Helper()

	dsn := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DSN"))
	if dsn == "" {
		tb.Skip("ESHU_CYPHER_BOLT_DSN not set; skipping bolt integration test")
	}
	database := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DATABASE"))
	if database == "" {
		database = "nornic"
	}

	driver, err := neo4jdriver.NewDriverWithContext(dsn, neo4jdriver.NoAuth())
	if err != nil {
		tb.Fatalf("open bolt driver %q: %v", dsn, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		tb.Fatalf("verify bolt connectivity %q: %v", dsn, err)
	}
	return &reducerBoltLiveRunner{driver: driver, databaseName: database}
}

func (r *reducerBoltLiveRunner) close(ctx context.Context) {
	_ = r.driver.Close(ctx)
}

// write executes a Cypher write statement through ExecuteWrite for seed/setup
// operations, mirroring boltWriteStatement.
func (r *reducerBoltLiveRunner) write(ctx context.Context, cypher string, params map[string]any) error {
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: r.databaseName,
	})
	defer func() { _ = session.Close(ctx) }()

	_, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		result, runErr := tx.Run(ctx, cypher, params)
		if runErr != nil {
			return nil, runErr
		}
		if _, consumeErr := result.Consume(ctx); consumeErr != nil {
			return nil, consumeErr
		}
		return nil, nil
	})
	return err
}

// ExecuteCypher implements this package's CypherExecutor interface via a
// single-statement autocommit session.Run, mirroring cmd/reducer's
// reducerCypherExecutor.ExecuteCypher / cypherRunnerStatementExecutor.Execute
// dispatch. This is the exact call shape WorkloadMaterializer uses in
// production, so passing this runner directly to NewWorkloadMaterializer
// exercises the real statement-execution contract.
func (r *reducerBoltLiveRunner) ExecuteCypher(ctx context.Context, cypher string, params map[string]any) error {
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: r.databaseName,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

// queryRows runs a read query and returns the collected rows.
func (r *reducerBoltLiveRunner) queryRows(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: r.databaseName,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		row := make(map[string]any, len(record.Keys))
		for i, key := range record.Keys {
			row[key] = record.Values[i]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (r *reducerBoltLiveRunner) count(ctx context.Context, cypher string, params map[string]any) (int64, error) {
	rows, err := r.queryRows(ctx, cypher, params)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	switch v := rows[0]["count"].(type) {
	case int64:
		return v, nil
	case float64:
		return int64(v), nil
	}
	return 0, fmt.Errorf("unexpected count type %T", rows[0]["count"])
}

func assertReducerBoltCount(
	tb testing.TB,
	ctx context.Context,
	r *reducerBoltLiveRunner,
	cypher string,
	params map[string]any,
	want int64,
	label string,
) {
	tb.Helper()
	got, err := r.count(ctx, cypher, params)
	if err != nil {
		tb.Fatalf("%s count query: %v", label, err)
	}
	if got != want {
		tb.Fatalf("%s count = %d, want %d", label, got, want)
	}
}

// TestBoltWorkloadInstanceRetractRespectsDeleteTimeOwnershipPredicate is the
// HIGH live-backend proof requested in the round-2 review of #5473. It runs
// the ACTUAL batchWorkloadInstanceRetractCypher statement (via
// WorkloadMaterializer.RetractInstances, not a reimplementation) against a
// real graph backend and proves two things a Cypher-call-recording unit test
// cannot:
//
//  1. A cross-owner race (CRITICAL 2): a retraction decision computed for
//     repoOwned must not delete the target node once a concurrent write from a
//     DIFFERENT scope has re-owned the same id by delete time — instance ids
//     are not repository-namespaced and the MERGE key is id-only, so this is
//     exactly the race the delete-time WHERE predicate closes.
//  2. The predicate is not merely present but functionally correct: retracting
//     with the CURRENT (matching) owner deletes the target node AND every one
//     of its INSTANCE_OF/DEPLOYMENT_SOURCE/RUNS_ON edges (DETACH DELETE), on a
//     backend where NornicDB has documented divergence on batched deletes.
//
// Gate: ESHU_CYPHER_BOLT_DSN must be set (e.g. bolt://127.0.0.1:27689). When
// unset the test skips.
func TestBoltWorkloadInstanceRetractRespectsDeleteTimeOwnershipPredicate(t *testing.T) {
	runner := openReducerBoltLiveRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })

	const (
		repoOwned  = "repository:5473-live-owned"
		repoOther  = "repository:5473-live-other"
		workloadID = "workload:5473-live"
		platformID = "platform:5473-live"
		staleID    = "workload-instance:5473-live:production"
	)
	ctx := context.Background()
	ids := []string{repoOwned, repoOther, workloadID, platformID, staleID}
	t.Cleanup(func() {
		if err := runner.write(
			context.Background(),
			`MATCH (n) WHERE n.id IN $ids DETACH DELETE n`,
			map[string]any{"ids": ids},
		); err != nil {
			t.Errorf("cleanup live workload-instance retract proof: %v", err)
		}
	})

	nodeSeeds := []struct {
		name   string
		cypher string
		params map[string]any
	}{
		{"owned repository", `MERGE (:Repository {id: $id})`, map[string]any{"id": repoOwned}},
		{"other repository", `MERGE (:Repository {id: $id})`, map[string]any{"id": repoOther}},
		{"workload", `MERGE (:Workload {id: $id})`, map[string]any{"id": workloadID}},
		{"platform", `MERGE (:Platform {id: $id})`, map[string]any{"id": platformID}},
		{
			"instance",
			`MERGE (i:WorkloadInstance {id: $id}) SET i.repo_id = $repo_id, i.evidence_source = $evidence_source`,
			map[string]any{"id": staleID, "repo_id": repoOwned, "evidence_source": EvidenceSourceWorkloads},
		},
	}
	for _, seed := range nodeSeeds {
		if err := runner.write(ctx, seed.cypher, seed.params); err != nil {
			t.Fatalf("seed live workload-instance retract %s node: %v", seed.name, err)
		}
	}
	edgeSeeds := []struct {
		name   string
		cypher string
		params map[string]any
	}{
		{
			"INSTANCE_OF",
			`MATCH (i:WorkloadInstance {id: $instance_id}) MATCH (w:Workload {id: $workload_id}) MERGE (i)-[:INSTANCE_OF]->(w)`,
			map[string]any{"instance_id": staleID, "workload_id": workloadID},
		},
		{
			"DEPLOYMENT_SOURCE",
			`MATCH (i:WorkloadInstance {id: $instance_id}) MATCH (r:Repository {id: $repo_id}) MERGE (i)-[:DEPLOYMENT_SOURCE]->(r)`,
			map[string]any{"instance_id": staleID, "repo_id": repoOwned},
		},
		{
			"RUNS_ON",
			`MATCH (i:WorkloadInstance {id: $instance_id}) MATCH (p:Platform {id: $platform_id}) MERGE (i)-[:RUNS_ON]->(p)`,
			map[string]any{"instance_id": staleID, "platform_id": platformID},
		},
	}
	for _, seed := range edgeSeeds {
		if err := runner.write(ctx, seed.cypher, seed.params); err != nil {
			t.Fatalf("seed live workload-instance retract %s edge: %v", seed.name, err)
		}
	}

	assertReducerBoltCount(t, ctx, runner,
		`MATCH (:WorkloadInstance {id: $id}) RETURN count(*) AS count`,
		map[string]any{"id": staleID}, 1, "seeded instance node")
	assertReducerBoltCount(t, ctx, runner,
		`MATCH (:WorkloadInstance {id: $id})-[r]-() RETURN count(r) AS count`,
		map[string]any{"id": staleID}, 3, "seeded instance edges")

	materializer := NewWorkloadMaterializer(runner)

	// --- Part 1: cross-owner race must NOT delete (CRITICAL 2) ---
	//
	// Simulate a concurrent WorkloadMaterialization pass for a DIFFERENT
	// repository re-owning the exact same instance id before this test's
	// repoOwned-scoped retract decision (already computed as `[staleID]`
	// against `repo_ids=[repoOwned]`) actually runs.
	if err := runner.write(
		ctx,
		`MATCH (i:WorkloadInstance {id: $id}) SET i.repo_id = $repo_id`,
		map[string]any{"id": staleID, "repo_id": repoOther},
	); err != nil {
		t.Fatalf("simulate concurrent re-ownership: %v", err)
	}
	if err := materializer.RetractInstances(ctx, []string{staleID}, []string{repoOwned}, EvidenceSourceWorkloads); err != nil {
		t.Fatalf("RetractInstances (stale owner) error = %v", err)
	}
	assertReducerBoltCount(t, ctx, runner,
		`MATCH (:WorkloadInstance {id: $id}) RETURN count(*) AS count`,
		map[string]any{"id": staleID}, 1, "instance node after cross-owner retract attempt")
	assertReducerBoltCount(t, ctx, runner,
		`MATCH (:WorkloadInstance {id: $id})-[r]-() RETURN count(r) AS count`,
		map[string]any{"id": staleID}, 3, "instance edges after cross-owner retract attempt")

	// --- Part 2: retracting with the CURRENT (correct) owner deletes ---
	//
	// Proves the predicate is not a permanently-false no-op: it deletes when
	// ownership genuinely matches, along with every edge type.
	if err := materializer.RetractInstances(ctx, []string{staleID}, []string{repoOther}, EvidenceSourceWorkloads); err != nil {
		t.Fatalf("RetractInstances (correct owner) error = %v", err)
	}
	assertReducerBoltCount(t, ctx, runner,
		`MATCH (:WorkloadInstance {id: $id}) RETURN count(*) AS count`,
		map[string]any{"id": staleID}, 0, "instance node after correct-owner retract")
	assertReducerBoltCount(t, ctx, runner,
		`MATCH (:WorkloadInstance {id: $id})-[r]-() RETURN count(r) AS count`,
		map[string]any{"id": staleID}, 0, "instance edges after correct-owner retract")
}
