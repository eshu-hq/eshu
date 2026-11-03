// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplanestore_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossplane"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/crossplane"
)

// crossplaneRedriveSpyEdgeWriter records every SATISFIED_BY row the
// materialization handler would have committed, without touching any graph
// backend. Sufficient to prove the handler's own resolution decision (would
// it produce an edge, and for which claim/xrd pair) -- the actual Cypher
// MERGE is exercised by cypher.CrossplaneSatisfiedByEdgeWriter's own tests.
type crossplaneRedriveSpyEdgeWriter struct {
	written []map[string]any
}

func (s *crossplaneRedriveSpyEdgeWriter) WriteCrossplaneSatisfiedByEdges(
	_ context.Context, rows []map[string]any, _, _, _ string,
) error {
	s.written = append(s.written, rows...)
	return nil
}

func (s *crossplaneRedriveSpyEdgeWriter) RetractCrossplaneSatisfiedByEdges(
	context.Context, []string, string, string,
) error {
	return nil
}

// crossplaneRedriveFakeEdgeExistenceReader simulates the post-write
// existence-check read (issue #5476 P1-b) without a real graph backend,
// mirroring how crossplaneRedriveSpyEdgeWriter simulates the write itself.
// confirmAll true confirms every requested (claim_uid, xrd_uid) pair,
// simulating a graph where every written edge actually committed; the zero
// value confirms nothing, simulating cypher.CrossplaneSatisfiedByEdgeWriter's
// MATCH-MATCH-MERGE having silently no-oped because an endpoint node was
// absent -- the exact scenario the ledger must never fence on.
type crossplaneRedriveFakeEdgeExistenceReader struct {
	confirmAll bool
}

func (r *crossplaneRedriveFakeEdgeExistenceReader) Run(
	_ context.Context, _ string, params map[string]any,
) ([]map[string]any, error) {
	if !r.confirmAll {
		return nil, nil
	}
	rows, _ := params["rows"].([]map[string]any)
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"confirmed_claim_uid": row["claim_uid"],
			"confirmed_xrd_uid":   row["xrd_uid"],
		})
	}
	return out, nil
}

// TestCrossplaneSatisfiedByRedriveClosesXRDLagWindowLive is the issue #5476
// acceptance-criterion regression: a Claim scope is projected BEFORE its XRD
// scope, so its own SATISFIED_BY materialization pass resolves zero edges
// (the false-negative window #5347 left open for cross-scope XRDs). The XRD
// scope is then ingested and activated. The cross-scope redrive sweep --
// triggered here directly, exactly like ProjectorQueue.Ack's post-commit
// hook would -- re-enqueues the Claim scope's SATISFIED_BY intent. Replaying
// that intent (the reducer's normal claim/handle path) now resolves the
// edge, WITHOUT the Claim scope ever being re-ingested: no new
// scope_generations row, no new fact_records row, is written for the Claim
// scope anywhere in this test after its initial seed.
// TestCrossplaneSatisfiedByRedriveClosesXRDLagWindowLive keeps every redrive scenario in the scheduled proof.
func TestCrossplaneSatisfiedByRedriveClosesXRDLagWindowLive(t *testing.T) {
	t.Run("xrd_lag_window", testCrossplaneSatisfiedByRedriveClosesXRDLagWindowLive)
	t.Run("running_claim_ack", testCrossplaneRedriveDuringRunningClaimReturnsAckToPendingLive)
	t.Run("claimed_ack", testCrossplaneRedriveDuringClaimedClaimReturnsAckToPendingLive)
	t.Run("duplicate_schedule", testCrossplaneRedriveDuplicateScheduleKeepsSingleItemLive)
	t.Run("retry_clears_debt", testCrossplaneRedriveRetryClearsReplayDebtLive)
	t.Run("concurrent_insert", testCrossplaneRedriveConcurrentInsertWinnerKeepsReplayDebtLive)
	t.Run("claimed_row_cost", testCrossplaneReplayClaimedRowCostLive)
	t.Run("terminal_target_claimed", testCrossplaneRedriveTerminalTargetStaysClaimedForCatchUpLive)
	t.Run("stale_sweep_single_scheduling", testCrossplaneRedriveStaleSweepReplayKeepsSingleSchedulingLive)
}

