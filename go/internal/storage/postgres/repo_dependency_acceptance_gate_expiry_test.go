// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"hash/crc32"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

func TestRepoDependencyLeaseOwnerActiveUsesWallClockTimestamp(t *testing.T) {
	if !strings.Contains(repoDependencyLeaseOwnerActiveSQL, "clock_timestamp()") {
		t.Fatal("lease validation must use wall-clock time after waiting for the repository lock")
	}
	if strings.Contains(repoDependencyLeaseOwnerActiveSQL, "CURRENT_TIMESTAMP") {
		t.Fatal("transaction-start time can accept a lease that expired while the repository lock was blocked")
	}
}

func TestReducerContentionGateRepoDependencyAcceptanceUnitGateRejectsLeaseExpiredWhileWaitingForRepoLock(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_SHARED_PROJECTION_RESCALE_PROOF_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	}
	if dsn == "" {
		t.Skip("set ESHU_SHARED_PROJECTION_RESCALE_PROOF_DSN or ESHU_POSTGRES_DSN to run the real-Postgres gate proof")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	database, cleanup := openSharedIntentRescaleProofDB(t, ctx, dsn)
	defer cleanup()

	const (
		domain = "repo_dependency_gate_expiry_proof"
		repoID = "repository:gate-expiry-proof"
		owner  = "process-a/worker-0-of-4"
	)
	store := NewSharedIntentStore(SQLDB{DB: database})
	claimed, err := store.ClaimPartitionLease(ctx, domain, 0, 4, owner, 30*time.Second)
	if err != nil || !claimed {
		t.Fatalf("claim owner lease = %v, %v; want true, nil", claimed, err)
	}

	blocker, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin repository-lock blocker: %v", err)
	}
	defer func() { _ = blocker.Rollback() }()
	if err := acquireDeferredMaintenanceRepoExclusiveLocks(ctx, SQLTx{Tx: blocker}, []string{repoID}); err != nil {
		t.Fatalf("hold repository lock: %v", err)
	}

	lockAttempted := make(chan struct{})
	gate := NewRepoDependencyAcceptanceUnitGate(&signalingRepoDependencyGateBeginner{
		inner:         SQLDB{DB: database},
		lockAttempted: lockAttempted,
	})
	key := reducer.RepoDependencyAcceptanceUnitGateKey{
		Domain: domain, AcceptanceUnitID: repoID,
		PartitionID: 0, PartitionCount: 4, LeaseOwner: owner,
	}
	type gateResult struct {
		ran bool
		err error
	}
	callbackRan := make(chan struct{}, 1)
	result := make(chan gateResult, 1)
	go func() {
		ran, gateErr := gate.WithAcceptanceUnit(ctx, key, func(context.Context, reducer.RepoDependencyProjectionIntentReader) error {
			callbackRan <- struct{}{}
			return nil
		})
		result <- gateResult{ran: ran, err: gateErr}
	}()

	select {
	case <-lockAttempted:
	case <-ctx.Done():
		t.Fatal("gate did not attempt the blocked repository lock")
	}
	var expiresAt time.Time
	if err := database.QueryRowContext(ctx, `
		UPDATE shared_projection_partition_leases
		SET lease_expires_at = clock_timestamp() + interval '200 milliseconds'
		WHERE projection_domain = $1 AND partition_id = 0 AND partition_count = 4
		RETURNING lease_expires_at
	`, domain).Scan(&expiresAt); err != nil {
		t.Fatalf("set lease expiry after gate transaction began: %v", err)
	}
	waitForPostgresWallClockAfter(t, ctx, database, expiresAt)
	if err := blocker.Commit(); err != nil {
		t.Fatalf("release repository-lock blocker: %v", err)
	}

	got := <-result
	if got.err != nil || got.ran {
		t.Fatalf("expired gate result = %+v, want ran=false and err=nil", got)
	}
	select {
	case <-callbackRan:
		t.Fatal("callback ran after the lease expired while waiting for the repository lock")
	default:
	}
}

type signalingRepoDependencyGateBeginner struct {
	inner         db.Beginner
	lockAttempted chan struct{}
}

func (b *signalingRepoDependencyGateBeginner) Begin(ctx context.Context) (db.Transaction, error) {
	tx, err := b.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &signalingRepoDependencyGateTransaction{Transaction: tx, lockAttempted: b.lockAttempted}, nil
}

type signalingRepoDependencyGateTransaction struct {
	db.Transaction
	lockAttempted chan struct{}
	once          sync.Once
}

func (tx *signalingRepoDependencyGateTransaction) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if strings.Contains(query, "pg_advisory_xact_lock") {
		tx.once.Do(func() { close(tx.lockAttempted) })
	}
	return tx.Transaction.ExecContext(ctx, query, args...)
}

