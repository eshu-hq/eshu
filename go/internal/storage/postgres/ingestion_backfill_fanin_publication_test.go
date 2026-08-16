// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Fan-in publication proofs for the deferred backfill.
//
// A (scope, generation) partition's repositories are NOT adjacent in the
// repo-ID sort order the evidence batches are cut from, so one partition
// routinely spans several fixed-size batches. Publishing readiness
// (graph_projection_phase_state) and the partition memo
// (deferred_backfill_partition_memo) from inside a batch therefore publishes a
// partition-wide claim from a transaction that only persisted SOME of the
// partition's evidence. When a sibling batch carrying the rest of that
// partition fails or is canceled, the memo survives, and every later pass sees
// a memo hit in applyDeferredPartitionMemoGate and skips the partition's fact
// load until the catalog fingerprint changes -- durable wrong state.
//
// These tests drive the real batched entry point against a real Postgres and
// pin the fan-in contract: publication happens once per partition, in its own
// transaction, strictly AFTER every evidence batch for the pass has committed,
// and not at all when any batch failed or was canceled.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// fanInProofDSN resolves the throwaway Postgres these proofs run against. It
// accepts ESHU_POSTGRES_DSN in addition to the partition-proof DSNs so the
// reducer contention gate, which provisions ESHU_POSTGRES_DSN, can run them in
// CI. A concurrency regression guarded only by a locally-set env var is a guard
// that exists on paper.
func fanInProofDSN(t *testing.T) string {
	t.Helper()
	for _, key := range []string{
		"ESHU_DEFERRED_PARTITION_PROOF_DSN",
		"ESHU_LATEST_GENERATION_PROOF_DSN",
		"ESHU_POSTGRES_DSN",
	} {
		if dsn := strings.TrimSpace(os.Getenv(key)); dsn != "" {
			return dsn
		}
	}
	t.Skip("set ESHU_POSTGRES_DSN (or ESHU_DEFERRED_PARTITION_PROOF_DSN) to run the deferred backfill fan-in proofs")
	return ""
}

// openFanInProofSchema provisions an isolated schema and returns a handle whose
// EVERY connection resolves to it.
//
// The sibling helper openDeferredPartitionMemoProofDB pins search_path with a
// session-level SET, which forces MaxOpenConns(1) because a second connection
// would land in "public". These proofs need genuinely concurrent transactions
// to exercise the cancel path, so the schema is bound as a connection parameter
// instead: pgx applies unrecognized DSN parameters as PostgreSQL runtime
// parameters, so search_path is set on every connection the pool opens.
func openFanInProofSchema(t *testing.T, ctx context.Context, dsn string, maxConns int) *sql.DB {
	t.Helper()

	schemaName := fmt.Sprintf("deferred_fanin_proof_%d", time.Now().UnixNano())

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = admin.Close() }()
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create fan-in proof schema: %v", err)
	}
	t.Cleanup(func() {
		cleanup, err := sql.Open("pgx", dsn)
		if err != nil {
			return
		}
		defer func() { _ = cleanup.Close() }()
		_, _ = cleanup.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})

	db, err := sql.Open("pgx", withSearchPath(t, dsn, schemaName))
	if err != nil {
		t.Fatalf("open postgres with scoped search_path: %v", err)
	}
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns)
	t.Cleanup(func() { _ = db.Close() })

	// Prove the parameter took effect before any fixture depends on it; a
	// silently-ignored search_path would put the proof tables in "public" and
	// make every assertion below meaningless.
	var effective string
	if err := db.QueryRowContext(ctx, "SHOW search_path").Scan(&effective); err != nil {
		t.Fatalf("read effective search_path: %v", err)
	}
	if !strings.Contains(effective, schemaName) {
		t.Fatalf("effective search_path = %q, want it to contain the proof schema %q", effective, schemaName)
	}
	if _, err := db.ExecContext(ctx, deferredPartitionMemoProofSchemaSQL); err != nil {
		t.Fatalf("create fan-in proof tables: %v", err)
	}
	return db
}

// withSearchPath binds schemaName as the connection's search_path, handling both
// the URL and the keyword/value DSN forms.
func withSearchPath(t *testing.T, dsn, schemaName string) string {
	t.Helper()
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		separator := "?"
		if strings.Contains(dsn, "?") {
			separator = "&"
		}
		return dsn + separator + "search_path=" + schemaName
	}
	return dsn + " search_path=" + schemaName
}