func testCrossplaneSatisfiedByRedriveClosesXRDLagWindowLive(t *testing.T) {
	db, _ := crossplaneRedriveProofDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	const (
		claimScopeID      = "scope-claim-behavior"
		claimGenerationID = "gen-claim-behavior-001"
		xrdScopeID        = "scope-xrd-behavior"
		xrdGenerationID   = "gen-xrd-behavior-001"
		group             = "example.org"
		claimKind         = "XExampleClaim"
	)

	// crossplaneRedriveProofConn pins db to a single connection so search_path
	// stays set on the one connection every subsequent call reuses. Seed
	// directly through db (never a separately checked-out *sql.Conn): holding
	// a dedicated connection open would starve the handler/sweeper calls
	// below, which draw from this same single-connection pool.
	seedCrossplaneRedriveClaimScope(ctx, t, db, claimScopeID, claimGenerationID, group, claimKind, 1, now)

	factStore := postgres.NewFactStore(postgres.SQLDB{DB: db})
	handler := crossplane.CrossplaneSatisfiedByMaterializationHandler{
		FactLoader: factStore,
		EdgeWriter: &crossplaneRedriveSpyEdgeWriter{},
	}
	claimIntent := reducercontract.Intent{
		IntentID:     "intent-claim-behavior-1",
		ScopeID:      claimScopeID,
		GenerationID: claimGenerationID,
		Domain:       reducer.DomainCrossplaneSatisfiedByMaterialization,
		AttemptCount: 1,
	}

	// RED: reproduce the false-negative window before this issue's fix. The
	// Claim's own materialization pass runs with zero XRD facts visible
	// anywhere -- zero-match, no edge.
	redSpy := handler.EdgeWriter.(*crossplaneRedriveSpyEdgeWriter)
	if _, err := handler.Handle(ctx, claimIntent); err != nil {
		t.Fatalf("handle claim intent (red): %v", err)
	}
	if len(redSpy.written) != 0 {
		t.Fatalf("expected zero SATISFIED_BY rows before the XRD exists, got %d: %v", len(redSpy.written), redSpy.written)
	}

	// Step 2: the XRD platform repo is ingested and its generation activates
	// -- AFTER the Claim scope's own generation already ran.
	seedCrossplaneRedriveXRD(ctx, t, db, xrdScopeID, xrdGenerationID, group, claimKind, now.Add(time.Minute))

	// Step 3: the cross-scope redrive sweep runs for the newly-active XRD
	// generation, exactly as ProjectorQueue.Ack's post-commit hook would
	// trigger it.
	reducerQueue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "test-owner", time.Minute)
	sweeper := crossplanestore.CrossplaneSatisfiedByRedriveSweeper{
		DB:       postgres.SQLQueryer{DB: db},
		State:    crossplanestore.NewCrossplaneRedriveStateStore(postgres.SQLDB{DB: db}),
		Replayer: reducerQueue,
		Owner:    "test-owner",
	}
	result, err := sweeper.Sweep(ctx, xrdScopeID, xrdGenerationID)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !result.Attempted {
		t.Fatalf("expected the sweep to attempt real work, got %+v", result)
	}
	if result.Outcome != crossplanestore.CrossplaneRedriveOutcomeCompletedForTest {
		t.Fatalf("expected sweep outcome %q, got %q", crossplanestore.CrossplaneRedriveOutcomeCompletedForTest, result.Outcome)
	}
	if result.TargetsEnqueued != 1 {
		t.Fatalf("expected exactly 1 target scope re-driven, got %d", result.TargetsEnqueued)
	}

	// The Claim scope's SATISFIED_BY intent must now be pending, without any
	// new generation or fact for the Claim scope having been written.
	assertCrossplaneRedriveWorkItemPending(ctx, t, db, claimScopeID, claimGenerationID)

	// GREEN: replay the SAME intent (same generation -- the Claim scope was
	// never re-ingested). The handler now sees the active cross-scope XRD and
	// resolves the edge.
	greenSpy := &crossplaneRedriveSpyEdgeWriter{}
	greenHandler := crossplane.CrossplaneSatisfiedByMaterializationHandler{
		FactLoader: factStore,
		EdgeWriter: greenSpy,
	}
	if _, err := greenHandler.Handle(ctx, claimIntent); err != nil {
		t.Fatalf("handle claim intent (green): %v", err)
	}
	if len(greenSpy.written) != 1 {
		t.Fatalf("expected exactly 1 SATISFIED_BY row after the redrive, got %d: %v", len(greenSpy.written), greenSpy.written)
	}
	row := greenSpy.written[0]
	expectedXRDUID := "fact-xrd-" + xrdGenerationID + "-uid"
	if row["xrd_uid"] != expectedXRDUID {
		t.Fatalf("expected the resolved edge to target %q, got %v", expectedXRDUID, row["xrd_uid"])
	}
}

