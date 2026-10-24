// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8946 disposable PostgreSQL 18 proofs that the security-alert org fan-out cursor commits with its generation, is read from the newest pending-or-active generation, stays put on a rollback or an unchanged-generation skip, is replaced on a replay, and survives two generations of one scope committing in parallel from the same start; each proof creates and drops its own database from the bootstrap schema; about 1.5 to 6 s per subtest on PostgreSQL 18.6 locally (database bootstrap dominates); wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/security/alerts/unread"
)

func fanoutCursorState(start, next string) securityalerts.TargetReadState {
	state := readStateUnreadState(150)
	state.FanoutStartRepository, state.FanoutNextRepository = start, next
	return state
}

// fanoutLapState is a read state that closed a lap at lap: the next claim
// starts at the top.
func fanoutLapState(start, lap string) securityalerts.TargetReadState {
	state := fanoutCursorState(start, "")
	state.FanoutLapRepository = lap
	return state
}

func fanoutCursorOf(t *testing.T, ctx context.Context, database *sql.DB) securityalerts.FanoutCursor {
	t.Helper()
	cursor, err := unread.NewCursorReader(postgres.SQLDB{DB: database}).FanoutCursor(ctx, readStateLiveScopeID)
	if err != nil {
		t.Fatalf("FanoutCursor() error = %v", err)
	}
	return cursor
}

func fanoutProofCommitsAndReadsTheCursor(t *testing.T) {
	database, ctx := readStateIsolatedLiveDB(t)
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	if got := fanoutCursorOf(t, ctx, database); got != (securityalerts.FanoutCursor{}) {
		t.Fatalf("cursor of a scope with no generation = %+v, want the top", got)
	}
	if err := readStateCommitGeneration(ctx, store, readStateClaimFor(t, ctx, database, "gen-1"), "gen-1", "hint-1",
		fanoutCursorState("", "acme/repo-100")); err != nil {
		t.Fatalf("commit gen-1: %v", err)
	}
	// Read before any activation work: the newest pending-or-active generation
	// carries the cursor, so a claim between commit and activation resumes
	// instead of re-reading the same window.
	if got := fanoutCursorOf(t, ctx, database); got.NextRepository != "acme/repo-100" {
		t.Fatalf("cursor after commit = %+v, want acme/repo-100", got)
	}
	if err := readStateCommitGeneration(ctx, store, readStateClaimFor(t, ctx, database, "gen-2"), "gen-2", "hint-2",
		fanoutCursorState("acme/repo-100", "acme/repo-050")); err != nil {
		t.Fatalf("commit gen-2: %v", err)
	}
	if got := fanoutCursorOf(t, ctx, database); got.NextRepository != "acme/repo-050" {
		t.Fatalf("cursor after the second generation = %+v, want the newest generation's acme/repo-050", got)
	}
	// A lap closes at the last position: the start returns to the top and the lap
	// repository is read back with it, while the next-repository column is NULL.
	if err := readStateCommitGeneration(ctx, store, readStateClaimFor(t, ctx, database, "gen-3"), "gen-3", "hint-3",
		fanoutLapState("acme/repo-050", "acme/repo-149")); err != nil {
		t.Fatalf("commit gen-3: %v", err)
	}
	if got, want := fanoutCursorOf(t, ctx, database), (securityalerts.FanoutCursor{LapRepository: "acme/repo-149"}); got != want {
		t.Fatalf("cursor after a lap closed = %+v, want %+v", got, want)
	}
	var start, next sql.NullString
	if err := database.QueryRowContext(ctx, `
SELECT fanout_start_repository, fanout_next_repository
FROM security_alert_target_read_states WHERE generation_id = 'gen-2'`).Scan(&start, &next); err != nil {
		t.Fatalf("read state row: %v", err)
	}
	if start.String != "acme/repo-100" || next.String != "acme/repo-050" {
		t.Errorf("stored cursor = %q -> %q, want acme/repo-100 -> acme/repo-050", start.String, next.String)
	}
}

func fanoutProofStartsAtTheTopWhenTheNewestGenerationHasNoReadState(t *testing.T) {
	database, ctx := readStateIsolatedLiveDB(t)
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	if err := readStateCommitGeneration(ctx, store, readStateClaimFor(t, ctx, database, "gen-1"), "gen-1", "hint-1",
		fanoutCursorState("", "acme/repo-100")); err != nil {
		t.Fatalf("commit gen-1: %v", err)
	}
	// A newer generation committed before the cursor existed (or by a repository
	// target) has no read-state row: the cursor must not fall through to the
	// older generation's value.
	if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint, status, observed_at, ingested_at)
