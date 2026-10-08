// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/query/admin/store"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Live reopen proof: seed a disposable Postgres database, drive POST
// /api/v0/admin/reopen through the real store, and prove that a reopened row
// is claimable through the production claim path and that a repeated
// idempotency key is a no-op. Enrolled in the live-postgres-readiness
// runner, which sets ESHU_ADMIN_REOPEN_PROOF_DSN (administrative database)
// and ESHU_ADMIN_REOPEN_PROOF_DISPOSABLE=1; without a DSN it skips. Run it
// locally with, for example:
//
//	ESHU_ADMIN_REOPEN_PROOF_DSN=postgres://eshu:eshu@127.0.0.1:<port>/postgres?sslmode=disable \
//	ESHU_ADMIN_REOPEN_PROOF_DISPOSABLE=1 \
//	go test ./internal/query/admin/ -run TestAdminHandler_ReopenLive -count=1
func TestAdminHandler_ReopenLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_ADMIN_REOPEN_PROOF_DSN"),
		os.Getenv("ESHU_ADMIN_REOPEN_PROOF_DISPOSABLE"),
		3*time.Minute,
	)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	// Full production schema on the disposable database: hand-picked
	// migrations would drift from the columns the claim/ack paths read.
	if err := pgstatus.ApplyBootstrapWithoutContentSearchIndexes(ctx, pgstatus.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}

	suffix := time.Now().UTC().UnixNano()
	scopeID := fmt.Sprintf("scope-reopen-live-%d", suffix)
	sourceKey := fmt.Sprintf("repo-reopen-live-%d", suffix)
	generationID := fmt.Sprintf("generation-reopen-live-%d", suffix)
	staleGenerationID := fmt.Sprintf("generation-reopen-stale-%d", suffix)
	workA := fmt.Sprintf("w-reopen-a-%d", suffix)
	workB := fmt.Sprintf("w-reopen-b-%d", suffix)
	workPin := fmt.Sprintf("w-reopen-pin-%d", suffix)
	workClaimed := fmt.Sprintf("w-reopen-claimed-%d", suffix)
	workStale := fmt.Sprintf("w-reopen-stale-%d", suffix)
	unit1 := fmt.Sprintf("repo:reopen/u1-%d", suffix)
	unit2 := fmt.Sprintf("repo:reopen/u2-%d", suffix)
	unit3 := fmt.Sprintf("repo:reopen/u3-%d", suffix)
	run1 := fmt.Sprintf("run-reopen-1-%d", suffix)
	run2 := fmt.Sprintf("run-reopen-2-%d", suffix)
	runOld := fmt.Sprintf("run-reopen-old-%d", suffix)
	intentOld := fmt.Sprintf("i-reopen-old-%d", suffix)
	intentNew := fmt.Sprintf("i-reopen-new-%d", suffix)
	intentOldRun := fmt.Sprintf("i-reopen-oldrun-%d", suffix)
	intent2 := fmt.Sprintf("i-reopen-2-%d", suffix)
	intent3 := fmt.Sprintf("i-reopen-3-%d", suffix)
	intentPending := fmt.Sprintf("i-reopen-pending-%d", suffix)
	unit4 := fmt.Sprintf("repo:reopen/u4-%d", suffix)
	run4 := fmt.Sprintf("run-reopen-4-%d", suffix)
	intentStale := fmt.Sprintf("i-reopen-stale-%d", suffix)
	keyReducer := fmt.Sprintf("reopen-live-reducer-%d", suffix)
	keyIntent := fmt.Sprintf("reopen-live-intent-%d", suffix)

	// No explicit cleanup: the database is disposable and dropped by the helper.
	seedReopenLiveFixture(t, ctx, db, reopenLiveIDs{
		scopeID: scopeID, sourceKey: sourceKey, generationID: generationID,
		staleGenerationID: staleGenerationID, workA: workA, workB: workB,
		workPin: workPin, workClaimed: workClaimed, workStale: workStale,
		unit1: unit1, unit2: unit2, unit3: unit3, run1: run1, run2: run2,
		runOld: runOld, intentOld: intentOld, intentNew: intentNew,
		intentOldRun: intentOldRun, intent2: intent2,
		intent3: intent3, intentPending: intentPending,
		unit4: unit4, run4: run4, intentStale: intentStale,
	})

	h := &admin.Handler{Store: store.NewStore(db), Audit: &testutil.FakeGovernanceAuditAppender{}}
	mux := testutil.MountAdminHandler(h)

	// Phase 1: reopen the workload_materialization domain, selecting the
	// scope by its source key rather than the raw scope id.
	w := testutil.PostJSON(mux, "/api/v0/admin/reopen", map[string]any{
		"domain":          "workload_materialization",
		"scope_id":        sourceKey,
		"reason":          "live proof: re-drive the repair targets",
		"idempotency_key": keyReducer,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("reopen reducer status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got := testutil.DecodeResponseBody(t, w)
	if got["generation_id"] != generationID {
		t.Fatalf("generation_id = %v, want the active generation %s", got["generation_id"], generationID)
	}
	if got["reopened_reducer_count"] != float64(2) {
		t.Fatalf("reopened_reducer_count = %v, want 2: %s", got["reopened_reducer_count"], w.Body.String())
	}
	ids := reopenLiveStrings(t, got["reopened_reducer_ids"])
	if len(ids) != 2 || ids[0] != workA || ids[1] != workB {
		t.Fatalf("reopened_reducer_ids = %v, want [%s %s]", ids, workA, workB)
	}

	// Phase 2: the reopened rows carry the full claim-path reset, and rows
	// outside the domain, generation, or succeeded status are untouched.
	assertReopenLiveRowReset(t, ctx, db, workA)
	assertReopenLiveRowReset(t, ctx, db, workB)
	assertReopenLiveRowStatus(t, ctx, db, workPin, "succeeded")
	assertReopenLiveRowStatus(t, ctx, db, workClaimed, "claimed")
	assertReopenLiveRowStatus(t, ctx, db, workStale, "succeeded")

	// Phase 3: both reopened rows are claimable through the production
	// claim path, one at a time on their shared conflict key. The claim
	// orders by (updated_at, work_item_id); the reopen stamped both rows
	// at once, so the smaller id claims first, deterministically.
	queue := pgstatus.NewReducerQueue(pgstatus.SQLDB{DB: db}, "reopen-live", time.Minute)
	intent, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("claim reopened row: %v", err)
	}
	if !ok || intent.IntentID != workA {
		t.Fatalf("first claim = %q ok=%v, want %q", intent.IntentID, ok, workA)
	}
	if err := queue.Ack(ctx, intent, reducer.Result{IntentID: intent.IntentID, Domain: intent.Domain}); err != nil {
		t.Fatalf("ack first claim: %v", err)
	}
	intent, ok, err = queue.Claim(ctx)
	if err != nil {
		t.Fatalf("claim second reopened row: %v", err)
	}
	if !ok || intent.IntentID != workB {
		t.Fatalf("second claim = %q ok=%v, want %q", intent.IntentID, ok, workB)
	}
	if err := queue.Ack(ctx, intent, reducer.Result{IntentID: intent.IntentID, Domain: intent.Domain}); err != nil {
		t.Fatalf("ack second claim: %v", err)
	}

	// Phase 4: reopen the repo_dependency domain by canonical scope id. One
	// row per acceptance unit reopens: the newest completed row of U1 (the
	// older completed row stays completed), the single row of U2, and the
	// superseded-generation row of U4 (intent selection follows the
	// accepted run across generations). The unit without an acceptance row
	// and the already-pending intent are untouched.
	w = testutil.PostJSON(mux, "/api/v0/admin/reopen", map[string]any{
		"domain":          "repo_dependency",
		"scope_id":        scopeID,
		"reason":          "live proof: re-drive the acceptance units",
		"idempotency_key": keyIntent,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("reopen intent status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got = testutil.DecodeResponseBody(t, w)
	if got["reopened_intent_count"] != float64(3) {
		t.Fatalf("reopened_intent_count = %v, want 3: %s", got["reopened_intent_count"], w.Body.String())
	}
	if got["generation_id"] != generationID {
		t.Fatalf("generation_id = %v, want active generation %s", got["generation_id"], generationID)
	}
	units, ok := got["reopened_units"].([]any)
	if !ok || len(units) != 3 {
		t.Fatalf("reopened_units = %v, want 3 units", got["reopened_units"])
	}
	byUnit := map[string]map[string]any{}
	for _, raw := range units {
		unit := raw.(map[string]any)
		byUnit[unit["acceptance_unit_id"].(string)] = unit
	}
	if byUnit[unit1]["source_run_id"] != run1 || byUnit[unit1]["intent_id"] != intentNew {
		t.Fatalf("unit1 = %v, want accepted run %s with newest intent %s", byUnit[unit1], run1, intentNew)
	}
	if byUnit[unit2]["source_run_id"] != run2 || byUnit[unit2]["intent_id"] != intent2 {
		t.Fatalf("unit2 = %v, want accepted run %s with intent %s", byUnit[unit2], run2, intent2)
	}
	if byUnit[unit4]["source_run_id"] != run4 || byUnit[unit4]["intent_id"] != intentStale {
		t.Fatalf("unit4 = %v, want accepted run %s with stale intent %s", byUnit[unit4], run4, intentStale)
	}
	assertReopenLiveIntentPending(t, ctx, db, intentNew, true)
	assertReopenLiveIntentPending(t, ctx, db, intentOld, false)
	assertReopenLiveIntentPending(t, ctx, db, intentOldRun, false)
	assertReopenLiveIntentPending(t, ctx, db, intent3, false)
	assertReopenLiveIntentPending(t, ctx, db, intentPending, true)
	assertReopenLiveIntentPending(t, ctx, db, intentStale, true)

	// Phase 5: repeating the reducer call with the same key is a no-op. Both
	// rows are succeeded again after the phase-3 Acks, so a re-execution
	// would flip them back to pending; the duplicate must leave them alone
	// and report the original outcome.
	w = testutil.PostJSON(mux, "/api/v0/admin/reopen", map[string]any{
		"domain":          "workload_materialization",
		"scope_id":        sourceKey,
		"reason":          "live proof: re-drive the repair targets",
		"idempotency_key": keyReducer,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("duplicate reopen status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	got = testutil.DecodeResponseBody(t, w)
	if got["duplicate"] != true {
		t.Fatalf("duplicate = %v, want true", got["duplicate"])
	}
	if got["reopened_total_count"] != float64(2) {
		t.Fatalf("reopened_total_count = %v, want the original 2", got["reopened_total_count"])
	}
	assertReopenLiveRowStatus(t, ctx, db, workA, "succeeded")
	assertReopenLiveRowStatus(t, ctx, db, workB, "succeeded")
}

// reopenLiveIDs carries the unique fixture identifiers for one live run.
type reopenLiveIDs struct {
	scopeID, sourceKey, generationID, staleGenerationID string
	workA, workB, workPin, workClaimed, workStale       string
	unit1, unit2, unit3, run1, run2, runOld             string
	unit4, run4, intentStale                            string
	intentOld, intentNew, intentOldRun                  string
	intent2, intent3, intentPending                     string
}

// seedReopenLiveFixture inserts the scope (with the active generation
// pinned), two generations, five reducer rows, six intents, and four
// acceptance rows. Succeeded reducer rows carry a stale lease-free shape
// with attempt history; the claimed row holds a live lease.
func seedReopenLiveFixture(t *testing.T, ctx context.Context, db *sql.DB, ids reopenLiveIDs) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	past := now.Add(-time.Hour)
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'repository', 'git', $2, 'git', $2, $3, $3, 'active', '{}'::jsonb)
`, ids.scopeID, ids.sourceKey, now); err != nil {
		t.Fatalf("insert scope: %v", err)
	}
	for _, gen := range []struct {
		id       string
		status   string
		observed time.Time
	}{{
		id: ids.staleGenerationID, status: "superseded", observed: past,
	}, {
		id: ids.generationID, status: "active", observed: now,
	}} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload
) VALUES ($1, $2, 'test', $3, $3, $4, '{}'::jsonb)
`, gen.id, ids.scopeID, gen.observed, gen.status); err != nil {
			t.Fatalf("insert generation %s: %v", gen.id, err)
		}
	}
	// Pin the active generation: without it the claim path's supersede sweep
	// would terminalize the reopened rows instead of claiming them.
	if _, err := db.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, ids.scopeID, ids.generationID); err != nil {
		t.Fatalf("pin active generation: %v", err)
	}
	succeeded := []struct{ id, domain, generation string }{
		{ids.workA, "workload_materialization", ids.generationID},
		{ids.workB, "workload_materialization", ids.generationID},
		{ids.workPin, "submodule_pin", ids.generationID},
		{ids.workStale, "workload_materialization", ids.staleGenerationID},
	}
	for _, row := range succeeded {
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, created_at, updated_at, payload
) VALUES ($1, $2, $3, 'reducer', $4, 'succeeded', 3, $5, $5, '{}'::jsonb)
`, row.id, ids.scopeID, row.generation, row.domain, past); err != nil {
			t.Fatalf("insert succeeded row %s: %v", row.id, err)
		}
	}
	// The claimed row sits on its own conflict key: on the scope's default key
	// its live lease would fence the reopened rows out of the claim phase.
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, conflict_key, status,
    attempt_count, lease_owner, claim_until, created_at, updated_at, payload
) VALUES ($1, $2, $3, 'reducer', 'workload_materialization', 'reopen-live-other', 'claimed',
    1, 'some-worker', $4, $5, $5, '{}'::jsonb)
`, ids.workClaimed, ids.scopeID, ids.generationID, now.Add(time.Hour), past); err != nil {
		t.Fatalf("insert claimed row: %v", err)
	}
	intents := []struct {
		id, unit, run, repository string
		created, completed        *time.Time
	}{
		{ids.intentOld, ids.unit1, ids.run1, "repo-old", timePtr(past.Add(-time.Hour)), timePtr(past)},
		{ids.intentNew, ids.unit1, ids.run1, "repo-new", timePtr(past), timePtr(now)},
		{ids.intentOldRun, ids.unit1, ids.runOld, "repo-oldrun", timePtr(past), timePtr(now)},
		{ids.intent2, ids.unit2, ids.run2, "repo-2", timePtr(past), timePtr(now)},
		{ids.intent3, ids.unit3, "run-orphan", "repo-3", timePtr(past), timePtr(now)},
		{ids.intentPending, ids.unit2, ids.run2, "repo-pending", timePtr(now), nil},
	}
	for _, intent := range intents {
		if _, err := db.ExecContext(ctx, `
INSERT INTO shared_projection_intents (
    intent_id, projection_domain, partition_key, scope_id, acceptance_unit_id,
    repository_id, source_run_id, generation_id, payload, created_at, completed_at
) VALUES ($1, 'repo_dependency', 'p0', $2, $3, $4, $5, $6, '{}'::jsonb, $7, $8)
`, intent.id, ids.scopeID, intent.unit, intent.repository, intent.run, ids.generationID, intent.created, nullTime(intent.completed)); err != nil {
			t.Fatalf("insert intent %s: %v", intent.id, err)
		}
	}
	// U4's only intent was written by the superseded generation: its
	// acceptance is still the newest for the unit, so intent selection
	// (which follows the accepted run, not the active generation) must
	// reopen it while reporting the active generation.
	if _, err := db.ExecContext(ctx, `
INSERT INTO shared_projection_intents (
    intent_id, projection_domain, partition_key, scope_id, acceptance_unit_id,
    repository_id, source_run_id, generation_id, payload, created_at, completed_at
) VALUES ($1, 'repo_dependency', 'p0', $2, $3, 'repo-stale', $4, $5, '{}'::jsonb, $6, $7)
`, ids.intentStale, ids.scopeID, ids.unit4, ids.run4, ids.staleGenerationID, past, now); err != nil {
		t.Fatalf("insert stale intent %s: %v", ids.intentStale, err)
	}
	// U1 carries two accepted runs so the phase-4 assertion pins the
	// newest-accepted tie-break: the reopen must report run1, not runOld.
	for _, acc := range []struct {
		unit, run string
		at        time.Time
	}{
		{ids.unit1, ids.run1, now},
		{ids.unit2, ids.run2, now},
		{ids.unit1, ids.runOld, past},
		{ids.unit4, ids.run4, now},
	} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO shared_projection_acceptance (
    scope_id, acceptance_unit_id, source_run_id, generation_id, accepted_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $5)
`, ids.scopeID, acc.unit, acc.run, ids.generationID, acc.at); err != nil {
			t.Fatalf("insert acceptance %s/%s: %v", acc.unit, acc.run, err)
		}
	}
}

func timePtr(t time.Time) *time.Time { return &t }

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

// reopenLiveStrings decodes a JSON string array from a response field.
func reopenLiveStrings(t *testing.T, raw any) []string {
	t.Helper()
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected a string array, got %T (%v)", raw, raw)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("expected strings, got %T (%v)", item, item)
		}
		out = append(out, text)
	}
	return out
}

// assertReopenLiveRowReset checks the full claim-path reset on a reopened row.
func assertReopenLiveRowReset(t *testing.T, ctx context.Context, db *sql.DB, workItemID string) {
	t.Helper()
	var (
		status                   string
		attempts                 int
		leaseOwner               sql.NullString
		claimUntil               sql.NullTime
		visibleAt, reopenedAt    sql.NullTime
		nextAttempt              sql.NullTime
		failureClass, failureMsg sql.NullString
		failureDetails           sql.NullString
		identityV2, identityV3   string
	)
	if err := db.QueryRowContext(ctx, `
SELECT status, attempt_count, lease_owner, claim_until, visible_at,
       next_attempt_at, failure_class, failure_message, failure_details,
       container_image_identity_v2_authorized_status,
       container_image_identity_v3_authorized_status, reopened_at
FROM fact_work_items WHERE work_item_id = $1
`, workItemID).Scan(
		&status, &attempts, &leaseOwner, &claimUntil, &visibleAt,
		&nextAttempt, &failureClass, &failureMsg, &failureDetails,
		&identityV2, &identityV3, &reopenedAt,
	); err != nil {
		t.Fatalf("read reopened row %s: %v", workItemID, err)
	}
	if status != "pending" || attempts != 0 {
		t.Fatalf("row %s: status=%s attempts=%d, want pending/0", workItemID, status, attempts)
	}
	if leaseOwner.Valid || claimUntil.Valid {
		t.Fatalf("row %s: lease not cleared: %v %v", workItemID, leaseOwner, claimUntil)
	}
	if !visibleAt.Valid || !reopenedAt.Valid {
		t.Fatalf("row %s: visible_at/reopened_at not stamped", workItemID)
	}
	if nextAttempt.Valid || failureClass.Valid || failureMsg.Valid || failureDetails.Valid {
		t.Fatalf("row %s: retry/failure state not cleared", workItemID)
	}
	if identityV2 != "" || identityV3 != "" {
		t.Fatalf("row %s: identity statuses=%q/%q, want empty (not required)", workItemID, identityV2, identityV3)
	}
}

// assertReopenLiveRowStatus checks a row the reopen must not have touched.
func assertReopenLiveRowStatus(t *testing.T, ctx context.Context, db *sql.DB, workItemID, want string) {
	t.Helper()
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM fact_work_items WHERE work_item_id = $1`, workItemID).Scan(&status); err != nil {
		t.Fatalf("read row %s: %v", workItemID, err)
	}
	if status != want {
		t.Fatalf("row %s: status=%s, want %s", workItemID, status, want)
	}
}

// assertReopenLiveIntentPending checks an intent's completed_at state: pending
// means completed_at IS NULL.
func assertReopenLiveIntentPending(t *testing.T, ctx context.Context, db *sql.DB, intentID string, wantPending bool) {
	t.Helper()
	var completedAt sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT completed_at FROM shared_projection_intents WHERE intent_id = $1`, intentID).Scan(&completedAt); err != nil {
		t.Fatalf("read intent %s: %v", intentID, err)
	}
	if wantPending && completedAt.Valid {
		t.Fatalf("intent %s: completed_at=%v, want pending", intentID, completedAt.Time)
	}
	if !wantPending && !completedAt.Valid {
		t.Fatalf("intent %s: pending, want still completed", intentID)
	}
}