func assertCrossplaneRedriveWorkItemPending(ctx context.Context, t *testing.T, db *sql.DB, scopeID, generationID string) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
		SELECT status FROM fact_work_items
		WHERE scope_id = $1 AND generation_id = $2
		  AND stage = 'reducer' AND domain = 'crossplane_satisfied_by_materialization'
	`, scopeID, generationID)
	if err != nil {
		t.Fatalf("query fact_work_items: %v", err)
	}
	defer func() { _ = rows.Close() }()
	found := false
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			t.Fatalf("scan fact_work_items status: %v", err)
		}
		if status == "pending" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate fact_work_items: %v", err)
	}
	if !found {
		t.Fatalf("expected a pending crossplane_satisfied_by_materialization work item for scope %q generation %q", scopeID, generationID)
	}
}

// TestCrossplaneRedriveDuringRunningClaimReturnsAckToPendingLive pins the
// activation/ACK ordering seen by the fault-injection gate. The first Claim
// pass observes no XRD, activation schedules redrive while that same reducer
// row is still running, and its ACK must preserve one replay pass.
func testCrossplaneRedriveDuringRunningClaimReturnsAckToPendingLive(t *testing.T) {
	db, _ := crossplaneRedriveProofDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	const (
		claimScopeID      = "scope-claim-running-redrive"
		claimGenerationID = "gen-claim-running-redrive"
		xrdScopeID        = "scope-xrd-running-redrive"
		xrdGenerationID   = "gen-xrd-running-redrive"
		group             = "database.example.com"
		claimKind         = "PostgreSQL"
	)
	seedCrossplaneRedriveClaimScope(ctx, t, db, claimScopeID, claimGenerationID, group, claimKind, 2, now)
	queue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "running-redrive-worker", time.Minute)
	queue.ClaimDomain = reducer.DomainCrossplaneSatisfiedByMaterialization
	if scheduled, err := queue.ReplayCrossplaneSatisfiedByMaterialization(ctx, claimScopeID, claimGenerationID); err != nil || !scheduled {
		t.Fatalf("enqueue initial Claim materialization: scheduled=%t error=%v", scheduled, err)
	}
	claimed, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim initial materialization: claimed=%t error=%v", ok, err)
	}
	spy := &crossplaneRedriveSpyEdgeWriter{}
	handler := crossplane.CrossplaneSatisfiedByMaterializationHandler{
		FactLoader: postgres.NewFactStore(postgres.SQLDB{DB: db}),
		EdgeWriter: spy,
	}
	if _, err := handler.Handle(ctx, claimed); err != nil {
		t.Fatalf("materialize before XRD activation: %v", err)
	}
	if len(spy.written) != 0 {
		t.Fatalf("Claim materialization before XRD wrote %d edges, want zero", len(spy.written))
	}
	// The worker has finished its zero-edge handler but has not ACKed. Keep
	// its production claim and lease while modeling the runtime's running state.
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status = 'running' WHERE work_item_id = $1 AND status = 'claimed'`, claimed.IntentID); err != nil {
		t.Fatalf("mark claimed reducer work running: %v", err)
	}
	seedCrossplaneRedriveXRD(ctx, t, db, xrdScopeID, xrdGenerationID, group, claimKind, now.Add(time.Minute))
	sweeper := crossplanestore.CrossplaneSatisfiedByRedriveSweeper{
		DB:       postgres.SQLQueryer{DB: db},
		State:    crossplanestore.NewCrossplaneRedriveStateStore(postgres.SQLDB{DB: db}),
		Replayer: queue,
		Owner:    "running-redrive-worker",
	}
	result, err := sweeper.Sweep(ctx, xrdScopeID, xrdGenerationID)
	if err != nil {
		t.Fatalf("sweep after XRD activation: %v", err)
	}
	if result.Outcome != crossplanestore.CrossplaneRedriveOutcomeCompletedForTest || result.TargetsEnqueued != 1 {
		t.Fatalf("sweep result = %+v, want one completed target", result)
	}
	if err := queue.Ack(ctx, claimed, reducercontract.Result{}); err != nil {
		t.Fatalf("ACK zero-edge Claim pass: %v", err)
	}
	assertCrossplaneRedriveWorkItemPending(ctx, t, db, claimScopeID, claimGenerationID)
	replayed, ok, err := queue.Claim(ctx)
	if err != nil || !ok || replayed.IntentID != claimed.IntentID {
		t.Fatalf("claim replayed materialization: intent=%q claimed=%t error=%v", replayed.IntentID, ok, err)
	}
	if _, err := handler.Handle(ctx, replayed); err != nil {
		t.Fatalf("materialize Claim with XRD visible: %v", err)
	}
	if len(spy.written) != 2 {
		t.Fatalf("replayed materialization wrote %d edges, want two", len(spy.written))
	}
	if err := queue.Ack(ctx, replayed, reducercontract.Result{}); err != nil {
		t.Fatalf("ACK replayed Claim pass: %v", err)
	}
	if extra, ok, err := queue.Claim(ctx); err != nil || ok {
		t.Fatalf("claim after replay ACK = (%q, %t, %v), want no work", extra.IntentID, ok, err)
	}
}