// errFanInProofBatchFailed is the injected batch failure. It is a sentinel so
// the tests assert on the injected cause rather than on message text.
var errFanInProofBatchFailed = errors.New("injected deferred backfill batch failure")

// failOnNthBeginner wraps an ExecQueryer that is also a Beginner and fails the
// Nth Begin call, modelling "batch A commits, sibling batch B fails". Every
// other call delegates untouched, so the pass runs against real Postgres up to
// the injected fault.
type failOnNthBeginner struct {
	inner ExecQueryer

	mu     sync.Mutex
	begins int
	failOn int // 1-based Begin ordinal to fail; 0 disables injection
	// beforeBegin, when set, runs just before the given 1-based Begin ordinal is
	// delegated. It is how a test perturbs the database in the window between
	// the last batch commit and the first fan-in transaction.
	beforeBegin func(ordinal int)
}

func (b *failOnNthBeginner) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return b.inner.QueryContext(ctx, query, args...)
}

func (b *failOnNthBeginner) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return b.inner.ExecContext(ctx, query, args...)
}

func (b *failOnNthBeginner) Begin(ctx context.Context) (Transaction, error) {
	b.mu.Lock()
	b.begins++
	ordinal := b.begins
	b.mu.Unlock()

	if b.beforeBegin != nil {
		b.beforeBegin(ordinal)
	}
	if b.failOn > 0 && ordinal == b.failOn {
		return nil, fmt.Errorf("begin %d: %w", ordinal, errFanInProofBatchFailed)
	}
	beginner, ok := b.inner.(Beginner)
	if !ok {
		return nil, fmt.Errorf("wrapped handle %T is not a Beginner", b.inner)
	}
	return beginner.Begin(ctx)
}

func (b *failOnNthBeginner) beginCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.begins
}

// fanInProofPartition is the shared (scope, generation) the fan-in proofs seed
// their multi-repository partition under.
const (
	fanInProofScopeID      = "git:scope-shared"
	fanInProofGenerationID = "gen-shared"
)

// seedFanInProofSharedPartition seeds one scope holding repoCount repositories
// under a single generation, each cross-referencing the next so the pass
// discovers real evidence for every repository. Returned repo IDs are in the
// same ascending order the batch cutter sorts them into.
func seedFanInProofSharedPartition(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	repoCount int,
	base time.Time,
) []string {
	t.Helper()

	fixtures := make([]memoProofFixture, 0, repoCount)
	repoIDs := make([]string, 0, repoCount)
	for i := 0; i < repoCount; i++ {
		repoID := fmt.Sprintf("repo-r%d", i)
		repoIDs = append(repoIDs, repoID)
		fixtures = append(fixtures, memoProofFixture{
			scopeID:  fanInProofScopeID,
			genID:    fanInProofGenerationID,
			repoID:   repoID,
			repoName: fmt.Sprintf("r%d-service", i),
		})
	}
	crossRefs := make(map[string]string, repoCount)
	for i := 0; i < repoCount; i++ {
		crossRefs[repoIDs[i]] = fmt.Sprintf("r%d-service", (i+1)%repoCount)
	}
	seedMemoProofScopesAndFacts(t, ctx, db, fixtures, crossRefs, base)
	return repoIDs
}

// fanInProofCatalogFingerprint recomputes the catalog fingerprint the pass
// derives, so the gate assertions use the same value the write side would have
// memoized.
func fanInProofCatalogFingerprint(t *testing.T, ctx context.Context, queryer ExecQueryer) string {
	t.Helper()
	catalog, _, err := loadRepositoryCatalog(ctx, queryer)
	if err != nil {
		t.Fatalf("loadRepositoryCatalog() error = %v, want nil", err)
	}
	params, _ := buildDeferredScopedFactQueryParams(catalog)
	return deferredCatalogFingerprint(params)
}

