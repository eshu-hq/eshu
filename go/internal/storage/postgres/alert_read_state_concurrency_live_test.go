// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8943 disposable PostgreSQL 18 concurrency proofs under the race detector that generation retention pruning old security-alert generations never deadlocks with, blocks, or damages concurrent claim-fenced commits of the same target, that a second claim of the same target never mixes its read state with another claim's, and that two claims racing on one generation id leave one consistent state; each proof creates and drops its own database from the bootstrap schema; about 2 to 6 s per proof on PostgreSQL 18.6 locally; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	// readStateOldGenerations is the superseded history retention prunes while writers
	// commit. Each carries unreadPerGeneration unread rows, so the cascade
	// has real work to do.
	readStateOldGenerations     = 12
	readStateUnreadPerOld       = 40
	readStateConcurrentWriters  = 8
	readStateUnreadPerNew       = 25
	readStateRacingSameIDRounds = 6
)

func readStateStateWithUnread(n int, reason string) securityalerts.TargetReadState {
	unread := make([]securityalerts.UnreadRepository, n)
	for i := range unread {
		unread[i] = securityalerts.UnreadRepository{Repository: fmt.Sprintf("acme/repo-%03d", i), Reason: reason}
	}
	return readStateUnreadState(n+10, unread...)
}

func readStateRetentionPolicy() postgres.GenerationRetentionPolicy {
	return postgres.GenerationRetentionPolicy{
		MinSupersededGenerations: 0,
		MaxSupersededAge:         time.Hour,
		BatchGenerationLimit:     3,
		BatchRowLimit:            1_000_000,
		PolicyScope:              "global",
		PolicyRevision:           "8943-concurrency",
	}
}

// readStateSeedOldGenerations commits n generations through the production path and
// marks them as old superseded history.
func readStateSeedOldGenerations(t *testing.T, ctx context.Context, database *sql.DB, store postgres.IngestionStore) []string {
	t.Helper()
	ids := make([]string, readStateOldGenerations)
	for i := range ids {
		ids[i] = fmt.Sprintf("old-%02d", i)
		mutation := readStateClaimFor(t, ctx, database, ids[i])
		if err := readStateCommitGeneration(ctx, store, mutation, ids[i], "hint-"+ids[i],
			readStateStateWithUnread(readStateUnreadPerOld, "org_repository_fanout_cap_reached")); err != nil {
			t.Fatalf("seed %s: %v", ids[i], err)
		}
	}
	if _, err := database.ExecContext(ctx, `
UPDATE scope_generations SET status = 'superseded', superseded_at = now() - interval '2 days',
       ingested_at = now() - interval '3 days'
WHERE generation_id LIKE 'old-%'`); err != nil {
		t.Fatalf("supersede seeded history: %v", err)
	}
	return ids
}

func readStateIsDeadlock(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "40P01") || strings.Contains(err.Error(), "deadlock detected"))
}