func waitForPostgresWallClockAfter(t *testing.T, ctx context.Context, database *sql.DB, after time.Time) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var elapsed bool
		if err := database.QueryRowContext(ctx, "SELECT clock_timestamp() > $1", after).Scan(&elapsed); err != nil {
			t.Fatalf("read Postgres wall clock: %v", err)
		}
		if elapsed {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("Postgres wall clock did not pass the forced lease expiry")
		}
	}
}

func TestRepoDependencyRunsOnFenceComposesQueuePhaseAndProjectionLive(t *testing.T) {
	for _, batchAck := range []bool{false, true} {
		name := "single_ack"
		if batchAck {
			name = "batch_ack"
		}
		t.Run(name, func(t *testing.T) {
			database, ctx := refinalizeRebuildResetLiveDB(t)
			suffix := testSuffix(t)
			scopeID, generationID, _ := refinalizeResetScope(t, ctx, database, suffix)
			repoID, entityKey := "repo:fence-"+suffix, "repo:fence-"+suffix
			now := time.Now().UTC()
			row := reducer.SharedProjectionIntentRow{
				IntentID: "runs-on-" + suffix, ProjectionDomain: reducer.DomainRepoDependency,
				PartitionKey: "runs_on:" + repoID + "->platform:kubernetes:test", ScopeID: scopeID,
				AcceptanceUnitID: repoID, RepositoryID: repoID, SourceRunID: "repo_dependency:" + scopeID,
				GenerationID: generationID, CreatedAt: now,
				Payload: map[string]any{"repo_id": repoID, "platform_id": "platform:kubernetes:test", "relationship_type": "RUNS_ON", "evidence_source": reducer.CrossRepoEvidenceSource},
			}
			if err := NewSharedIntentAcceptanceWriter(SQLDB{DB: database}).UpsertIntents(ctx, []reducer.SharedProjectionIntentRow{row}); err != nil {
				t.Fatalf("seed RUNS_ON intent: %v", err)
			}
			phaseStore := NewGraphProjectionPhaseStateStore(SQLDB{DB: database})
			legacyKey := reducer.GraphProjectionPhaseKey{ScopeID: scopeID, AcceptanceUnitID: repoID, SourceRunID: generationID, GenerationID: generationID, Keyspace: reducer.GraphProjectionKeyspaceServiceUID}
			if err := phaseStore.Upsert(ctx, []reducer.GraphProjectionPhaseState{{Key: legacyKey, Phase: reducer.GraphProjectionPhaseWorkloadMaterialization, CommittedAt: now, UpdatedAt: now}}); err != nil {
				t.Fatalf("seed stale workload phase: %v", err)
			}

			queue := NewReducerQueue(SQLDB{DB: database}, "fence-proof-"+suffix, time.Minute)
			queue.ClaimDomain = reducer.DomainWorkloadMaterialization
			if _, err := queue.Enqueue(ctx, []projector.ReducerIntent{{ScopeID: scopeID, GenerationID: generationID, Domain: reducer.DomainWorkloadMaterialization, EntityKey: entityKey, Reason: "pre-fence pass", SourceSystem: "reducer"}}); err != nil {
				t.Fatalf("enqueue stale workload pass: %v", err)
			}
			stale, ok, err := queue.Claim(ctx)
			if err != nil || !ok {
				t.Fatalf("claim stale workload pass = (%v, %v), want work", ok, err)
			}

			writer := &causalFenceEdgeWriter{}
			runner := causalFenceRunner(database, queue, writer, suffix)
			stop := startCausalFenceRunner(ctx, t, runner)
			var fence string
			waitForCausalFence(t, ctx, func() bool {
				var repo string
				var dirty bool
				err := database.QueryRowContext(ctx, `SELECT COALESCE(payload->>'repo_dependency_readiness_fence',''), COALESCE(payload->>'repo_dependency_readiness_repo_id',''), cross_scope_replay_required FROM fact_work_items WHERE work_item_id=$1`, stale.IntentID).Scan(&fence, &repo, &dirty)
				return err == nil && fence != "" && repo == repoID && dirty
			})
			stop()
			if writer.writeCount() != 0 || writer.retractCount() != 0 || sharedIntentCompleted(t, ctx, database, row.IntentID) {
				t.Fatal("RUNS_ON retracted, projected, or completed before token-scoped workload readiness")
			}

			ackCausalFence(t, ctx, queue, stale, batchAck)
			if state := readClaimTokenWorkState(t, ctx, database, stale.IntentID); state.status != "pending" {
				t.Fatalf("stale ACK status = %q, want pending", state.status)
			}
			fresh, ok, err := queue.Claim(ctx)
			if err != nil || !ok || fresh.Payload[reducer.RepoDependencyReadinessFencePayloadKey] != fence || fresh.Payload[reducer.RepoDependencyReadinessRepoIDPayloadKey] != repoID {
				t.Fatalf("reclaimed fenced payload = (%v, %v, %v), want token %q repo %q", fresh.Payload, ok, err, fence, repoID)
			}
			handler := reducer.WorkloadMaterializationHandler{
				FactLoader:     causalFenceFactLoader{repoID: repoID},
				Materializer:   reducer.NewWorkloadMaterializer(nil),
				PhasePublisher: phaseStore,
			}
			if _, err := handler.Handle(ctx, fresh); err != nil {
				t.Fatalf("handle fenced workload pass: %v", err)
			}
			ackCausalFence(t, ctx, queue, fresh, batchAck)

			stop = startCausalFenceRunner(ctx, t, runner)
			waitForCausalFence(t, ctx, func() bool { return sharedIntentCompleted(t, ctx, database, row.IntentID) })
			stop()
			if writer.writeCount() != 1 {
				t.Fatalf("RUNS_ON write count = %d, want 1 after token readiness", writer.writeCount())
			}
		})
	}
}

