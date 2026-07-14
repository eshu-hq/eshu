// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifarepodependencyproof

package offlinetier_test

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	osexec "os/exec"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	repoDependencyProcessDeathHelperEnv = "ESHU_TEST_REPO_DEPENDENCY_PROCESS_DEATH_HELPER"
	repoDependencyProcessDeathDSNEnv    = "ESHU_TEST_REPO_DEPENDENCY_PROCESS_DEATH_DSN"
	repoDependencyProcessDeathSentinel  = "REPO_DEPENDENCY_GRAPH_COMMITTED"
)

func TestRepoDependencyIfaProcessDeathLive(t *testing.T) {
	if os.Getenv(repoDependencyProcessDeathHelperEnv) == "1" {
		runRepoDependencyProcessDeathHelper(t)
		return
	}
	if !repoDependencyConcurrencyProofEnabled(t) {
		return
	}
	baseDSN := firstRepoDependencyQuarantineDSN()
	if baseDSN == "" {
		t.Skipf("set %s or ESHU_POSTGRES_DSN to run the process-death proof", repoDependencyQuarantineProofDSNEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	graphExec, _ := openDeltaLiveBackend(ctx, t)
	odu := mustRepoDependencyIfaOdu(t)
	row := repoDependencyProcessDeathRow(t, repoDependencyIfaRows(t, odu))
	rows := []reducer.SharedProjectionIntentRow{row}
	artifactIDs := repoDependencyIfaArtifactIDs(t, rows)
	acquireRepoDependencyIfaExclusiveBackend(ctx, t, graphExec, artifactIDs)

	db, scopedDSN, cleanupDB := openRepoDependencyProcessDeathProofDB(ctx, t, baseDSN)
	defer cleanupDB()
	store := postgres.NewSharedIntentStore(postgres.SQLDB{DB: db})
	gate := postgres.NewRepoDependencyAcceptanceUnitGate(postgres.SQLDB{DB: db})
	baseWriter := cypher.NewEdgeWriter(&cypher.RetryingExecutor{Inner: graphExec}, 0)
	prepareRepoDependencyQuarantinePhase(ctx, t, db, store, graphExec, odu, artifactIDs, rows)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		cleanupRepoDependencyConcurrencyScope(cleanupCtx, t, graphExec, artifactIDs)
		assertRepoDependencyIfaCleanup(cleanupCtx, t, graphExec, artifactIDs)
	})

	command := osexec.Command(os.Args[0], "-test.run=^TestRepoDependencyIfaProcessDeathLive$")
	command.Env = append(
		os.Environ(),
		repoDependencyProcessDeathHelperEnv+"=1",
		repoDependencyProcessDeathDSNEnv+"="+scopedDSN,
	)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("open process-death helper stdout: %v", err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start process-death helper: %v", err)
	}
	sentinel := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == repoDependencyProcessDeathSentinel {
				close(sentinel)
				return
			}
		}
	}()
	select {
	case <-sentinel:
	case <-time.After(20 * time.Second):
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("helper did not reach committed graph write: %s", stderr.String())
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatalf("SIGKILL process-death helper: %v", err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("process-death helper exited cleanly, want SIGKILL")
	}

	if got := repoDependencyQuarantinePendingCount(ctx, t, db); got != 1 {
		t.Fatalf("pending intents after SIGKILL = %d, want 1", got)
	}
	postKill := readRepoDependencyIfaSnapshot(ctx, t, graphExec, artifactIDs)
	if !repoDependencyQuarantineContainsFaultEdge(postKill, rows, row.AcceptanceUnitID) {
		t.Fatal("graph commit was not visible after helper SIGKILL")
	}
	lease := readRepoDependencyQuarantinedLease(ctx, t, db)
	claimed, err := store.ClaimPartitionLease(
		ctx, reducer.DomainRepoDependency, 0, 1, "process-death-owner-b", 36*time.Second,
	)
	if err != nil {
		t.Fatalf("claim before killed owner's lease expiry: %v", err)
	}
	if claimed {
		t.Fatal("new owner claimed killed process shard before lease expiry")
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE shared_projection_partition_leases
		SET lease_expires_at = CURRENT_TIMESTAMP - INTERVAL '1 second'
		WHERE projection_domain = $1
		  AND partition_id = $2
		  AND partition_count = $3
		  AND lease_owner = $4
	`, reducer.DomainRepoDependency, lease.partitionID, lease.partitionCount, lease.owner); err != nil {
		t.Fatalf("expire killed process lease: %v", err)
	}

	runRepoDependencyQuarantineUntil(
		ctx,
		t,
		store,
		gate,
		baseWriter,
		repoDependencyIfaAcceptedGeneration(rows),
		"process-death-owner-b",
		1,
		func() bool { return repoDependencyQuarantinePendingCount(ctx, t, db) == 0 },
	)
	finalSnapshot := readRepoDependencyIfaSnapshot(ctx, t, graphExec, artifactIDs)
	assertRepoDependencyIfaSnapshot(t, finalSnapshot, repoDependencyIfaExpectedEdges(rows))
	missing, extra := bidirectionalStringDiff(postKill.canonical, finalSnapshot.canonical)
	if len(missing) != 0 || len(extra) != 0 {
		t.Fatalf("post-SIGKILL replay graph diff=%d/%d missing=%v extra=%v", len(missing), len(extra), missing, extra)
	}
	t.Logf("SIGKILL pending=1 pre_expiry_claim=false final_diff=0/0 owner=%s", lease.owner)
}

func runRepoDependencyProcessDeathHelper(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	graphExec, _ := openDeltaLiveBackend(ctx, t)
	row := repoDependencyProcessDeathRow(t, repoDependencyIfaRows(t, mustRepoDependencyIfaOdu(t)))
	db, err := sql.Open("pgx", os.Getenv(repoDependencyProcessDeathDSNEnv))
	if err != nil {
		t.Fatalf("open process-death helper database: %v", err)
	}
	defer func() { _ = db.Close() }()
	store := postgres.NewSharedIntentStore(postgres.SQLDB{DB: db})
	gate := postgres.NewRepoDependencyAcceptanceUnitGate(postgres.SQLDB{DB: db})
	writer := &repoDependencyProcessDeathWriter{
		inner: cypher.NewEdgeWriter(&cypher.RetryingExecutor{Inner: graphExec}, 0),
	}
	done := startRepoDependencyQuarantineRunner(
		ctx,
		store,
		gate,
		writer,
		repoDependencyIfaAcceptedGeneration([]reducer.SharedProjectionIntentRow{row}),
		"process-death-owner-a",
		1,
	)
	if err := <-done; err != nil {
		t.Fatalf("process-death helper runner: %v", err)
	}
}

type repoDependencyProcessDeathWriter struct {
	inner reducer.SharedProjectionEdgeWriter
}

func (w *repoDependencyProcessDeathWriter) RetractEdges(
	ctx context.Context, domain string, rows []reducer.SharedProjectionIntentRow, evidenceSource string,
) error {
	return w.inner.RetractEdges(ctx, domain, rows, evidenceSource)
}

func (w *repoDependencyProcessDeathWriter) WriteEdges(
	ctx context.Context, domain string, rows []reducer.SharedProjectionIntentRow, evidenceSource string,
) error {
	if err := w.inner.WriteEdges(ctx, domain, rows, evidenceSource); err != nil {
		return err
	}
	fmt.Println(repoDependencyProcessDeathSentinel)
	_ = os.Stdout.Sync()
	select {}
}

func repoDependencyProcessDeathRow(
	t *testing.T, rows []reducer.SharedProjectionIntentRow,
) reducer.SharedProjectionIntentRow {
	t.Helper()
	for _, row := range rows {
		key, ok := row.AcceptanceKey()
		if ok && key.AcceptanceUnitID == "repository:source-05" {
			return row
		}
	}
	t.Fatal("Odù is missing repository:source-05 process-death row")
	return reducer.SharedProjectionIntentRow{}
}

func openRepoDependencyProcessDeathProofDB(
	ctx context.Context, t *testing.T, dsn string,
) (*sql.DB, string, func()) {
	t.Helper()
	bootstrapDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open process-death bootstrap database: %v", err)
	}
	schemaName := fmt.Sprintf("repo_dependency_process_death_%d", time.Now().UnixNano())
	if _, err := bootstrapDB.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		_ = bootstrapDB.Close()
		t.Fatalf("create process-death schema: %v", err)
	}
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	scopedDSN := dsn + separator + "search_path=" + schemaName
	db, err := sql.Open("pgx", scopedDSN)
	if err != nil {
		_ = bootstrapDB.Close()
		t.Fatalf("open scoped process-death database: %v", err)
	}
	if err := postgres.NewSharedIntentStore(postgres.SQLDB{DB: db}).EnsureSchema(ctx); err != nil {
		_ = db.Close()
		_ = bootstrapDB.Close()
		t.Fatalf("ensure process-death schema: %v", err)
	}
	return db, scopedDSN, func() {
		_ = db.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = bootstrapDB.ExecContext(cleanupCtx, "DROP SCHEMA "+schemaName+" CASCADE")
		_ = bootstrapDB.Close()
	}
}