// TestCrossplaneRedriveDuringClaimedClaimReturnsAckToPendingLive pins the
// second ACK ordering: the sweep schedules redrive while the Claim pass is
// still claimed (never marked running), and its ACK must still preserve one
// replay pass that resolves both edges. The trigger path is uniform across
// claimed and running rows; this test locks the claimed half that the
// running-claim test does not cover.
func testCrossplaneRedriveDuringClaimedClaimReturnsAckToPendingLive(t *testing.T) {
	db, _ := crossplaneRedriveProofDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	const (
		claimScopeID      = "scope-claim-claimed-redrive"
		claimGenerationID = "gen-claim-claimed-redrive"
		xrdScopeID        = "scope-xrd-claimed-redrive"
		xrdGenerationID   = "gen-xrd-claimed-redrive"
		group             = "database.example.com"
		claimKind         = "PostgreSQL"
	)
	seedCrossplaneRedriveClaimScope(ctx, t, db, claimScopeID, claimGenerationID, group, claimKind, 2, now)
	queue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "claimed-redrive-worker", time.Minute)
	queue.ClaimDomain = reducer.DomainCrossplaneSatisfiedByMaterialization
	if scheduled, err := queue.ReplayCrossplaneSatisfiedByMaterialization(ctx, claimScopeID, claimGenerationID); err != nil || !scheduled {
		t.Fatalf("enqueue initial Claim materialization: scheduled=%t error=%v", scheduled, err)
	}
	claimed, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim initial materialization: claimed=%t error=%v", ok, err)
	}
	spy := &crossplaneRedriveSpyEdgeWriter{}
	handler := crossplane.CrossplaneSatisfiedByMaterializationHandler{
		FactLoader: postgres.NewFactStore(postgres.SQLDB{DB: db}),
		EdgeWriter: spy,
	}
	if _, err := handler.Handle(ctx, claimed); err != nil {
		t.Fatalf("materialize before XRD activation: %v", err)
	}
	if len(spy.written) != 0 {
		t.Fatalf("Claim materialization before XRD wrote %d edges, want zero", len(spy.written))
	}
	// Unlike the running-claim test, the item stays claimed here: the sweep
	// must schedule redrive against the claimed row without stealing its
	// lease, and the ACK must return it to pending.
	seedCrossplaneRedriveXRD(ctx, t, db, xrdScopeID, xrdGenerationID, group, claimKind, now.Add(time.Minute))
	sweeper := crossplanestore.CrossplaneSatisfiedByRedriveSweeper{
		DB:       postgres.SQLQueryer{DB: db},
		State:    crossplanestore.NewCrossplaneRedriveStateStore(postgres.SQLDB{DB: db}),
		Replayer: queue,
		Owner:    "claimed-redrive-worker",
	}
	result, err := sweeper.Sweep(ctx, xrdScopeID, xrdGenerationID)
	if err != nil {
		t.Fatalf("sweep after XRD activation: %v", err)
	}
	if result.Outcome != crossplanestore.CrossplaneRedriveOutcomeCompletedForTest || result.TargetsEnqueued != 1 {
		t.Fatalf("sweep result = %+v, want one completed target", result)
	}
	if err := queue.Ack(ctx, claimed, reducercontract.Result{}); err != nil {
		t.Fatalf("ACK zero-edge Claim pass: %v", err)
	}
	assertCrossplaneRedriveWorkItemPending(ctx, t, db, claimScopeID, claimGenerationID)
	replayed, ok, err := queue.Claim(ctx)
	if err != nil || !ok || replayed.IntentID != claimed.IntentID {
		t.Fatalf("claim replayed materialization: intent=%q claimed=%t error=%v", replayed.IntentID, ok, err)
	}
	if _, err := handler.Handle(ctx, replayed); err != nil {
		t.Fatalf("materialize Claim with XRD visible: %v", err)
	}
	if len(spy.written) != 2 {
		t.Fatalf("replayed materialization wrote %d edges, want two", len(spy.written))
	}
	if err := queue.Ack(ctx, replayed, reducercontract.Result{}); err != nil {
		t.Fatalf("ACK replayed Claim pass: %v", err)
	}
	if extra, ok, err := queue.Claim(ctx); err != nil || ok {
		t.Fatalf("claim after replay ACK = (%q, %t, %v), want no work", extra.IntentID, ok, err)
	}
}