VALUES ('gen-legacy', $1, 'snapshot', 'hint-legacy', 'pending', now(), now() + interval '1 minute')`, readStateLiveScopeID); err != nil {
		t.Fatalf("seed legacy generation: %v", err)
	}
	if got := fanoutCursorOf(t, ctx, database); got != (securityalerts.FanoutCursor{}) {
		t.Fatalf("cursor with a newer generation that has no read state = %+v, want the top", got)
	}
}

func fanoutProofRollbackLeavesTheCursorUnchanged(t *testing.T) {
	database, ctx := readStateIsolatedLiveDB(t)
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	if err := readStateCommitGeneration(ctx, store, readStateClaimFor(t, ctx, database, "gen-1"), "gen-1", "hint-1",
		fanoutCursorState("", "acme/repo-100")); err != nil {
		t.Fatalf("commit gen-1: %v", err)
	}
	// gen-2 fails its state validation inside the commit transaction, after the
	// scope_generations row was written and before the read-state upsert: the
	// whole transaction rolls back, and a surviving gen-2 generation row would
	// make the reader return "" instead of gen-1's cursor.
	bad := fanoutCursorState("acme/repo-100", "acme/repo-050")
	bad.Unread = []securityalerts.UnreadRepository{{Repository: "acme/a"}}
	if err := readStateCommitGeneration(ctx, store, readStateClaimFor(t, ctx, database, "gen-2"), "gen-2", "hint-2", bad); err == nil ||
		!strings.Contains(err.Error(), "needs a repository and a reason") {
		t.Fatalf("commit gen-2 error = %v, want the state validation error", err)
	}
	if got := fanoutCursorOf(t, ctx, database); got.NextRepository != "acme/repo-100" || got.LapRepository != "" {
		t.Fatalf("cursor after a rolled-back generation = %+v, want gen-1's acme/repo-100", got)
	}
}

func fanoutProofUnchangedGenerationSkipLeavesTheCursorUnchanged(t *testing.T) {
	database, ctx := readStateIsolatedLiveDB(t)
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	if err := readStateCommitGeneration(ctx, store, readStateClaimFor(t, ctx, database, "gen-1"), "gen-1", "hint-1",
		fanoutCursorState("", "acme/repo-100")); err != nil {
		t.Fatalf("commit gen-1: %v", err)
	}
	// Same freshness hint as the newest generation: the committer skips the
	// generation and never runs the state writer.
	if err := readStateCommitGeneration(ctx, store, readStateClaimFor(t, ctx, database, "gen-2"), "gen-2", "hint-1",
		fanoutCursorState("acme/repo-100", "acme/repo-050")); err != nil {
		t.Fatalf("commit gen-2: %v", err)
	}
	if got := readStateCount(t, ctx, database, `SELECT count(*) FROM scope_generations WHERE generation_id = 'gen-2'`); got != 0 {
		t.Fatalf("unchanged generation was stored (%d rows), want it skipped", got)
	}
	if got := fanoutCursorOf(t, ctx, database); got.NextRepository != "acme/repo-100" {
		t.Fatalf("cursor after a skipped generation = %+v, want gen-1's acme/repo-100", got)
	}
}

func fanoutProofReplayReplacesTheCursorColumns(t *testing.T) {
	database, ctx := readStateIsolatedLiveDB(t)
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	mutation := readStateClaimFor(t, ctx, database, "gen-1")
	if err := readStateCommitGeneration(ctx, store, mutation, "gen-1", "hint-a", fanoutCursorState("acme/repo-010", "acme/repo-110")); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	first := fanoutCursorState("acme/repo-010", "acme/repo-110")
	first.FanoutLapRepository = "acme/repo-149"
	if err := readStateCommitGeneration(ctx, store, mutation, "gen-1", "hint-a2", first); err != nil {
		t.Fatalf("first commit with a lap: %v", err)
	}
	if err := readStateCommitGeneration(ctx, store, mutation, "gen-1", "hint-b", fanoutCursorState("", "acme/repo-200")); err != nil {
		t.Fatalf("replay commit: %v", err)
	}
	var start, next, lap sql.NullString
	if err := database.QueryRowContext(ctx, `