// TestLiveRetentionPruneRunsAlongsideClaimFencedCommitsOfTheSameTarget proves
// the writer against retention: writers commit new generations (with state) of
// the scope retention is pruning. No commit or pass may fail or deadlock, every
// new generation keeps exactly its own state, and the old history leaves with
// its state.
func readStateProofRetentionPruneRunsAlongsideClaimFencedCommitsOfTheSameTarget(t *testing.T) {
	database, ctx := readStateIsolatedLiveDB(t)
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	readStateSeedOldGenerations(t, ctx, database, store)

	mutations := make([]string, readStateConcurrentWriters)
	for i := range mutations {
		mutations[i] = fmt.Sprintf("new-%02d", i)
	}
	prepared := make(map[string]readStateClaimMutation, readStateConcurrentWriters)
	for _, id := range mutations {
		prepared[id] = readStateClaimFor(t, ctx, database, id)
	}

	var wg sync.WaitGroup
	var commitFailures, passFailures, deadlocks, passes, pruned atomic.Int64
	writersDone := make(chan struct{})
	start := make(chan struct{})
	for _, id := range mutations {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			<-start
			err := readStateCommitGeneration(ctx, store, prepared[id], id, "hint-"+id,
				readStateStateWithUnread(readStateUnreadPerNew, "org_repository_read_failed"))
			if err != nil {
				commitFailures.Add(1)
				if readStateIsDeadlock(err) {
					deadlocks.Add(1)
				}
				t.Errorf("commit %s: %v", id, err)
			}
		}(id)
	}
	retentionDone := make(chan struct{})
	go func() {
		defer close(retentionDone)
		<-start
		retention := postgres.NewGenerationRetentionStore(postgres.SQLDB{DB: database})
		for {
			result, err := retention.PruneSupersededGenerations(ctx, readStateRetentionPolicy())
			passes.Add(1)
			if err != nil {
				passFailures.Add(1)
				if readStateIsDeadlock(err) {
					deadlocks.Add(1)
				}
				t.Errorf("retention pass: %v", err)
			}
			pruned.Add(int64(result.GenerationsPruned))
			select {
			case <-writersDone:
				if pruned.Load() >= readStateOldGenerations {
					return
				}
			default:
			}
			if ctx.Err() != nil {
				return
			}
		}
	}()
	began := time.Now()
	close(start)
	wg.Wait()
	close(writersDone)
	<-retentionDone
	t.Logf("%d writers and %d retention passes ran together in %s; pruned %d generations", readStateConcurrentWriters, passes.Load(), time.Since(began), pruned.Load())

	if commitFailures.Load()+passFailures.Load() != 0 || deadlocks.Load() != 0 {
		t.Fatalf("commit failures %d, pass failures %d, deadlocks %d, want all 0", commitFailures.Load(), passFailures.Load(), deadlocks.Load())
	}
	if got := readStateCount(t, ctx, database, `SELECT count(*) FROM security_alert_target_read_states WHERE generation_id LIKE 'old-%'`); got != 0 {
		t.Errorf("old state rows left after retention = %d, want 0", got)
	}
	if got := readStateCount(t, ctx, database, `SELECT count(*) FROM security_alert_unread_repositories WHERE generation_id LIKE 'old-%'`); got != 0 {
		t.Errorf("old unread rows left after retention = %d, want 0", got)
	}
	readStateAssertEveryNewGenerationHasItsOwnState(t, ctx, database, readStateConcurrentWriters, readStateUnreadPerNew)
}

// readStateAssertEveryNewGenerationHasItsOwnState checks the per-generation invariant:
// one state row each, and its unread_total equals its own child rows.
func readStateAssertEveryNewGenerationHasItsOwnState(t *testing.T, ctx context.Context, database *sql.DB, generations, unread int) {
	t.Helper()
	if got := readStateCount(t, ctx, database, `SELECT count(*) FROM security_alert_target_read_states WHERE generation_id LIKE 'new-%'`); got != generations {
		t.Errorf("new state rows = %d, want %d", got, generations)
	}
	if got := readStateCount(t, ctx, database, `
SELECT count(*) FROM security_alert_target_read_states AS state
WHERE state.generation_id LIKE 'new-%'
  AND state.unread_total = $1
  AND (SELECT count(*) FROM security_alert_unread_repositories AS child WHERE child.generation_id = state.generation_id) = state.unread_total`, unread); got != generations {
		t.Errorf("generations whose unread_total matches their own child rows = %d, want %d", got, generations)
	}
	if got := readStateCount(t, ctx, database, `SELECT count(*) FROM security_alert_unread_repositories WHERE generation_id LIKE 'new-%'`); got != generations*unread {
		t.Errorf("new unread rows = %d, want %d", got, generations*unread)
	}
	if got := readStateCount(t, ctx, database, `SELECT count(*) FROM security_alert_unread_repositories WHERE generation_id LIKE 'new-%' AND reason <> 'org_repository_read_failed'`); got != 0 {
		t.Errorf("new generations hold %d rows with another claim's reason, want 0", got)
	}
}