// TestCrossplaneRedriveDuplicateScheduleKeepsSingleItemLive pins duplicate
// scheduling: scheduling the same scope-generation twice must not create a
// second stable work item, and the single item must still drain normally.
func testCrossplaneRedriveDuplicateScheduleKeepsSingleItemLive(t *testing.T) {
	db, _ := crossplaneRedriveProofDB(t)
	ctx := context.Background()
	const (
		scopeID      = "scope-claim-duplicate-schedule"
		generationID = "gen-claim-duplicate-schedule"
	)
	seedCrossplaneRedriveClaimScope(ctx, t, db, scopeID, generationID, "database.example.com", "PostgreSQL", 1, time.Now().UTC())
	queue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "duplicate-schedule-worker", time.Minute)
	queue.ClaimDomain = reducer.DomainCrossplaneSatisfiedByMaterialization
	for i := 0; i < 2; i++ {
		if scheduled, err := queue.ReplayCrossplaneSatisfiedByMaterialization(ctx, scopeID, generationID); err != nil || !scheduled {
			t.Fatalf("schedule attempt %d: scheduled=%t error=%v", i, scheduled, err)
		}
	}
	var count int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM fact_work_items
		WHERE scope_id = $1 AND generation_id = $2
		  AND stage = 'reducer' AND domain = 'crossplane_satisfied_by_materialization'
	`, scopeID, generationID).Scan(&count); err != nil {
		t.Fatalf("count scheduled work items: %v", err)
	}
	if count != 1 {
		t.Fatalf("scheduled work items = %d, want exactly one after duplicate scheduling", count)
	}
	claimed, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim single scheduled item: claimed=%t error=%v", ok, err)
	}
	// No replay debt was ever recorded, so the ACK completes the item
	// instead of returning it to pending.
	if err := queue.Ack(ctx, claimed, reducercontract.Result{}); err != nil {
		t.Fatalf("ACK single scheduled item: %v", err)
	}
	if extra, ok, err := queue.Claim(ctx); err != nil || ok {
		t.Fatalf("claim after ACK = (%q, %t, %v), want no work", extra.IntentID, ok, err)
	}
}

// crossplaneRedriveRetryableError is a retryable failure cause for driving
// the production retry entry point (queue.Fail) in the retry-clears-debt
// test below.
type crossplaneRedriveRetryableError struct{}

func (crossplaneRedriveRetryableError) Error() string   { return "redrive proof retryable failure" }
func (crossplaneRedriveRetryableError) Retryable() bool { return true }

// TestCrossplaneRedriveRetryClearsReplayDebtLive pins the production retry
// path end to end: failing a claimed item that carries replay debt must move
// it to retrying, clear the debt, and release the lease (failIntent via the
// shipped retryReducerWorkQuery), and a later replay must still schedule.
func testCrossplaneRedriveRetryClearsReplayDebtLive(t *testing.T) {
	db, _ := crossplaneRedriveProofDB(t)
	ctx := context.Background()
	const (
		scopeID      = "scope-claim-retry-clears-debt"
		generationID = "gen-claim-retry-clears-debt"
		owner        = "retry-debt-worker"
	)
	seedCrossplaneRedriveClaimScope(ctx, t, db, scopeID, generationID, "database.example.com", "PostgreSQL", 1, time.Now().UTC())
	queue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, owner, time.Minute)
	queue.ClaimDomain = reducer.DomainCrossplaneSatisfiedByMaterialization
	if scheduled, err := queue.ReplayCrossplaneSatisfiedByMaterialization(ctx, scopeID, generationID); err != nil || !scheduled {
		t.Fatalf("enqueue Claim materialization: scheduled=%t error=%v", scheduled, err)
	}
	claimed, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim materialization: claimed=%t error=%v", ok, err)
	}
	if scheduled, err := queue.ReplayCrossplaneSatisfiedByMaterialization(ctx, scopeID, generationID); err != nil || !scheduled {
		t.Fatalf("record replay debt on claimed item: scheduled=%t error=%v", scheduled, err)
	}
	var status string
	var leaseOwner sql.NullString
	var debt bool
	readRow := func() {
		t.Helper()
		if err := db.QueryRowContext(ctx, `
			SELECT status, lease_owner, cross_scope_replay_required
			FROM fact_work_items WHERE work_item_id = $1
		`, claimed.IntentID).Scan(&status, &leaseOwner, &debt); err != nil {
			t.Fatalf("read work item: %v", err)
		}
	}
	readRow()
	if status != "claimed" || !leaseOwner.Valid || leaseOwner.String != owner || !debt {
		t.Fatalf("row before retry = (%q, %q, %t), want claimed/owner/debt", status, leaseOwner.String, debt)
	}
	// Drive the shipped retry entry point, not a hand-written status flip:
	// failIntent classifies the cause as retryable and runs
	// retryReducerWorkQuery, which sets status retrying, clears replay debt,
	// and releases the lease.
	if err := queue.Fail(ctx, claimed, crossplaneRedriveRetryableError{}); err != nil {
		t.Fatalf("fail claimed item with retryable cause: %v", err)
	}
	readRow()
	if status != "retrying" || leaseOwner.Valid || debt {
		t.Fatalf("row after retry = (%q, %q, %t), want retrying/released-lease/no-debt", status, leaseOwner.String, debt)
	}
	if scheduled, err := queue.ReplayCrossplaneSatisfiedByMaterialization(ctx, scopeID, generationID); err != nil || !scheduled {
		t.Fatalf("replay retrying item: scheduled=%t error=%v", scheduled, err)
	}
	readRow()
	if status != "retrying" || leaseOwner.Valid {
		t.Fatalf("row after replay = (%q, %q, %t), want retrying/lease still released", status, leaseOwner.String, debt)
	}
}

// crossplaneReplayInsertWinner runs a hook after the scheduler's first real
// UPDATE has found no row, before its conflict-aware INSERT. The hook inserts
// and claims the stable work item through a second real queue, making the
// otherwise timing-dependent insert winner deterministic.
type crossplaneReplayInsertWinner struct {
	postgres.SQLDB
	afterFirstUpdate func(context.Context) error
	called           bool
	firstUpdateRows  int64
}

func (d *crossplaneReplayInsertWinner) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	result, err := d.SQLDB.ExecContext(ctx, query, args...)
	if err != nil || d.called {
		return result, err
	}
	d.firstUpdateRows, err = result.RowsAffected()
	if err != nil {
		return nil, err
	}
	d.called = true
	if err := d.afterFirstUpdate(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func testCrossplaneRedriveConcurrentInsertWinnerKeepsReplayDebtLive(t *testing.T) {
	db, _ := crossplaneRedriveProofDB(t)
	ctx := context.Background()
	const (
		scopeID      = "scope-claim-insert-winner"
		generationID = "gen-claim-insert-winner"
	)
	seedCrossplaneRedriveClaimScope(ctx, t, db, scopeID, generationID, "database.example.com", "PostgreSQL", 1, time.Now().UTC())
	winnerQueue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "insert-winner", time.Minute)
	winnerQueue.ClaimDomain = reducer.DomainCrossplaneSatisfiedByMaterialization
	var winnerClaim reducercontract.Intent
	interleaver := &crossplaneReplayInsertWinner{SQLDB: postgres.SQLDB{DB: db}}
	interleaver.afterFirstUpdate = func(ctx context.Context) error {
		if scheduled, err := winnerQueue.ReplayCrossplaneSatisfiedByMaterialization(ctx, scopeID, generationID); err != nil || !scheduled {
			t.Fatalf("insert winner: scheduled=%t error=%v", scheduled, err)
		}
		claimed, ok, err := winnerQueue.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("claim insert winner: claimed=%t error=%v", ok, err)
		}
		winnerClaim = claimed
		return nil
	}
	replayQueue := postgres.NewReducerQueue(interleaver, "other-scheduler", time.Minute)
	scheduled, err := replayQueue.ReplayCrossplaneSatisfiedByMaterialization(ctx, scopeID, generationID)
	if err != nil || !scheduled {
		t.Fatalf("replay after insert winner: scheduled=%t error=%v", scheduled, err)
	}
	if !interleaver.called || interleaver.firstUpdateRows != 0 || winnerClaim.IntentID == "" {
		t.Fatalf("insert winner interleave = (called:%t, first update rows:%d, winner:%q), want true/zero/claimed", interleaver.called, interleaver.firstUpdateRows, winnerClaim.IntentID)
	}
	var status, leaseOwner string
	var replayRequired bool
	if err := db.QueryRowContext(ctx, `
		SELECT status, lease_owner, cross_scope_replay_required
		FROM fact_work_items WHERE work_item_id = $1
	`, winnerClaim.IntentID).Scan(&status, &leaseOwner, &replayRequired); err != nil {
		t.Fatalf("read winner after replay: %v", err)
	}
	if status != "claimed" || leaseOwner != "insert-winner" || !replayRequired {
		t.Fatalf("winner state = (%q, %q, %t), want claimed/insert-winner/replay debt", status, leaseOwner, replayRequired)
	}
	if err := winnerQueue.Ack(ctx, winnerClaim, reducercontract.Result{}); err != nil {
		t.Fatalf("ACK insert winner: %v", err)
	}
	assertCrossplaneRedriveWorkItemPending(ctx, t, db, scopeID, generationID)
}

// TestCrossplaneReplayClaimedRowCostLive measures the old public-method
// sequence and the fixed scheduler on the same claimed row and connection.
// It asserts state after each pair; timing is recorded for review, not gated.
func testCrossplaneReplayClaimedRowCostLive(t *testing.T) {
	db, _ := crossplaneRedriveProofDB(t)
	ctx := context.Background()
	const (
		scopeID      = "scope-claim-replay-cost"
		generationID = "gen-claim-replay-cost"
	)
	seedCrossplaneRedriveClaimScope(ctx, t, db, scopeID, generationID, "database.example.com", "PostgreSQL", 1, time.Now().UTC())
	queue := postgres.NewReducerQueue(postgres.SQLDB{DB: db}, "replay-cost-worker", time.Minute)
	queue.ClaimDomain = reducer.DomainCrossplaneSatisfiedByMaterialization
	if scheduled, err := queue.ReplayCrossplaneSatisfiedByMaterialization(ctx, scopeID, generationID); err != nil || !scheduled {
		t.Fatalf("seed replay-cost row: scheduled=%t error=%v", scheduled, err)
	}
	claimed, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim replay-cost row: claimed=%t error=%v", ok, err)
	}
	intent := runtime.ReducerIntent{
		ScopeID: scopeID, GenerationID: generationID,
		Domain:    reducer.DomainCrossplaneSatisfiedByMaterialization,
		EntityKey: "crossplane_satisfied_by_materialization:" + scopeID,
		Reason:    "crossplane redrive replay cost proof", SourceSystem: "reducer",
	}
	const pairs = 50
	baseline := make([]time.Duration, 0, pairs)
	fixed := make([]time.Duration, 0, pairs)
	resetDebt := func() {
		t.Helper()
		if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET cross_scope_replay_required = FALSE WHERE work_item_id = $1`, claimed.IntentID); err != nil {
			t.Fatalf("reset replay debt: %v", err)
		}
	}
	for i := 0; i < pairs; i++ {
		resetDebt()
		started := time.Now()
		reopened, err := queue.ReopenSucceeded(ctx, claimed.IntentID)
		if err != nil || reopened {
			t.Fatalf("legacy succeeded-only reopen on claimed row = (%t, %v)", reopened, err)
		}
		if _, err := queue.Enqueue(ctx, []runtime.ReducerIntent{intent}); err != nil {
			t.Fatalf("legacy conflict enqueue: %v", err)
		}
		baseline = append(baseline, time.Since(started))

		resetDebt()
		started = time.Now()
		scheduled, err := queue.ReplayCrossplaneSatisfiedByMaterialization(ctx, scopeID, generationID)
		if err != nil || !scheduled {
			t.Fatalf("fixed replay = (%t, %v)", scheduled, err)
		}
		fixed = append(fixed, time.Since(started))
		var status, leaseOwner string
		var debt bool
		if err := db.QueryRowContext(ctx, `SELECT status, lease_owner, cross_scope_replay_required FROM fact_work_items WHERE work_item_id = $1`, claimed.IntentID).Scan(&status, &leaseOwner, &debt); err != nil {
			t.Fatalf("read replay-cost row: %v", err)
		}
		if status != "claimed" || leaseOwner != "replay-cost-worker" || !debt {
			t.Fatalf("replay-cost row = (%q, %q, %t), want claimed/unchanged lease/debt", status, leaseOwner, debt)
		}
	}
	slices.Sort(baseline)
	slices.Sort(fixed)
	t.Logf("claimed-row replay, %d paired samples, one PostgreSQL 16 connection: legacy p50=%s fixed p50=%s; legacy p95=%s fixed p95=%s", pairs, baseline[pairs/2], fixed[pairs/2], baseline[pairs*95/100], fixed[pairs*95/100])
}

