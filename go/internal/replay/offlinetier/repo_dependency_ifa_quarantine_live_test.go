// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifarepodependencyproof

package offlinetier_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const repoDependencyQuarantineProofDSNEnv = "ESHU_REPO_DEPENDENCY_QUARANTINE_PROOF_DSN"

var errRepoDependencyGraphResponseLost = errors.New("injected post-commit graph response loss")

func TestRepoDependencyIfaQuarantineLive(t *testing.T) {
	if !repoDependencyConcurrencyProofEnabled(t) {
		return
	}
	dsn := firstRepoDependencyQuarantineDSN()
	if dsn == "" {
		t.Skipf("set %s or ESHU_POSTGRES_DSN to run the real-Postgres quarantine proof", repoDependencyQuarantineProofDSNEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	exec, _ := openDeltaLiveBackend(ctx, t)
	odu := mustRepoDependencyIfaOdu(t)
	rows := repoDependencyIfaRows(t, odu)
	expectedEdges := repoDependencyIfaExpectedEdges(rows)
	artifactIDs := repoDependencyIfaArtifactIDs(t, rows)
	acceptedGeneration := repoDependencyIfaAcceptedGeneration(rows)
	acquireRepoDependencyIfaExclusiveBackend(ctx, t, exec, artifactIDs)

	db, cleanupDB := openRepoDependencyQuarantineProofDB(ctx, t, dsn)
	defer cleanupDB()
	store := postgres.NewSharedIntentStore(postgres.SQLDB{DB: db})
	gate := postgres.NewRepoDependencyAcceptanceUnitGate(postgres.SQLDB{DB: db})

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		cleanupRepoDependencyConcurrencyScope(cleanupCtx, t, exec, artifactIDs)
		assertRepoDependencyIfaCleanup(cleanupCtx, t, exec, artifactIDs)
	})

	baseWriter := cypher.NewEdgeWriter(&cypher.RetryingExecutor{Inner: exec}, 0)
	prepareRepoDependencyQuarantinePhase(ctx, t, db, store, exec, odu, artifactIDs, rows)
	runRepoDependencyQuarantineUntil(ctx, t, store, gate, baseWriter, acceptedGeneration, "baseline", 1, func() bool {
		return repoDependencyQuarantinePendingCount(ctx, t, db) == 0
	})
	baseline := readRepoDependencyIfaSnapshot(ctx, t, exec, artifactIDs)
	assertRepoDependencyIfaSnapshot(t, baseline, expectedEdges)

	prepareRepoDependencyQuarantinePhase(ctx, t, db, store, exec, odu, artifactIDs, rows)
	const faultAcceptanceUnit = "repository:source-05"
	faultShard := repoDependencyQuarantineShard(faultAcceptanceUnit, 4)
	overlapWriter := &repoDependencyOverlapWriter{inner: baseWriter, delay: 250 * time.Millisecond}
	faultWriter := newRepoDependencyLostResponseWriter(overlapWriter, faultAcceptanceUnit)

	runCtx, stopRun := context.WithCancel(ctx)
	runDone := startRepoDependencyQuarantineRunner(
		runCtx,
		store,
		gate,
		faultWriter,
		acceptedGeneration,
		"odu-quarantine-owner-a",
		4,
	)
	waitRepoDependencyQuarantineCondition(ctx, t, "post-commit loss and disjoint shard drain", func() bool {
		if faultWriter.failureCount() != 1 {
			return false
		}
		state := repoDependencyQuarantineCompletionState(ctx, t, db)
		if state[faultAcceptanceUnit] {
			return false
		}
		for acceptanceUnitID, completed := range state {
			if repoDependencyQuarantineShard(acceptanceUnitID, 4) != faultShard && !completed {
				return false
			}
		}
		return len(state) == len(rows) && repoDependencyQuarantineOnlyActiveLease(ctx, t, db, faultShard)
	})
	stopRun()
	assertRepoDependencyQuarantineRunnerStopped(t, runDone)

	if got := overlapWriter.maxConcurrent(); got < 4 {
		t.Fatalf("max concurrent graph writes=%d, want >=4", got)
	}
	lease := readRepoDependencyQuarantinedLease(ctx, t, db)
	if lease.partitionID != faultShard || lease.partitionCount != 4 {
		t.Fatalf("quarantined lease partition=%d-of-%d, want %d-of-4", lease.partitionID, lease.partitionCount, faultShard)
	}
	if !strings.HasPrefix(lease.owner, "odu-quarantine-owner-a/") {
		t.Fatalf("quarantined lease owner=%q, want owner A worker", lease.owner)
	}
	if !lease.expiresAt.After(time.Now().UTC()) {
		t.Fatalf("quarantined lease expiry=%s, want future expiry", lease.expiresAt)
	}
	if err := store.ReleasePartitionLease(
		ctx,
		reducer.DomainRepoDependency,
		lease.partitionID,
		lease.partitionCount,
		"odu-quarantine-owner-b/worker-0-of-4",
	); err != nil {
		t.Fatalf("wrong-owner release probe: %v", err)
	}
	if got := readRepoDependencyQuarantinedLease(ctx, t, db).owner; got != lease.owner {
		t.Fatalf("wrong-owner release changed lease owner from %q to %q", lease.owner, got)
	}
	postFault := readRepoDependencyIfaSnapshot(ctx, t, exec, artifactIDs)
	if !repoDependencyQuarantineContainsFaultEdge(postFault, rows, faultAcceptanceUnit) {
		t.Fatalf("fault acceptance unit %q graph write was not committed before response loss", faultAcceptanceUnit)
	}
	pendingAfterFault := repoDependencyQuarantinePendingCount(ctx, t, db)

	if _, err := db.ExecContext(ctx, `
		UPDATE shared_projection_partition_leases
		SET lease_expires_at = CURRENT_TIMESTAMP - INTERVAL '1 second'
		WHERE projection_domain = $1
		  AND partition_id = $2
		  AND partition_count = $3
		  AND lease_owner = $4
	`, reducer.DomainRepoDependency, lease.partitionID, lease.partitionCount, lease.owner); err != nil {
		t.Fatalf("expire quarantined test lease: %v", err)
	}

	runRepoDependencyQuarantineUntil(ctx, t, store, gate, baseWriter, acceptedGeneration, "odu-quarantine-owner-b", 4, func() bool {
		return repoDependencyQuarantinePendingCount(ctx, t, db) == 0
	})
	finalSnapshot := readRepoDependencyIfaSnapshot(ctx, t, exec, artifactIDs)
	assertRepoDependencyIfaSnapshot(t, finalSnapshot, expectedEdges)
	missing, extra := bidirectionalStringDiff(baseline.canonical, finalSnapshot.canonical)
	if len(missing) != 0 || len(extra) != 0 {
		t.Fatalf("post-quarantine graph diff=%d/%d missing=%v extra=%v", len(missing), len(extra), missing, extra)
	}

	if err := store.UpsertIntents(ctx, rows); err != nil {
		t.Fatalf("replay completed Odù intents: %v", err)
	}
	if got := repoDependencyQuarantinePendingCount(ctx, t, db); got != 0 {
		t.Fatalf("duplicate replay reopened %d completed intents", got)
	}
	afterDuplicate := readRepoDependencyIfaSnapshot(ctx, t, exec, artifactIDs)
	missing, extra = bidirectionalStringDiff(finalSnapshot.canonical, afterDuplicate.canonical)
	if len(missing) != 0 || len(extra) != 0 {
		t.Fatalf("duplicate replay graph diff=%d/%d missing=%v extra=%v", len(missing), len(extra), missing, extra)
	}
	t.Logf(
		"workers=4 fault_shard=%d max_concurrent_writes=%d pending_after_fault=%d final_diff=0/0 duplicate_diff=0/0",
		faultShard,
		overlapWriter.maxConcurrent(),
		pendingAfterFault,
	)
}