// TestLiveSecondClaimOfTheSameTargetKeepsItsOwnState proves two claims of the
// same target that commit different generations at once never mix state.
func readStateProofSecondClaimOfTheSameTargetKeepsItsOwnState(t *testing.T) {
	database, ctx := readStateIsolatedLiveDB(t)
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	ids := []string{"new-00", "new-01", "new-02", "new-03"}
	prepared := make(map[string]readStateClaimMutation, len(ids))
	for _, id := range ids {
		prepared[id] = readStateClaimFor(t, ctx, database, id)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, len(ids))
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			<-start
			errs <- readStateCommitGeneration(ctx, store, prepared[id], id, "hint-"+id, readStateStateWithUnread(readStateUnreadPerNew, "org_repository_read_failed"))
		}(id)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent commit of a second claim: %v", err)
		}
	}
	readStateAssertEveryNewGenerationHasItsOwnState(t, ctx, database, len(ids), readStateUnreadPerNew)
}

// TestLiveClaimsRacingOnOneGenerationIDLeaveOneConsistentState proves the
// replace semantics under a race: two claims commit the same generation id
// with different states. Whichever commits last wins whole; the parent total
// always equals its child rows, so a reader never sees a mixed set.
func readStateProofClaimsRacingOnOneGenerationIDLeaveOneConsistentState(t *testing.T) {
	database, ctx := readStateIsolatedLiveDB(t)
	store := postgres.NewIngestionStore(postgres.SQLDB{DB: database})
	for round := 0; round < readStateRacingSameIDRounds; round++ {
		id := fmt.Sprintf("same-%d", round)
		mutation := readStateClaimFor(t, ctx, database, id)
		sizes := [2]int{7, 31}
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range sizes {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				reason := fmt.Sprintf("org_repository_read_failed_%d", i)
				err := readStateCommitGeneration(ctx, store, mutation, id, fmt.Sprintf("hint-%d-%d", round, i), readStateStateWithUnread(sizes[i], reason))
				if err != nil && !errors.Is(err, context.Canceled) {
					t.Errorf("round %d writer %d: %v", round, i, err)
				}
			}(i)
		}
		close(start)
		wg.Wait()

		var total int
		if err := database.QueryRowContext(ctx, `SELECT unread_total FROM security_alert_target_read_states WHERE generation_id = $1`, id).Scan(&total); err != nil {
			t.Fatalf("round %d: read state: %v", round, err)
		}
		children := readStateCount(t, ctx, database, `SELECT count(*) FROM security_alert_unread_repositories WHERE generation_id = $1`, id)
		distinctReasons := readStateCount(t, ctx, database, `SELECT count(DISTINCT reason) FROM security_alert_unread_repositories WHERE generation_id = $1`, id)
		if children != total || distinctReasons != 1 || (total != sizes[0] && total != sizes[1]) {
			t.Fatalf("round %d: unread_total %d, child rows %d, distinct reasons %d: want one writer's whole state", round, total, children, distinctReasons)
		}
	}
}

// TestSecurityAlertReadStateLiveConcurrency runs the concurrency proofs of the
// security-alert org read state as subtests, so the readiness shards price them
// as one test: each subtest still creates and drops its own database.
func TestSecurityAlertReadStateLiveConcurrency(t *testing.T) {
	t.Run("RetentionPruneRunsAlongsideClaimFencedCommitsOfTheSameTarget", readStateProofRetentionPruneRunsAlongsideClaimFencedCommitsOfTheSameTarget)
	t.Run("SecondClaimOfTheSameTargetKeepsItsOwnState", readStateProofSecondClaimOfTheSameTargetKeepsItsOwnState)
	t.Run("ClaimsRacingOnOneGenerationIDLeaveOneConsistentState", readStateProofClaimsRacingOnOneGenerationIDLeaveOneConsistentState)
}
