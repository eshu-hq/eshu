// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplanestore_test

import (
	"context"
	"database/sql"
	"slices"
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
	t.Run("concurrent_insert", testCrossplaneRedriveConcurrentInsertWinnerKeepsReplayDebtLive)
	t.Run("claimed_row_cost", testCrossplaneReplayClaimedRowCostLive)
}

func testCrossplaneSatisfiedByRedriveClosesXRDLagWindowLive(t *testing.T) {
	dsn, schema := crossplaneRedriveProofSchema(t)
	db := crossplaneRedriveProofConn(t, dsn, schema)
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
	dsn, schema := crossplaneRedriveProofSchema(t)
	db := crossplaneRedriveProofConn(t, dsn, schema)
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
	dsn, schema := crossplaneRedriveProofSchema(t)
	db := crossplaneRedriveProofConn(t, dsn, schema)
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
	dsn, schema := crossplaneRedriveProofSchema(t)
	db := crossplaneRedriveProofConn(t, dsn, schema)
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
