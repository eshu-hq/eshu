// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"os"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

func TestLiveNornicDBRelationshipSnapshotConflictRetryContract(t *testing.T) {
	if !liveNornicDBRetryContractEnabled() {
		t.Skipf("set %s=1 to run live NornicDB retry classification contract", liveNornicDBRetryContractEnv)
	}

	backend, err := runtimecfg.LoadGraphBackend(os.Getenv)
	if err != nil {
		t.Fatalf("load graph backend: %v", err)
	}
	if backend != runtimecfg.GraphBackendNornicDB {
		t.Skipf("%s only runs against NornicDB, got %q", liveNornicDBRetryContractEnv, backend)
	}

	ctx, cancel := context.WithTimeout(context.Background(), liveNornicDBRetryTimeout)
	defer cancel()

	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, os.Getenv)
	if err != nil {
		t.Fatalf("open Bolt driver: %v", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		if err := driver.Close(closeCtx); err != nil {
			t.Fatalf("close Bolt driver: %v", err)
		}
	}()

	fixture := liveRelationshipSnapshotFixture{
		digest:   "sha256:5767000000000000000000000000000000000000000000000000000000000000",
		imageUID: "container-image-5767-synthetic",
		repoID:   "repository-5767-synthetic",
		repoPath: "registry.example.com/example/repository",
	}
	if err := cleanupLiveRelationshipSnapshotFixture(ctx, driver, cfg.DatabaseName, fixture); err != nil {
		t.Fatalf("clean relationship snapshot fixture before test: %v", err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := cleanupLiveRelationshipSnapshotFixture(cleanupCtx, driver, cfg.DatabaseName, fixture); err != nil {
			t.Fatalf("clean relationship snapshot fixture after test: %v", err)
		}
	}()
	if err := seedLiveRelationshipSnapshotEndpoints(ctx, driver, cfg.DatabaseName, fixture); err != nil {
		t.Fatalf("seed relationship snapshot endpoints: %v", err)
	}

	staleSession := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: cfg.DatabaseName,
	})
	defer func() { _ = staleSession.Close(ctx) }()
	staleTx, err := staleSession.BeginTransaction(ctx)
	if err != nil {
		t.Fatalf("begin stale relationship transaction: %v", err)
	}
	defer func() { _ = staleTx.Rollback(ctx) }()
	if err := pinLiveRelationshipSnapshot(ctx, staleTx, fixture.digest); err != nil {
		t.Fatalf("pin stale relationship snapshot: %v", err)
	}

	if err := executeLiveRetryWrite(
		ctx,
		driver,
		cfg.DatabaseName,
		liveRelationshipSnapshotMergeCypher,
		fixture.params("scope-winner"),
	); err != nil {
		t.Fatalf("commit winning relationship: %v", err)
	}

	conflictErr := runLiveRelationshipSnapshotMerge(ctx, staleTx, fixture.params("scope-stale"))
	if conflictErr == nil {
		conflictErr = staleTx.Commit(ctx)
	}
	if conflictErr == nil {
		t.Fatal("stale relationship transaction succeeded, want snapshot conflict")
	}
	if got := classifyRetryableGraphWriteGroupError(
		conflictErr,
		[]Statement{fixture.statement("scope-retry")},
	); got != graphWriteRetryReasonWriteConflict {
		t.Fatalf("retry reason = %q, want %q; error: %v", got, graphWriteRetryReasonWriteConflict, conflictErr)
	}
	if err := staleTx.Rollback(ctx); err != nil {
		t.Fatalf("rollback stale relationship transaction: %v", err)
	}

	inner := &liveRelationshipSnapshotReplayExecutor{
		conflictErr: conflictErr,
		driver:      driver,
		database:    cfg.DatabaseName,
	}
	retrying := &RetryingExecutor{
		Inner:      inner,
		MaxRetries: 1,
		BaseDelay:  time.Millisecond,
	}
	if err := retrying.ExecuteGroup(
		ctx,
		[]Statement{fixture.statement("scope-retry")},
	); err != nil {
		t.Fatalf("retry relationship snapshot conflict: %v", err)
	}
	if got, want := inner.calls, 2; got != want {
		t.Fatalf("ExecuteGroup() calls = %d, want %d", got, want)
	}

	scopes, err := readLiveRelationshipSnapshotScopes(ctx, driver, cfg.DatabaseName, fixture)
	if err != nil {
		t.Fatalf("read retried relationship: %v", err)
	}
	if got, want := len(scopes), 1; got != want {
		t.Fatalf("BUILT_FROM edge count = %d, want %d; scopes=%v", got, want, scopes)
	}
	if got, want := scopes[0], "scope-retry"; got != want {
		t.Fatalf("BUILT_FROM scope_id = %q, want %q", got, want)
	}
}

