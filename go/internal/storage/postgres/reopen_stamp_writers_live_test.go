// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9320 PostgreSQL 18.6 full-bootstrap proof that every terminal-to-outstanding fact_work_items writer stamps reopened_at: the recovery replay, the bounded dead-letter drain, the poison sweep, the reconcile re-projection, the generation-liveness re-drive, the zombie heal, and the refinalize re-enqueue, each beside an expired claimed or running row that keeps its earlier stamp, plus a refinalize-reopened shared projection intent that ages from the reopen; seven subtests, about 1-2 s locally in an isolated schema
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/poison"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/rebuild/reset"
)

// assertReopenBacklogAge requires backlog's OldestAge to equal want exactly:
// exact-equality semantics are kept (no approximation), and both sides are
// logged as %v plus nanos alongside the full backlog struct (%+v), the
// migration receipt, the subtest name, and the involved row timelines.
func assertReopenBacklogAge(t *testing.T, f reopenStampFixture, backlog statuspkg.DomainBacklog, all []statuspkg.DomainBacklog, rowIDs []string, want time.Duration, why string) {
	t.Helper()
	if backlog.OldestAge == want {
		t.Logf("reopen stamp backlog: domain=%q oldest=%v (%d nanos) == want %v (%d nanos) %s | %s",
			backlog.Domain, backlog.OldestAge, backlog.OldestAge.Nanoseconds(),
			want, want.Nanoseconds(), why, f.testContext(t))
		return
	}
	rows := make([]string, 0, len(rowIDs))
	for _, id := range rowIDs {
		rows = append(rows, f.rowTimestamps(id))
	}
	t.Fatalf("reopen stamp backlog: domain=%q oldest=%v (%d nanos), want %v (%d nanos) %s\n%s\nbacklog=%+v allBacklogs=%+v\n%s",
		backlog.Domain, backlog.OldestAge, backlog.OldestAge.Nanoseconds(),
		want, want.Nanoseconds(), why, f.testContext(t), backlog, all, strings.Join(rows, "\n"))
}

// assertReopenBacklogUnder requires backlog's OldestAge to stay at or under
// bound (kept as a bound, not equality), with the same diagnostic bundle.
func assertReopenBacklogUnder(t *testing.T, f reopenStampFixture, backlog statuspkg.DomainBacklog, all []statuspkg.DomainBacklog, rowIDs []string, bound time.Duration, why string) {
	t.Helper()
	if backlog.OldestAge <= bound {
		t.Logf("reopen stamp backlog: domain=%q oldest=%v (%d nanos) <= bound %v (%d nanos) %s | %s",
			backlog.Domain, backlog.OldestAge, backlog.OldestAge.Nanoseconds(),
			bound, bound.Nanoseconds(), why, f.testContext(t))
		return
	}
	rows := make([]string, 0, len(rowIDs))
	for _, id := range rowIDs {
		rows = append(rows, f.rowTimestamps(id))
	}
	t.Fatalf("reopen stamp backlog: domain=%q oldest=%v (%d nanos) exceeds bound %v (%d nanos) %s\n%s\nbacklog=%+v allBacklogs=%+v\n%s",
		backlog.Domain, backlog.OldestAge, backlog.OldestAge.Nanoseconds(),
		bound, bound.Nanoseconds(), why, f.testContext(t), backlog, all, strings.Join(rows, "\n"))
}

// assertReopenUntouched requires the over-bound row to keep its terminal
// status with no stamp, logging the full state on mismatch.
func assertReopenUntouched(t *testing.T, f reopenStampFixture, id string, got reopenStampState, wantStatus string) {
	t.Helper()
	if got.status == wantStatus && !got.reopenedAt.Valid {
		t.Logf("reopen stamp untouched: %s still %q with no stamp | %s", id, got.status, f.testContext(t))
		return
	}
	t.Fatalf("reopen stamp untouched: %s status=%q reopened_at=%s, want %q with no stamp beyond the drain bound\n%s state=%+v",
		id, got.status, reopenStampNullTime(got.reopenedAt), wantStatus, f.failureContext(t, id), got)
}

