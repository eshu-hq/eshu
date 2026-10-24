// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8943 disposable PostgreSQL 18 proofs that the security-alert org read state commits with its generation through the claim-fenced ingestion store, is replaced on a replay of a pending generation, rolls back with a failed generation, and leaves with the generation when generation retention prunes it; each proof creates and drops its own database from the bootstrap schema; under 1 s per proof on PostgreSQL 18.6 locally; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func readStateProofCommitPersistsReadStateWithGeneration(t *testing.T) {
	database, ctx := readStateIsolatedLiveDB(t)
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	mutation := readStateClaimFor(t, ctx, database, "gen-1")

	state := readStateUnreadState(5,
		securityalerts.UnreadRepository{Repository: "acme/b", Reason: "org_repository_read_rate_limited"},
		securityalerts.UnreadRepository{Repository: "acme/a", Reason: "org_repository_fanout_cap_reached"},
	)
	if err := readStateCommitGeneration(ctx, store, mutation, "gen-1", "hint-1", state); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var truncated bool
	var total, unread int
	if err := database.QueryRowContext(ctx, `
SELECT org_read_truncated, repositories_total, unread_total
FROM security_alert_target_read_states WHERE generation_id = 'gen-1' AND scope_id = $1`, readStateLiveScopeID,
	).Scan(&truncated, &total, &unread); err != nil {
		t.Fatalf("read state row: %v", err)
	}
	if !truncated || total != 5 || unread != 2 {
		t.Errorf("state row = truncated %v total %d unread %d, want true 5 2", truncated, total, unread)
	}
	if got := readStateCount(t, ctx, database, `SELECT count(*) FROM security_alert_unread_repositories WHERE generation_id = 'gen-1'`); got != 2 {
		t.Errorf("unread rows = %d, want 2", got)
	}
	var reason string
	if err := database.QueryRowContext(ctx, `
SELECT reason FROM security_alert_unread_repositories WHERE generation_id = 'gen-1' AND repository = 'acme/a'`,
	).Scan(&reason); err != nil || reason != "org_repository_fanout_cap_reached" {
		t.Errorf("reason for acme/a = %q (err %v), want org_repository_fanout_cap_reached", reason, err)
	}
}

func readStateProofReplayOfPendingGenerationReplacesUnreadRepositories(t *testing.T) {
	database, ctx := readStateIsolatedLiveDB(t)
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	mutation := readStateClaimFor(t, ctx, database, "gen-1")

	first := readStateUnreadState(4,
		securityalerts.UnreadRepository{Repository: "acme/a", Reason: "org_repository_read_failed"},
		securityalerts.UnreadRepository{Repository: "acme/b", Reason: "org_repository_read_failed"},
		securityalerts.UnreadRepository{Repository: "acme/c", Reason: "org_repository_read_failed"},
	)
	if err := readStateCommitGeneration(ctx, store, mutation, "gen-1", "hint-a", first); err != nil {
		t.Fatalf("first commit: %v", err)
	}
	// The same generation id commits again with a different freshness hint, as
	// a retried claim whose read changed does. The second set replaces the
	// first: nothing from the first set may survive.
	second := readStateUnreadState(4, securityalerts.UnreadRepository{Repository: "acme/d", Reason: "org_repository_read_rate_limited"})
	if err := readStateCommitGeneration(ctx, store, mutation, "gen-1", "hint-b", second); err != nil {
		t.Fatalf("replay commit: %v", err)
	}
	if got := readStateCount(t, ctx, database, `SELECT count(*) FROM security_alert_target_read_states WHERE generation_id = 'gen-1'`); got != 1 {
		t.Errorf("state rows = %d, want 1", got)
	}
	if got := readStateCount(t, ctx, database, `SELECT count(*) FROM security_alert_unread_repositories WHERE generation_id = 'gen-1' AND repository = 'acme/d'`); got != 1 {
		t.Errorf("replacement rows = %d, want acme/d present", got)
	}
	if got := readStateCount(t, ctx, database, `SELECT count(*) FROM security_alert_unread_repositories WHERE generation_id = 'gen-1'`); got != 1 {
		t.Errorf("unread rows after replay = %d, want exactly 1 (stale rows must not survive)", got)
	}
	if got := readStateCount(t, ctx, database, `SELECT unread_total FROM security_alert_target_read_states WHERE generation_id = 'gen-1'`); got != 1 {
		t.Errorf("unread_total after replay = %d, want 1", got)
	}
}