// assertFanInProofGateLoads fails unless the memo gate puts the shared
// partition in ToLoad. A Skipped verdict is the durable-wrong-state outcome the
// fan-in exists to prevent: the partition's evidence was never fully committed,
// yet every later pass would refuse to reload it.
func assertFanInProofGateLoads(t *testing.T, ctx context.Context, adapter ExecQueryer, fingerprint string) {
	t.Helper()
	partition := scopeGenerationPartition{ScopeID: fanInProofScopeID, GenerationID: fanInProofGenerationID}
	gate, err := applyDeferredPartitionMemoGate(
		ctx,
		newDeferredBackfillPartitionMemoStore(adapter),
		[]scopeGenerationPartition{partition},
		fingerprint,
		nil,
	)
	if err != nil {
		t.Fatalf("applyDeferredPartitionMemoGate() error = %v, want nil", err)
	}
	if len(gate.Skipped) != 0 {
		t.Fatalf(
			"memo gate SKIPPED %v after an incomplete pass; the partition's fact load is now suppressed until the catalog fingerprint changes",
			gate.Skipped,
		)
	}
	if len(gate.ToLoad) != 1 {
		t.Fatalf("memo gate ToLoad = %v, want exactly the shared partition", gate.ToLoad)
	}
}

func countFanInProofPhaseRows(t *testing.T, ctx context.Context, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(
		ctx,
		"SELECT count(*) FROM graph_projection_phase_state WHERE scope_id = $1 AND generation_id = $2",
		fanInProofScopeID, fanInProofGenerationID,
	).Scan(&count); err != nil {
		t.Fatalf("count graph_projection_phase_state rows: %v", err)
	}
	return count
}

func countFanInProofMemoRows(t *testing.T, ctx context.Context, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(
		ctx,
		"SELECT count(*) FROM deferred_backfill_partition_memo WHERE scope_id = $1 AND generation_id = $2",
		fanInProofScopeID, fanInProofGenerationID,
	).Scan(&count); err != nil {
		t.Fatalf("count deferred_backfill_partition_memo rows: %v", err)
	}
	return count
}

// TestDeferredBackfillWithholdsPublicationWhenSiblingBatchFails is the primary
// regression. Two repositories share one (scope, generation) partition and land
// in SEPARATE evidence batches. The first batch commits; the second fails. No
// readiness row and no memo row may exist for the partition, because the
// partition's evidence is only half committed -- and the memo gate must still
// put the partition in ToLoad so the next pass reloads it.
func TestDeferredBackfillWithholdsPublicationWhenSiblingBatchFails(t *testing.T) {
	ctx := context.Background()
	db := openFanInProofSchema(t, ctx, fanInProofDSN(t), 4)

	base := time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)
	seedFanInProofSharedPartition(t, ctx, db, 2, base)

	adapter := SQLDB{DB: db}
	fingerprint := fanInProofCatalogFingerprint(t, ctx, adapter)

	// Begin ordinal 2 is the second evidence batch: repo-r0's batch commits,
	// repo-r1's batch never opens.
	failing := &failOnNthBeginner{inner: adapter, failOn: 2}
	store := NewIngestionStore(failing)
	store.Now = func() time.Time { return base }
	store.maintenanceBatchSize = 1
	store.maintenanceWorkers = 1

	err := store.BackfillAllRelationshipEvidence(ctx, nil, nil)
	if err == nil {
		t.Fatal("BackfillAllRelationshipEvidence() error = nil, want the injected batch failure")
	}
	if !errors.Is(err, errFanInProofBatchFailed) {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want the injected batch failure", err)
	}
	if got := failing.beginCount(); got < 2 {
		t.Fatalf("opened %d transactions, want at least 2 (the committing batch and the failing sibling)", got)
	}

	if got := countFanInProofMemoRows(t, ctx, db); got != 0 {
		t.Fatalf(
			"deferred_backfill_partition_memo rows for the half-committed partition = %d, want 0; a memo here permanently suppresses the partition's fact load",
			got,
		)
	}
	if got := countFanInProofPhaseRows(t, ctx, db); got != 0 {
		t.Fatalf(
			"graph_projection_phase_state rows for the half-committed partition = %d, want 0; readiness must not be visible while a contributing batch is unfinished",
			got,
		)
	}
	assertFanInProofGateLoads(t, ctx, adapter, fingerprint)
}