// assertReopenMatched requires the re-projection to match and reopen,
// logging both flags with the row timeline on mismatch.
func assertReopenMatched(t *testing.T, f reopenStampFixture, scopeID, rowID string, matched, reopened bool) {
	t.Helper()
	if matched && reopened {
		t.Logf("reopen stamp reproject: %s matched and reopened | %s", scopeID, f.testContext(t))
		return
	}
	t.Fatalf("reopen stamp reproject: %s matched=%v reopened=%v, want both true\n%s",
		scopeID, matched, reopened, f.failureContext(t, rowID))
}

// assertReopenRecoveredAtLeast requires at least want recoveries, logging the
// count against the row timeline on mismatch.
func assertReopenRecoveredAtLeast(t *testing.T, f reopenStampFixture, rowID string, got, want int, what string) {
	t.Helper()
	if got >= want {
		t.Logf("reopen stamp recovered: %s %d >= %d %s | %s", what, got, want, rowID, f.testContext(t))
		return
	}
	t.Fatalf("reopen stamp recovered: %s recovered %d rows, want at least %d (%s)\n%s",
		what, got, want, rowID, f.failureContext(t, rowID))
}

// assertReopenHealOutcome requires the zombie-heal outcome to equal want
// exactly, logging both sides with the row timeline on mismatch.
func assertReopenHealOutcome(t *testing.T, f reopenStampFixture, scopeID, rowID, got, want string) {
	t.Helper()
	if got == want {
		t.Logf("reopen stamp heal: %s outcome=%q | %s", scopeID, got, f.testContext(t))
		return
	}
	t.Fatalf("reopen stamp heal: %s outcome = %q, want %q\n%s",
		scopeID, got, want, f.failureContext(t, rowID))
}

// assertReopenSharedIntents requires exactly want shared intents reopened,
// logging the count with the row timeline on mismatch.
func assertReopenSharedIntents(t *testing.T, f reopenStampFixture, rowID string, got, want int) {
	t.Helper()
	if got == want {
		t.Logf("reopen stamp shared intents: reopened=%d | %s", got, f.testContext(t))
		return
	}
	t.Fatalf("reopen stamp shared intents: SharedIntentsReopened = %d, want %d\n%s",
		got, want, f.failureContext(t, rowID))
}