type repoDependencyLostResponseWriter struct {
	inner                reducer.SharedProjectionEdgeWriter
	faultAcceptanceUnit  string
	injectedFailureCount atomic.Int32
	once                 sync.Once
}

func newRepoDependencyLostResponseWriter(inner reducer.SharedProjectionEdgeWriter, faultAcceptanceUnit string) *repoDependencyLostResponseWriter {
	return &repoDependencyLostResponseWriter{inner: inner, faultAcceptanceUnit: faultAcceptanceUnit}
}

func (w *repoDependencyLostResponseWriter) RetractEdges(ctx context.Context, domain string, rows []reducer.SharedProjectionIntentRow, evidenceSource string) error {
	return w.inner.RetractEdges(ctx, domain, rows, evidenceSource)
}

func (w *repoDependencyLostResponseWriter) WriteEdges(ctx context.Context, domain string, rows []reducer.SharedProjectionIntentRow, evidenceSource string) error {
	if err := w.inner.WriteEdges(ctx, domain, rows, evidenceSource); err != nil {
		return err
	}
	for _, row := range rows {
		if row.AcceptanceUnitID != w.faultAcceptanceUnit {
			continue
		}
		injected := false
		w.once.Do(func() {
			w.injectedFailureCount.Add(1)
			injected = true
		})
		if injected {
			return errRepoDependencyGraphResponseLost
		}
	}
	return nil
}

