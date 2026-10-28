// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9320 PostgreSQL 18.6 full-bootstrap proof that shared projection intent age is reopen-aware through the status snapshot: a five-day-old completed intent reopened by the admin repo_dependency reopen and one reopened by the rebuild reset each report an oldest age from the reopen, while a fresh five-day-old pending intent keeps its creation age; plus the reopen races: two reopeners on one intent reopen it once and keep the winner's stamp, and a drain racing a reopen leaves the intent completed with no pending age; about 2-4 s locally in isolated schemas
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/rebuild/reset"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// intentAgeFixture seeds scopes and intents for the shared-intent age proofs.
type intentAgeFixture struct {
	ctx      context.Context
	database *sql.DB
	old      time.Time
}

func openIntentAgeFixture(t *testing.T, prefix string) intentAgeFixture {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #9320 shared-intent age proof")
	}
	database := postgresproof.OpenIsolatedSchema(t, dsn, prefix, func(ctx context.Context, database *sql.DB) error {
		return pgstatus.ApplyBootstrap(ctx, pgstatus.SQLDB{DB: database})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return intentAgeFixture{ctx: ctx, database: database, old: time.Now().UTC().Add(-5 * 24 * time.Hour).Truncate(time.Microsecond)}
}

func (f intentAgeFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.database.ExecContext(f.ctx, query, args...); err != nil {
		t.Fatalf("seed: %v\n%s", err, query)
	}
}

// scope seeds an active scope and generation.
func (f intentAgeFixture) scope(t *testing.T, scopeID, generationID string) {
	t.Helper()
	f.exec(t, `INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'repository', 'git', $1, 'git', $1, $3, $3, 'active', $2, '{}'::jsonb)`, scopeID, generationID, f.old)
	f.exec(t, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'snapshot', $3, $3, 'active', $3, '{}'::jsonb)`, generationID, scopeID, f.old)
}

// intent seeds one five-day-old intent and its acceptance row; completed
// marks it finished at the same instant.
func (f intentAgeFixture) intent(t *testing.T, intentID, domain, scopeID, generationID string, completed bool) {
	t.Helper()
	var completedAt any
	if completed {
		completedAt = f.old
	}
	unit := "unit-" + intentID
	f.exec(t, `INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key, scope_id,
  acceptance_unit_id, repository_id, source_run_id, generation_id, payload, created_at, completed_at)
VALUES ($1, $2, 'p', $3, $4, 'repo', 'run', $5, '{}'::jsonb, $6, $7)`,
		intentID, domain, scopeID, unit, generationID, f.old, completedAt)
	f.exec(t, `INSERT INTO shared_projection_acceptance (scope_id, acceptance_unit_id, source_run_id, generation_id, accepted_at, updated_at)
VALUES ($1, $2, 'run', $3, $4, $4)`, scopeID, unit, generationID, f.old)
}

// state reads an intent's completion and reopen stamp.
func (f intentAgeFixture) state(t *testing.T, intentID string) (completed bool, reopenedAt sql.NullTime) {
	t.Helper()
	if err := f.database.QueryRowContext(f.ctx, `SELECT completed_at IS NOT NULL, reopened_at
FROM shared_projection_intents WHERE intent_id = $1`, intentID).Scan(&completed, &reopenedAt); err != nil {
		t.Fatalf("read intent %s: %v", intentID, err)
	}
	return completed, reopenedAt
}

// ages reads the status snapshot's shared-projection oldest age by domain.
func (f intentAgeFixture) ages(t *testing.T) map[string]time.Duration {
	t.Helper()
	snapshot, err := pgstatus.NewStatusStore(pgstatus.SQLDB{DB: f.database}).ReadStatusSnapshot(f.ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ReadStatusSnapshot() error = %v", err)
	}
	ages := map[string]time.Duration{}
	for _, backlog := range snapshot.DomainBacklogs {
		ages[backlog.Domain] = backlog.OldestAge
	}
	return ages
}

// adminReopen runs the admin repo_dependency reopen with a fixed clock.
func (f intentAgeFixture) adminReopen(t *testing.T, scopeID string, now time.Time) []string {
	t.Helper()
	adminStore := NewStore(f.database)
	adminStore.(*postgresStore).now = func() time.Time { return now }
	result, err := adminStore.ReopenCompletedWork(f.ctx, admin.ReopenFilter{Domain: admin.ReopenDomainRepoDependency, ScopeID: scopeID, Limit: 10})
	if err != nil {
		t.Fatalf("admin ReopenCompletedWork(%s) error = %v", scopeID, err)
	}
	return result.IntentIDs
}

// resetReopen runs the rebuild reset's pre-retirement statements, which
// include reopenSharedIntentsTemplate, for one generation inside tx.
func resetReopen(ctx context.Context, t *testing.T, tx *sql.Tx, scopeID, generationID string) int {
	t.Helper()
	var generations reset.Generations
	generations.Append(scopeID, generationID)
	counts, err := reset.ApplyPreRetirement(ctx, tx, generations)
	if err != nil {
		t.Fatalf("ApplyPreRetirement() error = %v", err)
	}
	return counts.SharedIntentsReopened
}

// holder opens a transaction and reports its backend pid, so a concurrent
// writer can be shown to wait on its row locks.
func (f intentAgeFixture) holder(t *testing.T) (*sql.Tx, int) {
	t.Helper()
	tx, err := f.database.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	var pid int
	if err := tx.QueryRowContext(f.ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("holder pid: %v", err)
	}
	return tx, pid
}

// waitBlockedBy returns once some backend waits on a lock pid holds.
func (f intentAgeFixture) waitBlockedBy(t *testing.T, pid int, done <-chan error) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var blocked int
		if err := f.database.QueryRowContext(f.ctx, `SELECT count(*) FROM pg_stat_activity
WHERE $1 = ANY(pg_blocking_pids(pid))`, pid).Scan(&blocked); err != nil {
			t.Fatalf("poll blocked backends: %v", err)
		}
		if blocked > 0 {
			return
		}
		select {
		case err := <-done:
			t.Fatalf("writer finished without waiting on the holder's lock: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("writer never blocked on the holder's row lock")
}

// TestSharedIntentReopenAgeThroughStatusLive is the #9320 AC-4 proof: the
// status snapshot's shared-projection backlog ages a reopened intent from the
// reopen, through both reopen writers, and a fresh pending intent from its
// creation. A fresh insert leaves reopened_at NULL, and the upsert never
// clears completed_at, so COALESCE(reopened_at, created_at) is the reopen
// time for a reopened intent and the creation time for any other pending one.
func TestSharedIntentReopenAgeThroughStatusLive(t *testing.T) {
	f := openIntentAgeFixture(t, "intent_age_status")
	f.scope(t, "s-admin", "g-admin")
	f.intent(t, "i-admin", admin.ReopenDomainRepoDependency, "s-admin", "g-admin", true)
	f.scope(t, "s-reset", "g-reset")
	f.intent(t, "i-reset", "reset_domain", "s-reset", "g-reset", true)
	f.scope(t, "s-fresh", "g-fresh")
	f.intent(t, "i-fresh", "fresh_domain", "s-fresh", "g-fresh", false)

	if ids := f.adminReopen(t, "s-admin", time.Now().UTC()); len(ids) != 1 {
		t.Fatalf("admin reopen reopened %v, want one intent", ids)
	}
	tx, _ := f.holder(t)
	if n := resetReopen(f.ctx, t, tx, "s-reset", "g-reset"); n != 1 {
		t.Fatalf("reset reopened %d intents, want 1", n)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit reset: %v", err)
	}

	ages := f.ages(t)
	for _, domain := range []string{admin.ReopenDomainRepoDependency, "reset_domain"} {
		age, ok := ages[domain]
		if !ok {
			t.Fatalf("%s missing from the status backlog after the reopen: %v", domain, ages)
		}
		if age > time.Minute {
			t.Fatalf("%s oldest age = %v, want under a minute since the reopen, not five days since creation", domain, age)
		}
	}
	if age := ages["fresh_domain"]; age < 5*24*time.Hour-time.Minute {
		t.Fatalf("fresh_domain oldest age = %v, want its five-day creation age (a fresh insert has no reopen)", age)
	}
	if _, stamp := f.state(t, "i-fresh"); stamp.Valid {
		t.Fatalf("fresh pending intent reopened_at = %v, want NULL", stamp.Time)
	}
}

// TestSharedIntentReopenRacesLive is the #9320 concurrency proof for the
// shared-intent reopen stamp. The conflict domain is one intent row. Both
// reopen writers guard on completed_at IS NOT NULL, and the admin reopen
// takes its rows FOR UPDATE SKIP LOCKED, so two reopeners reopen an intent
// once and the winner's stamp stays; a drain completing the intent waits on
// a reopen's row lock and then completes it, leaving no pending age.
func TestSharedIntentReopenRacesLive(t *testing.T) {
	f := openIntentAgeFixture(t, "intent_age_race")
	domain := admin.ReopenDomainRepoDependency

	t.Run("reset holds the row, admin reopen skips it", func(t *testing.T) {
		f.scope(t, "s-two", "g-two")
		f.intent(t, "i-two", domain, "s-two", "g-two", true)
		tx, _ := f.holder(t)
		if n := resetReopen(f.ctx, t, tx, "s-two", "g-two"); n != 1 {
			t.Fatalf("reset reopened %d intents, want 1", n)
		}
		if ids := f.adminReopen(t, "s-two", time.Now().UTC()); len(ids) != 0 {
			t.Fatalf("admin reopen took %v while the reset held the row, want none", ids)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit reset: %v", err)
		}
		completed, stamp := f.state(t, "i-two")
		if completed || !stamp.Valid {
			t.Fatalf("i-two completed=%v reopened_at=%v, want pending with the reset's stamp", completed, stamp)
		}
		if ids := f.adminReopen(t, "s-two", time.Now().UTC()); len(ids) != 0 {
			t.Fatalf("admin reopen of a pending intent took %v, want none", ids)
		}
		if _, again := f.state(t, "i-two"); !again.Time.Equal(stamp.Time) {
			t.Fatalf("i-two reopened_at moved from %v to %v on a no-op reopen", stamp.Time, again.Time)
		}
	})

	t.Run("admin reopen first, reset finds nothing", func(t *testing.T) {
		f.scope(t, "s-first", "g-first")
		f.intent(t, "i-first", domain, "s-first", "g-first", true)
		adminAt := time.Now().UTC().Truncate(time.Microsecond)
		if ids := f.adminReopen(t, "s-first", adminAt); len(ids) != 1 {
			t.Fatalf("admin reopen reopened %v, want one intent", ids)
		}
		tx, _ := f.holder(t)
		if n := resetReopen(f.ctx, t, tx, "s-first", "g-first"); n != 0 {
			t.Fatalf("reset reopened %d intents after the admin reopen, want 0", n)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit reset: %v", err)
		}
		if _, stamp := f.state(t, "i-first"); !stamp.Valid || !stamp.Time.Equal(adminAt) {
			t.Fatalf("i-first reopened_at = %v, want the admin reopen's %v", stamp, adminAt)
		}
	})

	t.Run("drain waits on a reopen, then completes", func(t *testing.T) {
		f.scope(t, "s-drain", "g-drain")
		f.intent(t, "i-drain", "drain_domain", "s-drain", "g-drain", true)
		tx, pid := f.holder(t)
		if n := resetReopen(f.ctx, t, tx, "s-drain", "g-drain"); n != 1 {
			t.Fatalf("reset reopened %d intents, want 1", n)
		}
		done := make(chan error, 1)
		go func() {
			_, err := pgstatus.NewSharedIntentStore(pgstatus.SQLDB{DB: f.database}).
				MarkIntentsCompleted(f.ctx, []string{"i-drain"}, time.Now().UTC())
			done <- err
		}()
		f.waitBlockedBy(t, pid, done)
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit reset: %v", err)
		}
		if err := <-done; err != nil {
			t.Fatalf("MarkIntentsCompleted() error = %v", err)
		}
		if completed, _ := f.state(t, "i-drain"); !completed {
			t.Fatal("i-drain is pending after the drain completed it")
		}
		if age, ok := f.ages(t)["drain_domain"]; ok {
			t.Fatalf("drain_domain still reports a pending age %v after the drain", age)
		}
	})

	t.Run("drain holds the row, admin reopen skips it", func(t *testing.T) {
		f.scope(t, "s-held", "g-held")
		f.intent(t, "i-held", domain, "s-held", "g-held", true)
		tx, _ := f.holder(t)
		if _, err := tx.ExecContext(f.ctx, `UPDATE shared_projection_intents SET completed_at = $2
WHERE intent_id = ANY($1)`, []string{"i-held"}, time.Now().UTC()); err != nil {
			t.Fatalf("drain re-complete in holder: %v", err)
		}
		if ids := f.adminReopen(t, "s-held", time.Now().UTC()); len(ids) != 0 {
			t.Fatalf("admin reopen took %v while a drain held the row, want none", ids)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit drain: %v", err)
		}
		if completed, stamp := f.state(t, "i-held"); !completed || stamp.Valid {
			t.Fatalf("i-held completed=%v reopened_at=%v, want completed with no reopen stamp", completed, stamp)
		}
	})
}