// TestTerminalReopenWritersStampReopenedAtLive is the #9320 per-writer proof:
// every writer that moves a finished (succeeded, failed, dead_letter) fact
// work item back to pending stamps reopened_at, so the #9292 backlog age
// (COALESCE(reopened_at, created_at)) counts from the reopen, not from the
// row's first creation. The upsert writers also re-drive claimed or running
// rows whose lease expired; those rows were already outstanding and keep the
// reopen evidence they had.
//
// Before #9320 every subtest failed on the stamp: the row went back to
// pending with reopened_at untouched and reported 100 hours of age.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestTerminalReopenWritersStampReopenedAtLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "reopen_stamp", "set ESHU_POSTGRES_DSN to run the #9320 reopen-stamp writer proof")
	now := time.Now().UTC().Truncate(time.Microsecond)
	f := newReopenStampFixture(ctx, database, now)
	expired := now.Add(-time.Minute)
	earlier := now.Add(-3 * time.Hour)

	t.Run("dead-letter and failed replay", func(t *testing.T) {
		f.scope(t, "s-replay", "g-replay", f.old)
		f.row(t, reopenStampRow{id: "r-dead", scopeID: "s-replay", generationID: "g-replay", stage: "reducer", domain: "d-replay", status: "dead_letter"})
		f.row(t, reopenStampRow{id: "r-failed", scopeID: "s-replay", generationID: "g-replay", stage: "reducer", domain: "d-replay", status: "failed"})
		f.row(t, reopenStampRow{id: "projector_s-replay_g-replay", scopeID: "s-replay", generationID: "g-replay", stage: "projector", domain: "source_local", status: "dead_letter"})
		store := NewRecoveryStore(SQLDB{DB: database})
		for _, stage := range []recovery.Stage{recovery.StageReducer, recovery.StageProjector} {
			if _, err := store.ReplayFailedWorkItems(ctx, recovery.ReplayFilter{Stage: stage, ScopeIDs: []string{"s-replay"}}, now); err != nil {
				t.Fatalf("ReplayFailedWorkItems(%s) error = %v\n%s", stage, err, f.testContext(t))
			}
		}
		for _, id := range []string{"r-dead", "r-failed", "projector_s-replay_g-replay"} {
			f.assertReopenedAt(t, id, now)
		}
		// The status backlog reader reports the replayed domain's age since
		// the replay, not 100 hours.
		backlogs, err := listDomainBacklogs(ctx, SQLDB{DB: database}, now.Add(time.Minute))
		if err != nil {
			t.Fatalf("listDomainBacklogs() error = %v\n%s", err, f.testContext(t))
		}
		for _, backlog := range backlogs {
			if backlog.Domain == "d-replay" {
				assertReopenBacklogAge(t, f, backlog, backlogs,
					[]string{"r-dead", "r-failed", "projector_s-replay_g-replay"},
					time.Minute, "since the replay")
			}
		}
	})

	t.Run("bounded dead-letter drain", func(t *testing.T) {
		f.scope(t, "s-drain", "g-drain", f.old)
		f.row(t, reopenStampRow{id: "r-drain-a", scopeID: "s-drain", generationID: "g-drain", stage: "reducer", domain: "d-drain", status: "dead_letter"})
		f.row(t, reopenStampRow{id: "r-drain-b", scopeID: "s-drain", generationID: "g-drain", stage: "reducer", domain: "d-drain", status: "dead_letter"})
		store := NewRecoveryStore(SQLDB{DB: database})
		if _, err := store.ReplayFailedWorkItems(ctx, recovery.ReplayFilter{Stage: recovery.StageReducer, ScopeIDs: []string{"s-drain"}, Limit: 1}, now); err != nil {
			t.Fatalf("bounded ReplayFailedWorkItems() error = %v\n%s", err, f.testContext(t))
		}
		f.assertReopenedAt(t, "r-drain-a", now)
		assertReopenUntouched(t, f, "r-drain-b", f.state(t, "r-drain-b", now), "dead_letter")
	})

	t.Run("poison sweep", func(t *testing.T) {
		f.scope(t, "s-poison", "g-poison", f.old)
		f.row(t, reopenStampRow{id: "r-poison", scopeID: "s-poison", generationID: "g-poison", stage: "reducer", domain: "d-poison", status: "dead_letter"})
		result, err := poison.NewStore(SQLDB{DB: database}).RecoverDeadLetters(ctx, poison.Policy{MaxRecoverAttempts: 3, BatchLimit: 100}, now)
		if err != nil {
			t.Fatalf("RecoverDeadLetters() error = %v\n%s", err, f.testContext(t))
		}
		assertReopenRecoveredAtLeast(t, f, "r-poison", result.Recovered, 1, "RecoverDeadLetters() for the seeded poison row")
		f.assertReopenedAt(t, "r-poison", now)
	})

	t.Run("reconcile re-projection", func(t *testing.T) {
		for _, c := range []struct {
			scopeID, status string
			claimUntil      *time.Time
		}{
			{"s-reproject-done", "succeeded", nil},
			{"s-reproject-dead", "dead_letter", nil},
			{"s-reproject-expired", "claimed", &expired},
		} {
			generationID := "g-" + c.scopeID
			f.scope(t, c.scopeID, generationID, f.old)
			id := "projector_" + c.scopeID + "_" + generationID
			f.row(t, reopenStampRow{
				id: id, scopeID: c.scopeID, generationID: generationID, stage: "projector",
				domain: "source_local", status: c.status, claimUntil: c.claimUntil, reopenedAt: &earlier,
			})
			var matched, reopened bool
			if err := database.QueryRowContext(ctx, reprojectActiveGenerationQuery,
				now, c.scopeID, generationID, "hint", "commit", "test", "").Scan(&matched, &reopened); err != nil {
				t.Fatalf("reprojectActiveGenerationQuery(%s) error = %v\n%s", c.scopeID, err, f.testContext(t))
			}
			assertReopenMatched(t, f, c.scopeID, id, matched, reopened)
			if c.claimUntil != nil {
				f.assertReopenKept(t, id, &earlier)
				continue
			}
			f.assertReopenedAt(t, id, now)
		}
	})

	t.Run("generation liveness re-drive", func(t *testing.T) {
		for _, c := range []struct {
			scopeID, status string
			claimUntil      *time.Time
		}{
			{"s-wedged-done", "succeeded", nil},
			{"s-wedged-expired", "running", &expired},
		} {
			generationID := "g-" + c.scopeID
			f.scope(t, c.scopeID, generationID, now.Add(-2*time.Hour))
			f.row(t, reopenStampRow{
				id: "projector_" + c.scopeID + "_" + generationID, scopeID: c.scopeID,
				generationID: generationID, stage: "projector", domain: "source_local", status: c.status, claimUntil: c.claimUntil,
			})
			f.exec(t, `INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key, scope_id,
  acceptance_unit_id, repository_id, source_run_id, generation_id, payload, created_at)
VALUES ($1, 'wedged_domain', 'p', $2, $2, $2, 'run', $3, '{}'::jsonb, $4)`, "i-"+c.scopeID, c.scopeID, generationID, f.old)
		}
		result, err := NewGenerationLivenessStore(SQLDB{DB: database}).RecoverWedgedGenerations(ctx, GenerationLivenessPolicy{
			ActivationDeadline: time.Hour, MaxRecoverAttempts: 3, BatchLimit: 10, ProgressWindow: 10 * time.Minute,
		}, now)
		if err != nil {
			t.Fatalf("RecoverWedgedGenerations() error = %v\n%s", err, f.testContext(t))
		}
		assertReopenRecoveredAtLeast(t, f, "projector_s-wedged-done_g-s-wedged-done", result.Recovered, 2, "RecoverWedgedGenerations() for both wedged generations")
		f.assertReopenedAt(t, "projector_s-wedged-done_g-s-wedged-done", now)
		f.assertReopenKept(t, "projector_s-wedged-expired_g-s-wedged-expired", nil)
	})

	t.Run("zombie heal", func(t *testing.T) {
		for _, c := range []struct {
			scopeID, status string
			claimUntil      *time.Time
		}{
			{"s-zombie-dead", "dead_letter", nil},
			{"s-zombie-expired", "claimed", &expired},
		} {
			active, refused := "g-active-"+c.scopeID, "g-refused-"+c.scopeID
			f.scope(t, c.scopeID, active, f.old)
			f.exec(t, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, projection_write_started_at, payload)
VALUES ($1, $2, 'snapshot', $3, $3, 'superseded', $3, '{}'::jsonb)`, refused, c.scopeID, f.old)
			id := "projector_" + c.scopeID + "_" + active
			f.row(t, reopenStampRow{
				id: id, scopeID: c.scopeID, generationID: active, stage: "projector",
				domain: "source_local", status: c.status, claimUntil: c.claimUntil, reopenedAt: &earlier,
			})
			got, err := ProjectorQueue{database: SQLDB{DB: database}}.healZombieActiveGeneration(ctx, c.scopeID, refused, now)
			if err != nil {
				t.Fatalf("healZombieActiveGeneration(%s) error = %v\n%s", c.scopeID, err, f.testContext(t))
			}
			assertReopenHealOutcome(t, f, c.scopeID, id, got.outcome, zombieHealOutcomeHealed)
			if c.claimUntil != nil {
				f.assertReopenKept(t, id, &earlier)
				continue
			}
			f.assertReopenedAt(t, id, now)
		}
	})

	t.Run("refinalize re-enqueue and shared intent reopen", func(t *testing.T) {
		f.scope(t, "s-refinalize", "g-refinalize", f.old)
		f.row(t, reopenStampRow{
			id: "refinalize_s-refinalize_g-refinalize", scopeID: "s-refinalize",
			generationID: "g-refinalize", stage: "projector", domain: "source_local", status: "succeeded",
		})
		f.scope(t, "s-refinalize-live", "g-refinalize-live", f.old)
		live := now.Add(time.Hour)
		f.row(t, reopenStampRow{
			id: "refinalize_s-refinalize-live_g-refinalize-live", scopeID: "s-refinalize-live",
			generationID: "g-refinalize-live", stage: "projector", domain: "source_local", status: "claimed",
			claimUntil: &live, reopenedAt: &earlier,
		})
		f.exec(t, `INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key, scope_id,
  acceptance_unit_id, repository_id, source_run_id, generation_id, payload, created_at, completed_at)
VALUES ('i-refinalize', 'refinalize_domain', 'p', 's-refinalize', 's-refinalize', 's-refinalize', 'run',
  'g-refinalize', '{}'::jsonb, $1, $1)`, f.old)

		tx, err := SQLDB{DB: database}.Begin(ctx)
		if err != nil {
			t.Fatalf("begin refinalize: %v\n%s", err, f.testContext(t))
		}
		var generations reset.Generations
		generations.Append("s-refinalize", "g-refinalize")
		generations.Append("s-refinalize-live", "g-refinalize-live")
		if _, err := reset.EnqueueProjectorWork(ctx, resetQueryer{Transaction: tx}, generations, now); err != nil {
			_ = tx.Rollback()
			t.Fatalf("EnqueueProjectorWork() error = %v\n%s", err, f.testContext(t))
		}
		counts, err := reset.ApplyPreRetirement(ctx, tx, generations)
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("ApplyPreRetirement() error = %v\n%s", err, f.testContext(t))
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit refinalize: %v\n%s", err, f.testContext(t))
		}
		assertReopenSharedIntents(t, f, "refinalize_s-refinalize_g-refinalize", counts.SharedIntentsReopened, 1)
		f.assertReopenedAt(t, "refinalize_s-refinalize_g-refinalize", now)
		f.assertReopenKept(t, "refinalize_s-refinalize-live_g-refinalize-live", &earlier)

		// The shared-projection backlog reports the reopened intent's age
		// from the reopen. The reset stamps the database clock, so read the
		// age against it.
		var dbNow time.Time
		if err := database.QueryRowContext(ctx, `SELECT now()`).Scan(&dbNow); err != nil {
			t.Fatalf("read database clock: %v\n%s", err, f.testContext(t))
		}
		backlogs, err := listDomainBacklogs(ctx, SQLDB{DB: database}, dbNow)
		if err != nil {
			t.Fatalf("listDomainBacklogs() error = %v\n%s", err, f.testContext(t))
		}
		found := false
		for _, backlog := range backlogs {
			if backlog.Domain != "refinalize_domain" {
				continue
			}
			found = true
			assertReopenBacklogUnder(t, f, backlog, backlogs,
				[]string{"refinalize_s-refinalize_g-refinalize"},
				time.Minute, "since the reopen (created 100 hours ago)")
		}
		if !found {
			t.Fatalf("refinalize_domain missing from the shared-projection backlog after the reopen\n%s\nbacklogs=%+v",
				f.testContext(t), backlogs)
		}
	})
}