func (w *repoDependencyLostResponseWriter) failureCount() int {
	return int(w.injectedFailureCount.Load())
}

type repoDependencyQuarantinedLease struct {
	partitionID    int
	partitionCount int
	owner          string
	expiresAt      time.Time
}

func firstRepoDependencyQuarantineDSN() string {
	for _, name := range []string{repoDependencyQuarantineProofDSNEnv, "ESHU_SHARED_PROJECTION_RESCALE_PROOF_DSN", "ESHU_POSTGRES_DSN"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func openRepoDependencyQuarantineProofDB(ctx context.Context, t *testing.T, dsn string) (*sql.DB, func()) {
	t.Helper()
	bootstrapDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open quarantine proof bootstrap database: %v", err)
	}
	schemaName := fmt.Sprintf("repo_dependency_odu_quarantine_%d", time.Now().UnixNano())
	if _, err := bootstrapDB.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		_ = bootstrapDB.Close()
		t.Fatalf("create quarantine proof schema: %v", err)
	}
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	db, err := sql.Open("pgx", dsn+separator+"search_path="+schemaName)
	if err != nil {
		_ = bootstrapDB.Close()
		t.Fatalf("open scoped quarantine proof database: %v", err)
	}
	if err := postgres.NewSharedIntentStore(postgres.SQLDB{DB: db}).EnsureSchema(ctx); err != nil {
		_ = db.Close()
		_ = bootstrapDB.Close()
		t.Fatalf("ensure quarantine proof schema: %v", err)
	}
	return db, func() {
		_ = db.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = bootstrapDB.ExecContext(cleanupCtx, "DROP SCHEMA "+schemaName+" CASCADE")
		_ = bootstrapDB.Close()
	}
}

func prepareRepoDependencyQuarantinePhase(ctx context.Context, t *testing.T, db *sql.DB, store *postgres.SharedIntentStore, exec liveExecutor, odu ifa.Odu, artifactIDs []string, rows []reducer.SharedProjectionIntentRow) {
	t.Helper()
	if _, err := db.ExecContext(ctx, "TRUNCATE shared_projection_intents, shared_projection_partition_leases"); err != nil {
		t.Fatalf("reset quarantine proof tables: %v", err)
	}
	cleanupRepoDependencyConcurrencyScope(ctx, t, exec, artifactIDs)
	assertRepoDependencyIfaCleanup(ctx, t, exec, artifactIDs)
	seedRepoDependencyIfaRepositories(ctx, t, exec, odu)
	if err := store.UpsertIntents(ctx, rows); err != nil {
		t.Fatalf("seed quarantine proof intents: %v", err)
	}
}

func repoDependencyIfaAcceptedGeneration(rows []reducer.SharedProjectionIntentRow) reducer.AcceptedGenerationLookup {
	accepted := make(map[reducer.SharedProjectionAcceptanceKey]string, len(rows))
	for _, row := range rows {
		if key, ok := row.AcceptanceKey(); ok {
			accepted[key] = row.GenerationID
		}
	}
	return func(key reducer.SharedProjectionAcceptanceKey) (string, bool) {
		generationID, ok := accepted[key]
		return generationID, ok
	}
}

func startRepoDependencyQuarantineRunner(
	ctx context.Context,
	store *postgres.SharedIntentStore,
	gate *postgres.RepoDependencyAcceptanceUnitGate,
	writer reducer.SharedProjectionEdgeWriter,
	acceptedGeneration reducer.AcceptedGenerationLookup,
	owner string,
	workers int,
) <-chan error {
	runner := reducer.RepoDependencyProjectionRunner{
		IntentReader:       store,
		LeaseManager:       store,
		AcceptanceUnitGate: gate,
		EdgeWriter:         writer,
		AcceptedGen:        acceptedGeneration,
		Config: reducer.RepoDependencyProjectionRunnerConfig{
			LeaseOwner:            owner,
			PollInterval:          time.Second,
			LeaseTTL:              36 * time.Second,
			CycleTimeout:          5 * time.Second,
			GraphQuiescenceBudget: time.Millisecond,
			BatchLimit:            100,
			Workers:               workers,
		},
	}
	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx)
	}()
	return done
}