type causalFenceFactLoader struct{ repoID string }

func (l causalFenceFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return []facts.Envelope{{FactID: "repo", FactKind: "repository", Payload: map[string]any{"graph_id": l.repoID}}}, nil
}

type causalFenceEdgeWriter struct {
	mu       sync.Mutex
	retracts int
	writes   int
}

func (w *causalFenceEdgeWriter) RetractEdges(context.Context, string, []sharedintent.Row, string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.retracts++
	return nil
}

func (w *causalFenceEdgeWriter) WriteEdges(context.Context, string, []sharedintent.Row, string) (sharedintent.WriteReport, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes++
	return sharedintent.WriteReport{}, nil
}

func (w *causalFenceEdgeWriter) writeCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writes
}

func (w *causalFenceEdgeWriter) retractCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.retracts
}

func causalFenceRunner(database *sql.DB, queue ReducerQueue, writer reducer.SharedProjectionEdgeWriter, suffix string) *reducer.RepoDependencyProjectionRunner {
	store := NewSharedIntentStore(SQLDB{DB: database})
	const partitionCount = 1_000_000_000
	partitionID := int(crc32.ChecksumIEEE([]byte(suffix)) % partitionCount)
	return &reducer.RepoDependencyProjectionRunner{
		IntentReader:                    store,
		LeaseManager:                    store,
		AcceptanceUnitGate:              NewRepoDependencyAcceptanceUnitGate(SQLDB{DB: database}),
		EdgeWriter:                      writer,
		WorkloadMaterializationReplayer: queue,
		WorkloadReadinessPrefetch:       NewGraphProjectionReadinessPrefetch(SQLDB{DB: database}),
		AcceptedGen:                     NewAcceptedGenerationLookup(SQLDB{DB: database}),
		AcceptedGenPrefetch:             NewAcceptedGenerationPrefetch(SQLDB{DB: database}),
		Config: reducer.RepoDependencyProjectionRunnerConfig{
			LeaseOwner:            "fence-runner-" + suffix,
			PollInterval:          time.Millisecond,
			LeaseTTL:              35 * time.Second,
			CycleTimeout:          2 * time.Second,
			GraphQuiescenceBudget: time.Millisecond,
			BatchLimit:            100,
			PartitionID:           partitionID,
			PartitionCount:        partitionCount,
		},
	}
}

func startCausalFenceRunner(ctx context.Context, t *testing.T, runner *reducer.RepoDependencyProjectionRunner) func() {
	t.Helper()
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- runner.Run(runCtx) }()
	return func() {
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("run repo dependency projection: %v", err)
		}
	}
}

func waitForCausalFence(t *testing.T, ctx context.Context, ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for !ready() {
		select {
		case <-time.After(10 * time.Millisecond):
		case <-deadline.C:
			t.Fatal("causal fence condition did not converge")
		case <-ctx.Done():
			t.Fatal("causal fence context expired")
		}
	}
}

func ackCausalFence(t *testing.T, ctx context.Context, queue ReducerQueue, intent reducer.Intent, batch bool) {
	t.Helper()
	var err error
	if batch {
		err = queue.AckBatch(ctx, []reducer.Intent{intent}, nil)
	} else {
		err = queue.Ack(ctx, intent, reducer.Result{})
	}
	if err != nil {
		t.Fatalf("ack workload pass: %v", err)
	}
}

func sharedIntentCompleted(t *testing.T, ctx context.Context, database *sql.DB, intentID string) bool {
	t.Helper()
	var completed bool
	if err := database.QueryRowContext(ctx, `SELECT completed_at IS NOT NULL FROM shared_projection_intents WHERE intent_id=$1`, intentID).Scan(&completed); err != nil {
		t.Fatalf("read shared intent completion: %v", err)
	}
	return completed
}