func readStateProofFailedStateWriteRollsBackTheGeneration(t *testing.T) {
	database, ctx := readStateIsolatedLiveDB(t)
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	mutation := readStateClaimFor(t, ctx, database, "gen-1")

	// An unread repository without a reason fails validation inside the commit
	// transaction, after the generation row was written.
	bad := readStateUnreadState(2, securityalerts.UnreadRepository{Repository: "acme/a"})
	if err := readStateCommitGeneration(ctx, store, mutation, "gen-1", "hint-1", bad); err == nil {
		t.Fatal("commit error = nil, want the state validation error")
	} else if !strings.Contains(err.Error(), "needs a repository and a reason") {
		t.Fatalf("commit error = %v, want the state validation error", err)
	}
	for _, table := range []string{"scope_generations", "security_alert_target_read_states", "security_alert_unread_repositories", "fact_records"} {
		if got := readStateCount(t, ctx, database, `SELECT count(*) FROM `+table+` WHERE generation_id = 'gen-1'`); got != 0 {
			t.Errorf("%s rows after a failed commit = %d, want 0: the state and the generation commit together", table, got)
		}
	}
}

func readStateProofGenerationRetentionRemovesReadStateWithItsGeneration(t *testing.T) {
	database, ctx := readStateIsolatedLiveDB(t)
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	for _, id := range []string{"gen-1", "gen-2", "gen-3"} {
		mutation := readStateClaimFor(t, ctx, database, id)
		state := readStateUnreadState(3,
			securityalerts.UnreadRepository{Repository: "acme/a", Reason: "org_repository_read_failed"},
			securityalerts.UnreadRepository{Repository: "acme/b", Reason: "org_repository_read_failed"},
		)
		if err := readStateCommitGeneration(ctx, store, mutation, id, "hint-"+id, state); err != nil {
			t.Fatalf("commit %s: %v", id, err)
		}
	}
	// gen-3 is the active generation; gen-1 and gen-2 are old superseded ones.
	if _, err := database.ExecContext(ctx, `
UPDATE scope_generations SET status = 'superseded', superseded_at = now() - interval '2 days',
       ingested_at = now() - interval '3 days'
WHERE generation_id IN ('gen-1', 'gen-2')`); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE scope_generations SET status = 'active' WHERE generation_id = 'gen-3'`); err != nil {
		t.Fatalf("activate: %v", err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE ingestion_scopes SET status = 'active', active_generation_id = 'gen-3' WHERE scope_id = $1`, readStateLiveScopeID); err != nil {
		t.Fatalf("point scope at active generation: %v", err)
	}

	result, err := postgres.NewGenerationRetentionStore(postgres.SQLDB{DB: database}).PruneSupersededGenerations(
		ctx, postgres.GenerationRetentionPolicy{
			MinSupersededGenerations: 0,
			MaxSupersededAge:         time.Hour,
			BatchGenerationLimit:     10,
			BatchRowLimit:            1_000_000,
			PolicyScope:              "global",
			PolicyRevision:           "8943-read-state",
		})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 2 {
		t.Fatalf("GenerationsPruned = %d, want 2", result.GenerationsPruned)
	}
	if got := result.RowsPruned["security_alert_target_read_states"]; got != 2 {
		t.Errorf("RowsPruned[security_alert_target_read_states] = %d, want 2", got)
	}
	if got := result.RowsPruned["security_alert_unread_repositories"]; got != 4 {
		t.Errorf("RowsPruned[security_alert_unread_repositories] = %d, want 4", got)
	}
	if got := readStateCount(t, ctx, database, `SELECT count(*) FROM security_alert_target_read_states WHERE generation_id IN ('gen-1','gen-2')`); got != 0 {
		t.Errorf("pruned generations keep %d state rows, want 0", got)
	}
	if got := readStateCount(t, ctx, database, `SELECT count(*) FROM security_alert_unread_repositories WHERE generation_id IN ('gen-1','gen-2')`); got != 0 {
		t.Errorf("pruned generations keep %d unread rows, want 0", got)
	}
	if got := readStateCount(t, ctx, database, `SELECT count(*) FROM security_alert_unread_repositories WHERE generation_id = 'gen-3'`); got != 2 {
		t.Errorf("active generation unread rows = %d, want 2 untouched", got)
	}
}

// TestSecurityAlertReadStateLiveCommit runs the commit-time proofs of the
// security-alert org read state as subtests, so the readiness shards price them
// as one test: each subtest still creates and drops its own database.
func TestSecurityAlertReadStateLiveCommit(t *testing.T) {
	t.Run("CommitPersistsReadStateWithGeneration", readStateProofCommitPersistsReadStateWithGeneration)
	t.Run("ReplayOfPendingGenerationReplacesUnreadRepositories", readStateProofReplayOfPendingGenerationReplacesUnreadRepositories)
	t.Run("FailedStateWriteRollsBackTheGeneration", readStateProofFailedStateWriteRollsBackTheGeneration)
	t.Run("GenerationRetentionRemovesReadStateWithItsGeneration", readStateProofGenerationRetentionRemovesReadStateWithItsGeneration)
}