func runRepoDependencyQuarantineUntil(
	ctx context.Context,
	t *testing.T,
	store *postgres.SharedIntentStore,
	gate *postgres.RepoDependencyAcceptanceUnitGate,
	writer reducer.SharedProjectionEdgeWriter,
	acceptedGeneration reducer.AcceptedGenerationLookup,
	owner string,
	workers int,
	condition func() bool,
) {
	t.Helper()
	runCtx, cancel := context.WithCancel(ctx)
	done := startRepoDependencyQuarantineRunner(runCtx, store, gate, writer, acceptedGeneration, owner, workers)
	waitRepoDependencyQuarantineCondition(ctx, t, owner, condition)
	cancel()
	assertRepoDependencyQuarantineRunnerStopped(t, done)
}

func assertRepoDependencyQuarantineRunnerStopped(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("repo dependency quarantine runner: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("repo dependency quarantine runner did not stop after cancellation")
	}
}

func waitRepoDependencyQuarantineCondition(
	ctx context.Context,
	t *testing.T,
	description string,
	condition func() bool,
) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if condition() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", description)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for %s: %v", description, ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func repoDependencyQuarantinePendingCount(ctx context.Context, t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM shared_projection_intents WHERE completed_at IS NULL
	`).Scan(&count); err != nil {
		t.Fatalf("count pending quarantine proof intents: %v", err)
	}
	return count
}

func repoDependencyQuarantineOnlyActiveLease(ctx context.Context, t *testing.T, db *sql.DB, partitionID int) bool {
	t.Helper()
	var active, matching int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*), count(*) FILTER (WHERE partition_id = $2)
		FROM shared_projection_partition_leases
		WHERE projection_domain = $1
		  AND lease_owner IS NOT NULL
		  AND lease_expires_at > CURRENT_TIMESTAMP
	`, reducer.DomainRepoDependency, partitionID).Scan(&active, &matching); err != nil {
		t.Fatalf("count active quarantine proof leases: %v", err)
	}
	return active == 1 && matching == 1
}

func repoDependencyQuarantineCompletionState(ctx context.Context, t *testing.T, db *sql.DB) map[string]bool {
	t.Helper()
	result, err := db.QueryContext(ctx, `
		SELECT acceptance_unit_id, bool_and(completed_at IS NOT NULL)
		FROM shared_projection_intents
		GROUP BY acceptance_unit_id
	`)
	if err != nil {
		t.Fatalf("read quarantine proof completion state: %v", err)
	}
	defer func() { _ = result.Close() }()
	state := make(map[string]bool)
	for result.Next() {
		var acceptanceUnitID string
		var completed bool
		if err := result.Scan(&acceptanceUnitID, &completed); err != nil {
			t.Fatalf("scan quarantine proof completion state: %v", err)
		}
		state[acceptanceUnitID] = completed
	}
	if err := result.Err(); err != nil {
		t.Fatalf("iterate quarantine proof completion state: %v", err)
	}
	return state
}

func readRepoDependencyQuarantinedLease(ctx context.Context, t *testing.T, db *sql.DB) repoDependencyQuarantinedLease {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
		SELECT partition_id, partition_count, lease_owner, lease_expires_at
		FROM shared_projection_partition_leases
		WHERE projection_domain = $1
		  AND lease_owner IS NOT NULL
		  AND lease_expires_at > CURRENT_TIMESTAMP
	`, reducer.DomainRepoDependency)
	if err != nil {
		t.Fatalf("read quarantined lease: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var leases []repoDependencyQuarantinedLease
	for rows.Next() {
		var lease repoDependencyQuarantinedLease
		if err := rows.Scan(&lease.partitionID, &lease.partitionCount, &lease.owner, &lease.expiresAt); err != nil {
			t.Fatalf("scan quarantined lease: %v", err)
		}
		leases = append(leases, lease)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate quarantined leases: %v", err)
	}
	if len(leases) != 1 {
		t.Fatalf("active quarantined leases=%d, want 1", len(leases))
	}
	return leases[0]
}

func repoDependencyQuarantineShard(acceptanceUnitID string, shardCount int) int {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(acceptanceUnitID))
	return int(hasher.Sum32() % uint32(shardCount))
}

func repoDependencyQuarantineContainsFaultEdge(
	snapshot repoDependencyIfaSnapshot,
	rows []reducer.SharedProjectionIntentRow,
	faultAcceptanceUnit string,
) bool {
	for _, row := range rows {
		if row.AcceptanceUnitID != faultAcceptanceUnit {
			continue
		}
		want := repoDependencyIfaExpectedEdges([]reducer.SharedProjectionIntentRow{row})
		if len(want) == 1 {
			for _, got := range snapshot.typedEdges {
				if got == want[0] {
					return true
				}
			}
		}
	}
	return false
}