SELECT fanout_start_repository, fanout_next_repository, fanout_lap_repository
FROM security_alert_target_read_states WHERE generation_id = 'gen-1'`).Scan(&start, &next, &lap); err != nil {
		t.Fatalf("read state row: %v", err)
	}
	if start.Valid || next.String != "acme/repo-200" || lap.Valid {
		t.Errorf("cursor after replay = %+v -> %+v lap %+v, want NULL -> acme/repo-200 with no lap (the replay replaces all three columns)", start, next, lap)
	}
}

func fanoutProofParallelGenerationsFromTheSameStartKeepTheirCursor(t *testing.T) {
	database, ctx := readStateIsolatedLiveDB(t)
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	if err := readStateCommitGeneration(ctx, store, readStateClaimFor(t, ctx, database, "gen-0"), "gen-0", "hint-0",
		fanoutCursorState("", "acme/repo-100")); err != nil {
		t.Fatalf("commit gen-0: %v", err)
	}
	// Two replicas read the same cursor and race: the lease fence admits both
	// commits here (the planner and the lease make that rare, not impossible),
	// they carry the same next value, and neither loses its row.
	ids := []string{"gen-a", "gen-b"}
	mutations := make([]readStateClaimMutation, len(ids))
	for i, id := range ids {
		mutations[i] = readStateClaimFor(t, ctx, database, id)
	}
	var wg sync.WaitGroup
	errs := make([]error, len(ids))
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = readStateCommitGeneration(ctx, store, mutations[i], id, "hint-"+id,
				fanoutCursorState("acme/repo-100", "acme/repo-050"))
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("parallel commit %s: %v", ids[i], err)
		}
	}
	if got := readStateCount(t, ctx, database,
		`SELECT count(*) FROM security_alert_target_read_states WHERE fanout_next_repository = 'acme/repo-050' AND fanout_start_repository = 'acme/repo-100'`); got != 2 {
		t.Fatalf("rows carrying the shared cursor = %d, want both generations", got)
	}
	if got := fanoutCursorOf(t, ctx, database); got.NextRepository != "acme/repo-050" {
		t.Fatalf("cursor after the race = %+v, want acme/repo-050", got)
	}
}

func fanoutProofCursorReadIsAnIndexRangeOnTheScope(t *testing.T) {
	database, ctx := readStateIsolatedLiveDB(t)
	// 50 org targets with 4 generations each, the scale the #8943 plan proof
	// uses, each generation carrying a read state with a cursor, among 5,000
	// scopes with 4 generations each (20,000 generations and read-state rows):
	// a planner that picks a sequential scan on a handful of rows would not
	// be representative.
	for _, statement := range []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status)
SELECT 'target-' || t, 'security_alert', 'security_alert', 'target-' || t, 'security_alert', 'p', now(), now(), 'active'
FROM generate_series(1, 5000) AS t`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, freshness_hint, status, observed_at, ingested_at)
SELECT 'gen-' || t || '-' || g, 'target-' || t, 'snapshot', 'hint-' || t || '-' || g,
       CASE WHEN g = 4 THEN 'active' ELSE 'superseded' END, now(), now() + g * interval '1 minute'
FROM generate_series(1, 5000) AS t, generate_series(1, 4) AS g`,
		`INSERT INTO security_alert_target_read_states
    (generation_id, scope_id, org_read_truncated, repositories_total, unread_total, observed_at,
     fanout_start_repository, fanout_next_repository)
SELECT 'gen-' || t || '-' || g, 'target-' || t, TRUE, 2005, 0, now(), 'acme/repo-' || g, 'acme/repo-' || (g + 100)
FROM generate_series(1, 5000) AS t, generate_series(1, 4) AS g`,
		`VACUUM (ANALYZE) scope_generations, security_alert_target_read_states`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	rows, err := database.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+unread.FanoutCursorQuery, "target-7")
	if err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	text := plan.String()
	t.Logf("EXPLAIN (ANALYZE, BUFFERS), 5,000 scopes x 4 generations, each with a read state, one scope:\n%s", text)
	if strings.Contains(text, "Seq Scan on scope_generations") {
		t.Fatalf("cursor read scans scope_generations sequentially:\n%s", text)
	}
	if !strings.Contains(text, "security_alert_target_read_states_pkey") ||
		strings.Contains(text, "Seq Scan on security_alert_target_read_states") {
		t.Fatalf("cursor read does not look the read state up by primary key:\n%s", text)
	}
	if !strings.Contains(text, "Index") || !strings.Contains(text, "scope_generations") {
		t.Fatalf("cursor read does not use an index on scope_generations:\n%s", text)
	}
}

// TestSecurityAlertFanoutCursorLive runs the fan-out cursor proofs as
// subtests, so the readiness shards price them as one test: each subtest still
// creates and drops its own database.
func TestSecurityAlertFanoutCursorLive(t *testing.T) {
	t.Run("CommitsAndReadsTheCursorOfTheNewestGeneration", fanoutProofCommitsAndReadsTheCursor)
	t.Run("StartsAtTheTopWhenTheNewestGenerationHasNoReadState", fanoutProofStartsAtTheTopWhenTheNewestGenerationHasNoReadState)
	t.Run("RollbackLeavesTheCursorUnchanged", fanoutProofRollbackLeavesTheCursorUnchanged)
	t.Run("UnchangedGenerationSkipLeavesTheCursorUnchanged", fanoutProofUnchangedGenerationSkipLeavesTheCursorUnchanged)
	t.Run("ReplayReplacesTheCursorColumns", fanoutProofReplayReplacesTheCursorColumns)
	t.Run("ParallelGenerationsFromTheSameStartKeepTheirCursor", fanoutProofParallelGenerationsFromTheSameStartKeepTheirCursor)
	t.Run("CursorReadIsAnIndexRangeOnTheScope", fanoutProofCursorReadIsAnIndexRangeOnTheScope)
}