// TestDeferredBackfillWithholdsPublicationWhenSiblingBatchCanceled covers the
// pool's cancel path (ingestion_backfill_pool.go): the first failing batch
// cancels groupCtx, and its in-flight siblings abort mid-transaction. That
// leaves a partition assembled from a mix of committed, rolled-back, and
// never-started batches -- the widest version of the hazard -- and nothing may
// be published for it.
//
// Unlike the serial case this runs several batch transactions at once, so the
// handle is opened with a multi-connection pool; with one connection the
// siblings would queue on Begin instead of being canceled while running.
func TestDeferredBackfillWithholdsPublicationWhenSiblingBatchCanceled(t *testing.T) {
	ctx := context.Background()
	db := openFanInProofSchema(t, ctx, fanInProofDSN(t), 6)

	base := time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)
	seedFanInProofSharedPartition(t, ctx, db, 6, base)

	adapter := SQLDB{DB: db}
	fingerprint := fanInProofCatalogFingerprint(t, ctx, adapter)

	// Fail the third batch to open: batches one and two are already running or
	// committed, and the rest are cancelled before they start.
	failing := &failOnNthBeginner{inner: adapter, failOn: 3}
	store := NewIngestionStore(failing)
	store.Now = func() time.Time { return base }
	store.maintenanceBatchSize = 1
	store.maintenanceWorkers = 4

	err := store.BackfillAllRelationshipEvidence(ctx, nil, nil)
	if err == nil {
		t.Fatal("BackfillAllRelationshipEvidence() error = nil, want the injected batch failure")
	}
	if !errors.Is(err, errFanInProofBatchFailed) {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want the injected batch failure", err)
	}

	if got := countFanInProofMemoRows(t, ctx, db); got != 0 {
		t.Fatalf("deferred_backfill_partition_memo rows after a canceled pass = %d, want 0", got)
	}
	if got := countFanInProofPhaseRows(t, ctx, db); got != 0 {
		t.Fatalf("graph_projection_phase_state rows after a canceled pass = %d, want 0", got)
	}
	assertFanInProofGateLoads(t, ctx, adapter, fingerprint)
}

// TestDeferredBackfillPublishesOncePerPartitionAcrossBatches is the success
// path: one partition spread across three separate committed batches publishes
// exactly one readiness row and one memo row, with no SQLSTATE 21000 and no
// duplicate publication. It is the counterpart to the withholding tests -- the
// fan-in must not become so cautious that a clean pass publishes nothing.
func TestDeferredBackfillPublishesOncePerPartitionAcrossBatches(t *testing.T) {
	ctx := context.Background()
	db := openFanInProofSchema(t, ctx, fanInProofDSN(t), 4)

	base := time.Date(2026, time.August, 15, 9, 0, 0, 0, time.UTC)
	seedFanInProofSharedPartition(t, ctx, db, 3, base)

	adapter := SQLDB{DB: db}
	counting := &failOnNthBeginner{inner: adapter}
	store := NewIngestionStore(counting)
	store.Now = func() time.Time { return base }
	store.maintenanceBatchSize = 1
	store.maintenanceWorkers = 2

	if err := store.BackfillAllRelationshipEvidence(ctx, nil, nil); err != nil {
		t.Fatalf("BackfillAllRelationshipEvidence() error = %v, want nil", err)
	}

	// Three evidence batches plus exactly one fan-in publication transaction:
	// the partition is published once, not once per contributing batch.
	if got, want := counting.beginCount(), 4; got != want {
		t.Fatalf("opened %d transactions, want %d (3 evidence batches + 1 fan-in publication)", got, want)
	}
	if got := countFanInProofPhaseRows(t, ctx, db); got != 1 {
		t.Fatalf("graph_projection_phase_state rows = %d, want exactly 1 for the shared partition", got)
	}
	if got := countFanInProofMemoRows(t, ctx, db); got != 1 {
		t.Fatalf("deferred_backfill_partition_memo rows = %d, want exactly 1 for the shared partition", got)
	}
	if got := countEvidenceRows(t, ctx, db); got == 0 {
		t.Fatal("the pass committed no evidence rows; the fixture is not exercising the evidence path")
	}

	// With everything published, the next pass is a memo hit and skips the load.
	fingerprint := fanInProofCatalogFingerprint(t, ctx, adapter)
	gate, err := applyDeferredPartitionMemoGate(
		ctx,
		newDeferredBackfillPartitionMemoStore(adapter),
		[]scopeGenerationPartition{{ScopeID: fanInProofScopeID, GenerationID: fanInProofGenerationID}},
		fingerprint,
		nil,
	)
	if err != nil {
		t.Fatalf("applyDeferredPartitionMemoGate() error = %v, want nil", err)
	}
	if len(gate.Skipped) != 1 {
		t.Fatalf("memo gate Skipped = %v, want the fully published partition (the memo must be usable after a clean pass)", gate.Skipped)
	}
}
