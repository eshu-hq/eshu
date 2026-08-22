// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

const (
	liveNornicDBRetryContractEnv   = "ESHU_NORNICDB_RETRY_CONTRACT_LIVE"
	liveNornicDBRetryTimeout       = 45 * time.Second
	liveNornicDBRetryBatchTailRows = 499
)

func TestRunLiveRetryWritesConcurrentlyOverlapsBothWrites(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	entered := make(chan string, 2)
	release := make(chan struct{})
	write := func(label string) func(context.Context) error {
		return func(ctx context.Context) error {
			entered <- label
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	resultsCh := make(chan [2]error, 1)
	go func() {
		resultsCh <- runLiveRetryWritesConcurrently(ctx, [2]func(context.Context) error{
			write("a"),
			write("b"),
		})
	}()

	seen := map[string]bool{}
	for range 2 {
		select {
		case label := <-entered:
			seen[label] = true
		case <-ctx.Done():
			t.Fatalf("writes did not overlap before timeout: %v", ctx.Err())
		}
	}
	close(release)
	results := <-resultsCh
	if !seen["a"] || !seen["b"] {
		t.Fatalf("entered writes = %v, want both a and b", seen)
	}
	if results[0] != nil || results[1] != nil {
		t.Fatalf("write results = %v, want both nil", results)
	}
}

func TestBuildLiveRetryContractRowsMirrorProductionPlatformBatch(t *testing.T) {
	t.Parallel()

	const wantRows = 500
	if liveNornicDBRetryBatchTailRows != wantRows-1 {
		t.Fatalf("tail rows = %d, want %d", liveNornicDBRetryBatchTailRows, wantRows-1)
	}
	sharedPlatformID := "retry-contract-unit-platform"
	rowsA := buildLiveRetryContractRows(sharedPlatformID, "tx-a")
	rowsB := buildLiveRetryContractRows(sharedPlatformID, "tx-b")
	if len(rowsA) != wantRows || len(rowsB) != wantRows {
		t.Fatalf("batch sizes = (%d, %d), want (%d, %d)", len(rowsA), len(rowsB), wantRows, wantRows)
	}
	materializerSource, err := os.ReadFile(filepath.Join("..", "..", "reducer", "workload_materializer.go"))
	if err != nil {
		t.Fatalf("read production workload materializer: %v", err)
	}
	if !strings.Contains(string(materializerSource), "const DefaultMaterializerBatchSize = 500") {
		t.Fatal("live retry batch size is not pinned to reducer.DefaultMaterializerBatchSize")
	}
	if rowsA[0]["platform_id"] != sharedPlatformID || rowsB[0]["platform_id"] != sharedPlatformID {
		t.Fatalf("shared first rows = (%v, %v), want platform_id %q", rowsA[0], rowsB[0], sharedPlatformID)
	}
	wantFields := []string{
		"instance_id", "platform_id", "platform_name", "platform_kind", "platform_provider",
		"environment", "platform_region", "platform_locator", "platform_confidence", "evidence_source",
	}
	for index := range rowsA {
		if len(rowsA[index]) != len(wantFields) || len(rowsB[index]) != len(wantFields) {
			t.Fatalf("row %d field counts = (%d, %d), want (%d, %d)",
				index, len(rowsA[index]), len(rowsB[index]), len(wantFields), len(wantFields))
		}
		for _, field := range wantFields {
			if _, ok := rowsA[index][field]; !ok {
				t.Fatalf("tx-a row %d missing production field %q: %v", index, field, rowsA[index])
			}
			if _, ok := rowsB[index][field]; !ok {
				t.Fatalf("tx-b row %d missing production field %q: %v", index, field, rowsB[index])
			}
		}
	}

	tailA := make(map[string]struct{}, liveNornicDBRetryBatchTailRows)
	for _, row := range rowsA[1:] {
		platformID, ok := row["platform_id"].(string)
		if !ok || !strings.HasPrefix(platformID, liveRetryContractTailPrefix(sharedPlatformID)+"tx-a-") {
			t.Fatalf("tx-a tail platform_id = %v, want fixture-scoped tx-a prefix", row["platform_id"])
		}
		tailA[platformID] = struct{}{}
	}
	if len(tailA) != liveNornicDBRetryBatchTailRows {
		t.Fatalf("tx-a distinct tail rows = %d, want %d", len(tailA), liveNornicDBRetryBatchTailRows)
	}
	tailB := make(map[string]struct{}, liveNornicDBRetryBatchTailRows)
	for _, row := range rowsB[1:] {
		platformID, ok := row["platform_id"].(string)
		if !ok || !strings.HasPrefix(platformID, liveRetryContractTailPrefix(sharedPlatformID)+"tx-b-") {
			t.Fatalf("tx-b tail platform_id = %v, want fixture-scoped tx-b prefix", row["platform_id"])
		}
		if _, overlaps := tailA[platformID]; overlaps {
			t.Fatalf("batch tails overlap at platform_id %q", platformID)
		}
		tailB[platformID] = struct{}{}
	}
	if len(tailB) != liveNornicDBRetryBatchTailRows {
		t.Fatalf("tx-b distinct tail rows = %d, want %d", len(tailB), liveNornicDBRetryBatchTailRows)
	}
	wantMergeCypher := `UNWIND $rows AS row
MERGE (p:Platform {id: row.platform_id})
ON CREATE SET p.evidence_source = row.evidence_source
SET p.type = 'platform',
    p.name = row.platform_name,
    p.kind = row.platform_kind,
    p.provider = row.platform_provider,
    p.environment = row.environment,
    p.region = row.platform_region,
    p.locator = row.platform_locator`
	if strings.TrimSpace(liveRetryContractMergeCypher) != wantMergeCypher {
		t.Fatalf("live retry query does not mirror batchRuntimePlatformNodeUpsertCypher:\n%s", liveRetryContractMergeCypher)
	}
	if !strings.Contains(string(materializerSource), "batchRuntimePlatformNodeUpsertCypher = `"+wantMergeCypher+"`") {
		t.Fatal("live retry query is not pinned to batchRuntimePlatformNodeUpsertCypher source text")
	}
	wantConstraintCypher := `CREATE CONSTRAINT nornicdb_retry_contract_platform_id_unique IF NOT EXISTS
FOR (p:Platform) REQUIRE p.id IS UNIQUE`
	if strings.TrimSpace(liveRetryContractConstraintCypher) != wantConstraintCypher {
		t.Fatalf("live retry constraint does not target Platform.id:\n%s", liveRetryContractConstraintCypher)
	}
	wantCleanupCypher := `MATCH (p:Platform)
WHERE p.id = $shared_platform_id OR p.id STARTS WITH $tail_platform_id_prefix
DETACH DELETE p`
	if strings.TrimSpace(liveRetryContractCleanupCypher) != wantCleanupCypher {
		t.Fatalf("live retry cleanup is not fixture-scoped to Platform.id:\n%s", liveRetryContractCleanupCypher)
	}
}

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

	platformID := "nornicdb-retry-contract-platform"
	if err := executeLiveRetryWrite(ctx, driver, cfg.DatabaseName, liveRetryContractConstraintCypher, nil); err != nil {
		t.Fatalf("create retry contract constraint: %v", err)
	}
	if err := cleanupLiveRetryContractNode(ctx, driver, cfg.DatabaseName, platformID); err != nil {
		t.Fatalf("clean retry contract fixture: %v", err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if err := cleanupLiveRetryContractNode(cleanupCtx, driver, cfg.DatabaseName, platformID); err != nil {
			t.Fatalf("cleanup retry contract fixture: %v", err)
		}
	}()

	conflictErr := provokeLiveNornicDBUniqueConflict(ctx, driver, cfg.DatabaseName, platformID)
	if conflictErr == nil {
		t.Fatal("live NornicDB duplicate MERGE committed without conflict")
	}
	if !isLiveNornicDBOnCreateCommitUniqueConflict(conflictErr, platformID) {
		t.Fatalf("live NornicDB conflict did not use the exact ON CREATE compatibility shape: %v", conflictErr)
	}
	if !isNornicDBCommitTimeUniqueConflictError(conflictErr) {
		t.Fatalf("live NornicDB conflict was not classified as commit-time UNIQUE conflict: %v", conflictErr)
	}
	if !isRetryableGraphWriteError(conflictErr, Statement{Operation: OperationCanonicalUpsert, Cypher: liveRetryContractMergeCypher}) {
		t.Fatalf("live NornicDB conflict was not retryable for MERGE statement: %v", conflictErr)
	}

	retryExecutor := &RetryingExecutor{
		Inner:      &liveRetryFailingExecutor{err: conflictErr},
		MaxRetries: 1,
		BaseDelay:  1 * time.Millisecond,
	}
	err = retryExecutor.Execute(ctx, Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    liveRetryContractMergeCypher,
	})
	if err != nil {
		t.Fatalf("RetryingExecutor.Execute() error = %v, want nil after live conflict retry", err)
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

func isLiveNornicDBOnCreateCommitUniqueConflict(err error, platformID string) bool {
	var neo4jErr *neo4jdriver.Neo4jError
	if !errors.As(err, &neo4jErr) || neo4jErr.Code != nornicDBStatementSyntaxErrorCode {
		return false
	}
	return strings.Contains(neo4jErr.Msg, "commit failed: constraint violation") &&
		strings.Contains(neo4jErr.Msg, "UNIQUE on Platform.[id]") &&
		strings.Contains(neo4jErr.Msg, "Node with id="+platformID+" already exists") &&
		isNornicDBUniqueConflictBody(neo4jErr.Msg)
}

const liveRetryContractConstraintCypher = `
CREATE CONSTRAINT nornicdb_retry_contract_platform_id_unique IF NOT EXISTS
FOR (p:Platform) REQUIRE p.id IS UNIQUE`

const liveRetryContractMergeCypher = `
UNWIND $rows AS row
MERGE (p:Platform {id: row.platform_id})
ON CREATE SET p.evidence_source = row.evidence_source
SET p.type = 'platform',
    p.name = row.platform_name,
    p.kind = row.platform_kind,
    p.provider = row.platform_provider,
    p.environment = row.environment,
    p.region = row.platform_region,
    p.locator = row.platform_locator`

func liveRetryContractTailPrefix(sharedPlatformID string) string {
	return sharedPlatformID + "-tail-"
}

func buildLiveRetryContractRows(sharedPlatformID string, writer string) []map[string]any {
	// A full production-sized cohort widens the commit-contention window. The
	// shared first row is the probe; writer-disjoint tails keep later rows from
	// introducing unrelated uniqueness conflicts.
	rows := make([]map[string]any, 0, liveNornicDBRetryBatchTailRows+1)
	rows = append(rows, liveRetryContractPlatformRow(sharedPlatformID, writer, "shared"))
	prefix := liveRetryContractTailPrefix(sharedPlatformID)
	for index := range liveNornicDBRetryBatchTailRows {
		platformID := fmt.Sprintf("%s%s-%03d", prefix, writer, index)
		rows = append(rows, liveRetryContractPlatformRow(platformID, writer, fmt.Sprintf("tail-%03d", index)))
	}
	return rows
}

func liveRetryContractPlatformRow(platformID string, writer string, suffix string) map[string]any {
	return map[string]any{
		"instance_id":         "nornicdb-retry-contract-instance-" + writer + "-" + suffix,
		"platform_id":         platformID,
		"platform_name":       "Retry contract platform " + writer,
		"platform_kind":       "kubernetes",
		"platform_provider":   "nornicdb-live-fixture",
		"environment":         "retry-contract",
		"platform_region":     "fixture-region",
		"platform_locator":    "fixture://" + platformID,
		"platform_confidence": 1.0,
		"evidence_source":     "finalization/workloads",
	}
}

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
	platformID string,
) error {
	return executeLiveRetryWrite(
		ctx,
		driver,
		database,
		liveRetryContractCleanupCypher,
		map[string]any{
			"shared_platform_id":      platformID,
			"tail_platform_id_prefix": liveRetryContractTailPrefix(platformID),
		},
	)
}

const liveRetryContractCleanupCypher = `
MATCH (p:Platform)
WHERE p.id = $shared_platform_id OR p.id STARTS WITH $tail_platform_id_prefix
DETACH DELETE p`

func runLiveRetryWritesConcurrently(
	ctx context.Context,
	writes [2]func(context.Context) error,
) [2]error {
	type indexedResult struct {
		index int
		err   error
	}

	ready := make(chan struct{}, len(writes))
	start := make(chan struct{})
	resultCh := make(chan indexedResult, len(writes))
	for index, write := range writes {
		go func() {
			ready <- struct{}{}
			select {
			case <-start:
				resultCh <- indexedResult{index: index, err: write(ctx)}
			case <-ctx.Done():
				resultCh <- indexedResult{index: index, err: ctx.Err()}
			}
		}()
	}
	for range writes {
		select {
		case <-ready:
		case <-ctx.Done():
			close(start)
			return [2]error{ctx.Err(), ctx.Err()}
		}
	}
	close(start)

	var results [2]error
	for range writes {
		select {
		case result := <-resultCh:
			results[result.index] = result.err
		case <-ctx.Done():
			return [2]error{ctx.Err(), ctx.Err()}
		}
	}
	return results
}

func provokeLiveNornicDBUniqueConflict(
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	database string,
	platformID string,
) error {
	observedBy := [2]string{"tx-a", "tx-b"}
	var writes [2]func(context.Context) error
	for index := range writes {
		writer := observedBy[index]
		writes[index] = func(writeCtx context.Context) error {
			return executeLiveRetryWrite(writeCtx, driver, database, liveRetryContractMergeCypher, map[string]any{
				"rows": buildLiveRetryContractRows(platformID, writer),
			})
		}
	}
	results := runLiveRetryWritesConcurrently(ctx, writes)
	for _, err := range results {
		if isLiveNornicDBOnCreateCommitUniqueConflict(err, platformID) {
			return err
		}
	}
	for index, err := range results {
		if err != nil {
			return fmt.Errorf("run implicit tx %s: %w", observedBy[index], err)
		}
	}
	return nil
}

type liveRetryFailingExecutor struct {
	err   error
	calls int
}

func (e *liveRetryFailingExecutor) Execute(context.Context, Statement) error {
	e.calls++
	if e.calls == 1 {
		return e.err
	}
	return nil
}
