// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
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
func TestCrossplaneSatisfiedByRedriveClosesXRDLagWindowLive(t *testing.T) {
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

	factStore := NewFactStore(SQLDB{DB: db})
	handler := reducer.CrossplaneSatisfiedByMaterializationHandler{
		FactLoader: factStore,
		EdgeWriter: &crossplaneRedriveSpyEdgeWriter{},
	}
	claimIntent := reducer.Intent{
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
	reducerQueue := NewReducerQueue(SQLDB{DB: db}, "test-owner", time.Minute)
	sweeper := CrossplaneSatisfiedByRedriveSweeper{
		DB:           SQLQueryer{DB: db},
		State:        NewCrossplaneRedriveStateStore(SQLDB{DB: db}),
		TargetLedger: NewCrossplaneRedriveTargetLedgerStore(SQLDB{DB: db}),
		Replayer:     reducerQueue,
		Owner:        "test-owner",
	}
	result, err := sweeper.Sweep(ctx, xrdScopeID, xrdGenerationID)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !result.Attempted {
		t.Fatalf("expected the sweep to attempt real work, got %+v", result)
	}
	if result.Outcome != crossplaneRedriveOutcomeCompleted {
		t.Fatalf("expected sweep outcome %q, got %q", crossplaneRedriveOutcomeCompleted, result.Outcome)
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
	greenHandler := reducer.CrossplaneSatisfiedByMaterializationHandler{
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
