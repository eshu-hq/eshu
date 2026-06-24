// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

const (
	liveNornicDBRetryContractEnv = "ESHU_NORNICDB_RETRY_CONTRACT_LIVE"
	liveNornicDBRetryTimeout     = 45 * time.Second
)

func TestLiveNornicDBRetryConflictClassificationContract(t *testing.T) {
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

	uid := "nornicdb-retry-contract"
	if err := executeLiveRetryWrite(ctx, driver, cfg.DatabaseName, liveRetryContractConstraintCypher, nil); err != nil {
		t.Fatalf("create retry contract constraint: %v", err)
	}
	if err := cleanupLiveRetryContractNode(ctx, driver, cfg.DatabaseName, uid); err != nil {
		t.Fatalf("clean retry contract fixture: %v", err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := cleanupLiveRetryContractNode(cleanupCtx, driver, cfg.DatabaseName, uid); err != nil {
			t.Fatalf("cleanup retry contract fixture: %v", err)
		}
	}()

	conflictErr := provokeLiveNornicDBUniqueConflict(ctx, driver, cfg.DatabaseName, uid)
	if conflictErr == nil {
		t.Fatal("live NornicDB duplicate MERGE committed without conflict")
	}
	if !isNornicDBCommitTimeUniqueConflictError(conflictErr) {
		t.Fatalf("live NornicDB conflict was not classified as commit-time UNIQUE conflict: %v", conflictErr)
	}
	if !isRetryableGraphWriteError(conflictErr, Statement{Operation: OperationCanonicalUpsert, Cypher: liveRetryContractMergeCypher}) {
		t.Fatalf("live NornicDB conflict was not retryable for MERGE statement: %v", conflictErr)
	}

	retryExecutor := &RetryingExecutor{
		Inner:      &liveRetryFailingGroupExecutor{err: conflictErr},
		MaxRetries: 1,
		BaseDelay:  1 * time.Millisecond,
	}
	err = retryExecutor.ExecuteGroup(ctx, []Statement{{
		Operation: OperationCanonicalUpsert,
		Cypher:    liveRetryContractMergeCypher,
	}})
	if err != nil {
		t.Fatalf("RetryingExecutor.ExecuteGroup() error = %v, want nil after live conflict retry", err)
	}
}

func liveNornicDBRetryContractEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(liveNornicDBRetryContractEnv))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

const liveRetryContractConstraintCypher = `
CREATE CONSTRAINT nornicdb_retry_contract_uid_unique IF NOT EXISTS
FOR (n:NornicDBRetryContract) REQUIRE n.uid IS UNIQUE`

const liveRetryContractMergeCypher = `
MERGE (n:NornicDBRetryContract {uid: $uid})
SET n.observed_by = $observed_by`

func executeLiveRetryWrite(
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	database string,
	cypher string,
	params map[string]any,
) error {
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: database,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return err
	}
	if _, err := result.Consume(ctx); err != nil {
		return err
	}
	return nil
}

func cleanupLiveRetryContractNode(
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	database string,
	uid string,
) error {
	return executeLiveRetryWrite(
		ctx,
		driver,
		database,
		"MATCH (n:NornicDBRetryContract {uid: $uid}) DETACH DELETE n",
		map[string]any{"uid": uid},
	)
}

func provokeLiveNornicDBUniqueConflict(
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	database string,
	uid string,
) error {
	sessionA := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: database})
	defer func() { _ = sessionA.Close(ctx) }()
	sessionB := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: database})
	defer func() { _ = sessionB.Close(ctx) }()

	txA, err := sessionA.BeginTransaction(ctx)
	if err != nil {
		return fmt.Errorf("begin tx a: %w", err)
	}
	defer func() { _ = txA.Rollback(ctx) }()
	txB, err := sessionB.BeginTransaction(ctx)
	if err != nil {
		return fmt.Errorf("begin tx b: %w", err)
	}
	defer func() { _ = txB.Rollback(ctx) }()

	if err := runLiveRetryMerge(ctx, txA, uid, "tx-a"); err != nil {
		return err
	}
	if err := runLiveRetryMerge(ctx, txB, uid, "tx-b"); err != nil {
		return err
	}

	errA := txA.Commit(ctx)
	errB := txB.Commit(ctx)
	switch {
	case errA != nil:
		return errA
	case errB != nil:
		return errB
	default:
		return nil
	}
}

func runLiveRetryMerge(ctx context.Context, tx neo4jdriver.ExplicitTransaction, uid string, observedBy string) error {
	result, err := tx.Run(ctx, liveRetryContractMergeCypher, map[string]any{
		"uid":         uid,
		"observed_by": observedBy,
	})
	if err != nil {
		return err
	}
	if _, err := result.Consume(ctx); err != nil {
		return err
	}
	return nil
}

type liveRetryFailingGroupExecutor struct {
	err   error
	calls int
}

func (e *liveRetryFailingGroupExecutor) Execute(context.Context, Statement) error {
	return nil
}

func (e *liveRetryFailingGroupExecutor) ExecuteGroup(context.Context, []Statement) error {
	e.calls++
	if e.calls == 1 {
		return e.err
	}
	return nil
}