// TestCrossplaneRedriveTerminalTargetStaysClaimedForCatchUpLive pins the
// unscheduled-target branch live: a target whose stable work item is already
// terminal (dead-lettered through the production Fail path) makes the sweep
// return the 'not schedulable' error and leaves the state row 'claimed' --
// never silently completed or lost. The catch-up path then reclaims the row
// and reports the same per-claim error, proving the target stays
// reclaimable pending operator repair instead of vanishing.
func testCrossplaneRedriveTerminalTargetStaysClaimedForCatchUpLive(t *testing.T) {
	database, _ := crossplaneRedriveProofDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	const (
		claimScopeID      = "scope-claim-terminal-redrive"
		claimGenerationID = "gen-claim-terminal-redrive"
		xrdScopeID        = "scope-xrd-terminal-redrive"
		xrdGenerationID   = "gen-xrd-terminal-redrive"
		group             = "database.example.com"
		claimKind         = "PostgreSQL"
	)
	seedCrossplaneRedriveClaimScope(ctx, t, database, claimScopeID, claimGenerationID, group, claimKind, 1, now)
	queue := postgres.NewReducerQueue(postgres.SQLDB{DB: database}, "terminal-redrive-worker", time.Minute)
	queue.ClaimDomain = reducer.DomainCrossplaneSatisfiedByMaterialization
	if scheduled, err := queue.ReplayCrossplaneSatisfiedByMaterialization(ctx, claimScopeID, claimGenerationID); err != nil || !scheduled {
		t.Fatalf("enqueue Claim materialization: scheduled=%t error=%v", scheduled, err)
	}
	claimed, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim materialization: claimed=%t error=%v", ok, err)
	}
	// Drive the row terminal through the production failure entry point
	// (a non-retryable cause dead-letters), not a hand-written status flip.
	if err := queue.Fail(ctx, claimed, errors.New("terminal proof failure")); err != nil {
		t.Fatalf("fail claim item with terminal cause: %v", err)
	}
	var status string
	if err := database.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`, claimed.IntentID).Scan(&status); err != nil {
		t.Fatalf("read terminal work item: %v", err)
	}
	if status != "dead_letter" {
		t.Fatalf("work item status = %q, want dead_letter before the sweep", status)
	}

	seedCrossplaneRedriveXRD(ctx, t, database, xrdScopeID, xrdGenerationID, group, claimKind, now.Add(time.Minute))
	shortLease := 300 * time.Millisecond
	sweeper := crossplanestore.CrossplaneSatisfiedByRedriveSweeper{
		DB:            postgres.SQLQueryer{DB: database},
		State:         crossplanestore.NewCrossplaneRedriveStateStore(postgres.SQLDB{DB: database}),
		Replayer:      queue,
		Owner:         "projector",
		LeaseDuration: shortLease,
	}
	_, err = sweeper.Sweep(ctx, xrdScopeID, xrdGenerationID)
	if err == nil || !strings.Contains(err.Error(), "not schedulable") {
		t.Fatalf("sweep against terminal target = %v, want the 'not schedulable' error", err)
	}
	assertCrossplaneRedriveStateStatus(ctx, t, database, xrdScopeID, xrdGenerationID, "claimed")
	assertCrossplaneRedriveLedgerEntry(ctx, t, database, claimScopeID, group, claimKind, false)

	// Past the short lease the catch-up path reclaims the same row and
	// reports the same per-claim error: still 'claimed', never 'completed',
	// still no ledger entry -- reclaimable pending operator repair.
	time.Sleep(shortLease + 200*time.Millisecond)
	catchUpSweeper := crossplanestore.CrossplaneSatisfiedByRedriveSweeper{
		DB:            postgres.SQLQueryer{DB: database},
		State:         crossplanestore.NewCrossplaneRedriveStateStore(postgres.SQLDB{DB: database}),
		Replayer:      queue,
		Owner:         "projector",
		LeaseDuration: time.Minute,
	}
	if _, err := catchUpSweeper.SweepBatch(ctx, 10); err == nil || !strings.Contains(err.Error(), "not schedulable") {
		t.Fatalf("catch-up sweep against terminal target = %v, want the 'not schedulable' error", err)
	}
	assertCrossplaneRedriveStateStatus(ctx, t, database, xrdScopeID, xrdGenerationID, "claimed")
	assertCrossplaneRedriveLedgerEntry(ctx, t, database, claimScopeID, group, claimKind, false)
}

// crossplaneRedriveGateReplayer wraps a real replayer and blocks every call
// after blockAfter until release is closed, stalling one sweep mid-fan-out
// while holding no database connection (the page rows are drained before
// each replay call). It makes the stale-sweep interleaving deterministic:
// sweep A replays its first target, stalls on the second, and resumes only
// after sweeper B has reclaimed and completed the same generation.
type crossplaneRedriveGateReplayer struct {
	real       crossplanestore.CrossplaneRedriveIntentReplayer
	blockAfter int
	calls      atomic.Int64
	release    chan struct{}
}

func (g *crossplaneRedriveGateReplayer) ReplayCrossplaneSatisfiedByMaterialization(
	ctx context.Context, targetScopeID, targetGenerationID string,
) (bool, error) {
	if g.calls.Add(1) > int64(g.blockAfter) {
		select {
		case <-g.release:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	return g.real.ReplayCrossplaneSatisfiedByMaterialization(ctx, targetScopeID, targetGenerationID)
}

// TestCrossplaneRedriveStaleSweepReplayKeepsSingleSchedulingLive is the
// issue #8948 lease-fencing proof (arbiter verdict C: no production change).
// Sweep A stalls mid-fan-out; sweeper B reclaims the expired lease under the
// same static owner string, replays both targets, and completes. Stale A then
// resumes: its replay calls must not create a second work item per target,
// and its trailing completion must report the reclaimed outcome instead of
// overwriting B's completion. This is the first live assertion of
// OutcomeReclaimedMidSweep.
func testCrossplaneRedriveStaleSweepReplayKeepsSingleSchedulingLive(t *testing.T) {
	database, _ := crossplaneRedriveProofDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	const (
		xrdScopeID      = "scope-xrd-stale-sweep"
		xrdGenerationID = "gen-xrd-stale-sweep"
		group           = "database.example.com"
		claimKind       = "PostgreSQL"
	)
	targets := []string{"scope-claim-stale-a", "scope-claim-stale-b"}
	seedCrossplaneRedriveXRD(ctx, t, database, xrdScopeID, xrdGenerationID, group, claimKind, now)
	for i, targetScopeID := range targets {
		seedCrossplaneRedriveClaimScope(ctx, t, database, targetScopeID, targetScopeID+"-gen-001", group, claimKind, 1, now.Add(time.Duration(i)*time.Second))
	}

	realQueue := postgres.NewReducerQueue(postgres.SQLDB{DB: database}, "stale-sweep-worker", time.Minute)
	gate := &crossplaneRedriveGateReplayer{real: realQueue, blockAfter: 1, release: make(chan struct{})}
	shortLease := 300 * time.Millisecond
	staleSweeper := crossplanestore.CrossplaneSatisfiedByRedriveSweeper{
		DB:            postgres.SQLQueryer{DB: database},
		State:         crossplanestore.NewCrossplaneRedriveStateStore(postgres.SQLDB{DB: database}),
		Replayer:      gate,
		Owner:         "projector",
		LeaseDuration: shortLease,
	}
	type sweepOutcome struct {
		result crossplanestore.CrossplaneRedriveSweepResult
		err    error
	}
	staleCh := make(chan sweepOutcome, 1)
	go func() {
		result, err := staleSweeper.Sweep(ctx, xrdScopeID, xrdGenerationID)
		staleCh <- sweepOutcome{result: result, err: err}
	}()

	// Wait until sweep A has replayed its first target and stalled on the
	// second before letting its lease expire.
	deadline := time.Now().Add(10 * time.Second)
	for gate.calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls := gate.calls.Load(); calls < 2 {
		t.Fatalf("stale sweep never reached its second replay call (calls=%d)", calls)
	}
	time.Sleep(shortLease + 200*time.Millisecond)

	freshSweeper := crossplanestore.CrossplaneSatisfiedByRedriveSweeper{
		DB:            postgres.SQLQueryer{DB: database},
		State:         crossplanestore.NewCrossplaneRedriveStateStore(postgres.SQLDB{DB: database}),
		Replayer:      realQueue,
		Owner:         "projector",
		LeaseDuration: time.Minute,
	}
	results, err := freshSweeper.SweepBatch(ctx, 10)
	if err != nil {
		t.Fatalf("reclaiming SweepBatch: %v", err)
	}
	if len(results) != 1 || results[0].Outcome != crossplanestore.CrossplaneRedriveOutcomeCompletedForTest || results[0].TargetsEnqueued != 2 {
		t.Fatalf("reclaiming sweep = %+v, %v, want one completed sweep with 2 targets", results, err)
	}

	close(gate.release)
	var stale sweepOutcome
	select {
	case stale = <-staleCh:
	case <-time.After(30 * time.Second):
		t.Fatalf("stale sweep did not return after release")
	}
	if stale.err != nil {
		t.Fatalf("stale sweep error = %v, want a clean reclaimed outcome", stale.err)
	}
	if stale.result.Outcome != crossplanestore.CrossplaneRedriveOutcomeReclaimedMidSweepForTest {
		t.Fatalf("stale sweep outcome = %q, want %q", stale.result.Outcome, crossplanestore.CrossplaneRedriveOutcomeReclaimedMidSweepForTest)
	}

	// Exactly one stable work item per target: the stale replay must not
	// duplicate anything the reclaiming sweep scheduled.
	var count int
	if err := database.QueryRowContext(ctx, `
		SELECT count(*) FROM fact_work_items
		WHERE stage = 'reducer' AND domain = 'crossplane_satisfied_by_materialization'
		  AND scope_id IN ('scope-claim-stale-a', 'scope-claim-stale-b')
	`).Scan(&count); err != nil {
		t.Fatalf("count stable work items: %v", err)
	}
	if count != 2 {
		t.Fatalf("stable work items = %d, want exactly one per target after the stale interleaving", count)
	}
	assertCrossplaneRedriveStateStatus(ctx, t, database, xrdScopeID, xrdGenerationID, "completed")
	for _, targetScopeID := range targets {
		assertCrossplaneRedriveTargetPending(ctx, t, database, targetScopeID, targetScopeID+"-gen-001", true)
		assertCrossplaneRedriveLedgerEntry(ctx, t, database, targetScopeID, group, claimKind, false)
	}
}