const liveRelationshipSnapshotMergeCypher = `
UNWIND $rows AS row
MATCH (img:ContainerImage {digest: row.digest})
MATCH (repo:Repository {id: row.repository_id})
MERGE (img)-[rel:BUILT_FROM]->(repo)
SET rel.scope_id = row.scope_id`

type liveRelationshipSnapshotFixture struct {
	digest   string
	imageUID string
	repoID   string
	repoPath string
}

func (f liveRelationshipSnapshotFixture) params(scopeID string) map[string]any {
	return map[string]any{
		"rows": []map[string]any{{
			"digest":        f.digest,
			"repository_id": f.repoID,
			"scope_id":      scopeID,
		}},
	}
}

func (f liveRelationshipSnapshotFixture) statement(scopeID string) Statement {
	return Statement{
		Operation:  OperationCanonicalUpsert,
		Cypher:     liveRelationshipSnapshotMergeCypher,
		Parameters: f.params(scopeID),
	}
}

func seedLiveRelationshipSnapshotEndpoints(
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	database string,
	fixture liveRelationshipSnapshotFixture,
) error {
	if err := executeLiveRetryWrite(
		ctx,
		driver,
		database,
		"MERGE (img:ContainerImage {digest: $digest}) SET img.uid = $uid",
		map[string]any{"digest": fixture.digest, "uid": fixture.imageUID},
	); err != nil {
		return err
	}
	return executeLiveRetryWrite(
		ctx,
		driver,
		database,
		"MERGE (repo:Repository {id: $repo_id}) SET repo.path = $repo_path",
		map[string]any{"repo_id": fixture.repoID, "repo_path": fixture.repoPath},
	)
}

func cleanupLiveRelationshipSnapshotFixture(
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	database string,
	fixture liveRelationshipSnapshotFixture,
) error {
	if err := executeLiveRetryWrite(
		ctx,
		driver,
		database,
		"MATCH (img:ContainerImage {digest: $digest}) DETACH DELETE img",
		map[string]any{"digest": fixture.digest},
	); err != nil {
		return err
	}
	return executeLiveRetryWrite(
		ctx,
		driver,
		database,
		"MATCH (repo:Repository {id: $repo_id}) DETACH DELETE repo",
		map[string]any{"repo_id": fixture.repoID},
	)
}

func pinLiveRelationshipSnapshot(
	ctx context.Context,
	tx neo4jdriver.ExplicitTransaction,
	digest string,
) error {
	result, err := tx.Run(
		ctx,
		"MATCH (img:ContainerImage {digest: $digest}) RETURN img.digest AS digest",
		map[string]any{"digest": digest},
	)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

func runLiveRelationshipSnapshotMerge(
	ctx context.Context,
	tx neo4jdriver.ExplicitTransaction,
	params map[string]any,
) error {
	result, err := tx.Run(ctx, liveRelationshipSnapshotMergeCypher, params)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

type liveRelationshipSnapshotReplayExecutor struct {
	conflictErr error
	driver      neo4jdriver.DriverWithContext
	database    string
	calls       int
}

func (e *liveRelationshipSnapshotReplayExecutor) Execute(
	context.Context,
	Statement,
) error {
	return nil
}

func (e *liveRelationshipSnapshotReplayExecutor) ExecuteGroup(
	ctx context.Context,
	statements []Statement,
) error {
	e.calls++
	if e.calls == 1 {
		return e.conflictErr
	}
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.database,
	})
	defer func() { _ = session.Close(ctx) }()
	_, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		for _, statement := range statements {
			result, runErr := tx.Run(ctx, statement.Cypher, statement.Parameters)
			if runErr != nil {
				return nil, runErr
			}
			if _, consumeErr := result.Consume(ctx); consumeErr != nil {
				return nil, consumeErr
			}
		}
		return nil, nil
	})
	return err
}

func readLiveRelationshipSnapshotScopes(
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	database string,
	fixture liveRelationshipSnapshotFixture,
) ([]string, error) {
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeRead,
		DatabaseName: database,
	})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(
		ctx,
		`MATCH (:ContainerImage {digest: $digest})-[rel:BUILT_FROM]->(:Repository {id: $repo_id})
RETURN rel.scope_id AS scope_id`,
		map[string]any{"digest": fixture.digest, "repo_id": fixture.repoID},
	)
	if err != nil {
		return nil, err
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, err
	}
	scopes := make([]string, 0, len(records))
	for _, record := range records {
		scope, _ := record.Get("scope_id")
		scopeString, _ := scope.(string)
		scopes = append(scopes, scopeString)
	}
	return scopes, nil
}
