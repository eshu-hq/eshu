// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8828 disposable PostgreSQL 18 proof of the coordinator_snapshot summary model: a row written by the production writer pass equals the live coordinator read recomputed at the row's as_of in one REPEATABLE READ snapshot, tuple for tuple, at an empty database and with runs, work items in every status, claims, collector instances, completeness rows, dead letters and an unacknowledged and an acknowledged terminal failure; the production stored-summary reader serves it with the live counts; four concurrent writer replicas racing for the same ticks beside workflow-queue claim traffic never error, write a monotonic as_of, leave exactly one decodable row, and never block the claims
//eshu:readiness:env ESHU_STATUS_SUMMARY_PROOF_DISPOSABLE ESHU_STATUS_SUMMARY_PROOF_DSN

import (
	"context"
	"database/sql"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	statussummary "github.com/eshu-hq/eshu/go/internal/reducer/status/summary"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

// newLiveWriterWithCoordinator is newLiveWriter plus the coordinator_snapshot
// companion, wired like the reducer does (cmd/reducer statusSummaryWriterFor).
func newLiveWriterWithCoordinator(database *sql.DB) *statussummary.Runner {
	writer := newLiveWriter(database)
	writer.Companions = []statussummary.Statement{{
		ModelKey:     store.ModelCoordinatorSnapshot,
		SourceSHA256: postgres.CoordinatorSummarySourceSHA256(),
		Compute:      postgres.ReadCoordinatorSummaryEntries,
	}}
	return writer
}

// seedCoordinator replaces the workflow tables with a populated fixture: two
// enabled collector instances (one with a 15 minute scan interval), runs in two
// statuses, total work items spread over every status the coordinator reads
// (pending, claimed with an active claim, a retrying row, terminal failures,
// expired, completed), a blocked completeness row, a dead-lettered generation,
// and an operator acknowledgement covering one failed scope.
func seedCoordinator(ctx context.Context, t *testing.T, database *sql.DB, total int) {
	t.Helper()
	for _, table := range []string{
		"collector_terminal_failure_acknowledgements", "workflow_claims", "workflow_run_completeness",
		"workflow_work_items", "workflow_runs", "collector_generation_dead_letters", "collector_instances",
	} {
		mustExec(ctx, t, database, `DELETE FROM `+table)
	}
	if total == 0 {
		return
	}
	mustExec(ctx, t, database, `
INSERT INTO collector_instances (instance_id, collector_kind, mode, enabled, bootstrap, claims_enabled,
  display_name, configuration, last_observed_at, created_at, updated_at)
VALUES ('inst-git', 'git', 'continuous', true, false, true, 'Git', '{"scan_interval":"15m"}'::jsonb, now(), now(), now()),
       ('inst-aws', 'aws', 'continuous', true, false, true, 'AWS', '{}'::jsonb, now(), now(), now()),
       -- Activated from a component: the status reader marks it extension-hosted
       -- (#8951), which runtime health uses to bound its terminal-failure class.
       ('inst-ext', 'git', 'continuous', true, false, true, 'Component', '{"component_id":"acme.jira"}'::jsonb, now(), now(), now())`)
	mustExec(ctx, t, database, `
INSERT INTO workflow_runs (run_id, trigger_kind, status, created_at, updated_at)
SELECT 'run-' || i, 'schedule', CASE WHEN i % 7 = 0 THEN 'failed' ELSE 'completed' END,
       now() - interval '2 hours', now() - make_interval(mins => i % 90)
FROM generate_series(1, 20) AS i`)
	mustExec(ctx, t, database, `
INSERT INTO workflow_work_items (work_item_id, run_id, collector_kind, collector_instance_id, source_system,
  scope_id, acceptance_unit_id, source_run_id, generation_id, status, visible_at, last_failure_class,
  last_failure_operation, created_at, updated_at)
SELECT 'wi-' || i, 'run-' || (1 + i % 20),
       CASE WHEN i % 2 = 0 THEN 'git' ELSE 'aws' END, CASE WHEN i % 2 = 0 THEN 'inst-git' ELSE 'inst-aws' END,
       CASE WHEN i % 2 = 0 THEN 'git' ELSE 'aws' END,
       'scope-' || (i % 40), 'au-' || i, 'sr-' || i, 'gen-' || i,
       CASE i % 10 WHEN 0 THEN 'pending' WHEN 1 THEN 'claimed' WHEN 2 THEN 'failed_retryable'
                   WHEN 3 THEN 'failed_terminal' WHEN 4 THEN 'expired' ELSE 'completed' END,
       CASE WHEN i % 10 = 2 THEN now() + interval '5 minutes' ELSE now() - interval '1 minute' END,
       CASE WHEN i % 10 IN (2, 3, 4) THEN 'timeout' END,
       CASE WHEN i % 10 = 3 THEN 'fetch' END,
       now() - make_interval(mins => 60 - (i % 50)), now() - make_interval(mins => 5 + (i % 20))
FROM generate_series(1, $1) AS i`, total)
	mustExec(ctx, t, database, `
INSERT INTO workflow_claims (claim_id, work_item_id, fencing_token, owner_id, status, claimed_at, heartbeat_at,
  lease_expires_at, created_at, updated_at)
SELECT 'cl-' || work_item_id, work_item_id, 1, 'owner', 'active', now() - interval '2 minutes', now(),
       now() + interval '3 minutes', now(), now()
FROM workflow_work_items WHERE status = 'claimed'`)
	mustExec(ctx, t, database, `
INSERT INTO workflow_run_completeness (run_id, collector_kind, phase_name, status, observed_at, updated_at)
VALUES ('run-1', 'git', 'p', 'blocked', now(), now()), ('run-2', 'git', 'p', 'complete', now(), now())`)
	mustExec(ctx, t, database, `
INSERT INTO collector_generation_dead_letters (generation_id, scope_id, collector_kind, source_system, scope_kind,
  partition_key, trigger_kind, failure_class, failure_message, status, first_dead_lettered_at,
  last_dead_lettered_at, updated_at)
VALUES ('dl-1', 'scope-1', 'git', 'git', 'repository', 'p', 'schedule', 'timeout', 'm', 'dead_letter', now(), now(), now())`)
	// Acknowledge the newest terminal failure of the aws instance, so the
	// snapshot carries both an unacknowledged and an acknowledged failure.
	mustExec(ctx, t, database, `
INSERT INTO collector_terminal_failure_acknowledgements (collector_kind, collector_instance_id, scope_id,
  failure_class, failure_at, work_item_id, reason, actor_class, acknowledged_at)
SELECT collector_kind, collector_instance_id, scope_id, last_failure_class, updated_at, work_item_id,
       'expected', 'operator', now()
FROM workflow_work_items WHERE collector_instance_id = 'inst-aws' AND status = 'failed_terminal'
ORDER BY updated_at DESC, work_item_id ASC LIMIT 1`)
}

// coordinatorRowAndLive returns the stored coordinator row and the live
// coordinator entries recomputed at the row's as_of, both from one REPEATABLE
// READ snapshot.
func coordinatorRowAndLive(ctx context.Context, t *testing.T, database *sql.DB) (store.Row, []store.Entry) {
	t.Helper()
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		t.Fatalf("begin snapshot: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	queryer := txQueryer{tx}
	row, err := store.Read(ctx, queryer, store.ModelCoordinatorSnapshot)
	if err != nil {
		t.Fatalf("read the coordinator row: %v", err)
	}
	live, err := postgres.ReadCoordinatorSummaryEntries(ctx, queryer, row.AsOf)
	if err != nil {
		t.Fatalf("run the live coordinator read at %s: %v", row.AsOf, err)
	}
	return row, live
}

// zeroAges clears every clock-relative duration so a stored snapshot (aged to
// the read) and a live one (computed at another instant) compare on counts.
func zeroAges(c *statuspkg.CoordinatorSnapshot) {
	if c == nil {
		return
	}
	// The driver hands back instance times in the session zone; the stored row
	// holds the same instants in UTC, and every renderer formats them as UTC.
	for i := range c.CollectorInstances {
		c.CollectorInstances[i].LastObservedAt = c.CollectorInstances[i].LastObservedAt.UTC()
		c.CollectorInstances[i].UpdatedAt = c.CollectorInstances[i].UpdatedAt.UTC()
	}
	c.OldestPendingAge = 0
	for i := range c.CollectorBackpressure {
		c.CollectorBackpressure[i].OldestPendingAge = 0
		c.CollectorBackpressure[i].OldestRetryAge = 0
		c.CollectorBackpressure[i].OldestClaimAge = 0
		c.CollectorBackpressure[i].NextRetryDelay = 0
	}
}

// TestCoordinatorModelServedEqualToLiveLive is the accuracy proof of the
// coordinator_snapshot model.
func TestCoordinatorModelServedEqualToLiveLive(t *testing.T) {
	ctx, database := openWriterDatabase(t)
	writer := newLiveWriterWithCoordinator(database)
	for _, fixture := range []struct {
		name  string
		total int
	}{{"empty", 0}, {"populated", 400}} {
		t.Run(fixture.name, func(t *testing.T) {
			seedCoordinator(ctx, t, database, fixture.total)
			pass := writer.RunOnce(ctx)
			if pass.Outcome != statussummary.OutcomeOK || len(pass.Models) != 2 || pass.Models[1].Outcome != statussummary.OutcomeOK {
				t.Fatalf("RunOnce() = %+v, want ok for both models", pass)
			}
			row, live := coordinatorRowAndLive(ctx, t, database)
			if row.SourceSHA256 != postgres.CoordinatorSummarySourceSHA256() || row.SchemaVersion != store.SchemaVersion || row.RowCount != len(row.Entries) {
				t.Fatalf("stored row version/digest/count = %d/%s/%d", row.SchemaVersion, row.SourceSHA256, row.RowCount)
			}
			if !reflect.DeepEqual(row.Entries, live) {
				t.Fatalf("stored coordinator row (%d entries) differs from the live read at as_of %s (%d entries):\nstored: %v\nlive:   %v",
					len(row.Entries), row.AsOf, len(live), row.Entries, live)
			}
			if fixture.total == 0 && len(row.Entries) == 0 {
				return // no coordinator state: the live read is nil and stores no entries
			}

			// The production reader, in one snapshot, serves the stored row and
			// its counts equal the live read's.
			tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			on := postgres.NewStatusStore(txQueryer{tx}).WithSummaryReader(postgres.NewStatusSummaryReaderWithConfig(store.ReadConfig{Enabled: true, StaleAfter: time.Hour}))
			off := postgres.NewStatusStore(txQueryer{tx}).WithSummaryReader(postgres.NewStatusSummaryReaderWithConfig(store.ReadConfig{}))
			model, err := on.ReadStatusSnapshotFiltered(ctx, time.Now(), statuspkg.SnapshotSelection{})
			if err != nil {
				t.Fatal(err)
			}
			liveSnap, err := off.ReadStatusSnapshotFiltered(ctx, time.Now(), statuspkg.SnapshotSelection{})
			if err != nil {
				t.Fatal(err)
			}
			if src := model.CoordinatorSource; src.Source != statuspkg.ActiveWorkSourceModel || src.Reason != statuspkg.ActiveWorkReasonFresh {
				t.Fatalf("coordinator source = %+v, want model/fresh", src)
			}
			if src := liveSnap.CoordinatorSource; src.Source != statuspkg.ActiveWorkSourceLive {
				t.Fatalf("comparison snapshot was not live: %+v", src)
			}
			zeroAges(model.Coordinator)
			zeroAges(liveSnap.Coordinator)
			if !reflect.DeepEqual(model.Coordinator, liveSnap.Coordinator) {
				t.Fatalf("served coordinator differs from the live read (ages excluded):\nmodel: %+v recent=%+v\nlive:  %+v recent=%+v", model.Coordinator, model.Coordinator.RecentFailures, liveSnap.Coordinator, liveSnap.Coordinator.RecentFailures)
			}
			var extensionHosted int
			for _, instance := range model.Coordinator.CollectorInstances {
				if instance.ExtensionHosted {
					extensionHosted++
				}
			}
			if extensionHosted != 1 {
				t.Fatalf("served %d extension-hosted instances, want exactly the component-activated one", extensionHosted)
			}
			var acknowledged, open int
			for _, failure := range model.Coordinator.CollectorTerminalFailures {
				if failure.Acknowledged {
					acknowledged++
				} else {
					open++
				}
			}
			if acknowledged == 0 || open == 0 || len(model.Coordinator.CollectorBackpressure) == 0 {
				t.Fatalf("fixture produced %d acknowledged and %d open terminal failures and %d backpressure rows; all must be present",
					acknowledged, open, len(model.Coordinator.CollectorBackpressure))
			}
		})
	}
}

// TestCoordinatorWritersBesideQueueTrafficLive races four writer replicas on
// the same ticks while workers claim and complete workflow work items. Each
// pass takes only the advisory try-lock and plain MVCC reads, so no replica
// errors, none blocks a claim, the guarded upsert keeps the stored as_of
// monotonic, and exactly one decodable row remains.
func TestCoordinatorWritersBesideQueueTrafficLive(t *testing.T) {
	ctx, database := openWriterDatabase(t)
	database.SetMaxOpenConns(32)
	seedCoordinator(ctx, t, database, 2000)

	var claimsDone atomic.Int64
	var slowestClaim atomic.Int64
	trafficCtx, stopTraffic := context.WithCancel(ctx)
	var traffic sync.WaitGroup
	for range 4 {
		traffic.Add(1)
		go func() {
			defer traffic.Done()
			for trafficCtx.Err() == nil {
				start := time.Now()
				// The coordinator's claim shape: lock a pending row with SKIP
				// LOCKED, then mark it claimed and bump updated_at.
				if _, err := database.ExecContext(trafficCtx, `
UPDATE workflow_work_items SET status = CASE WHEN status = 'pending' THEN 'claimed' ELSE 'pending' END, updated_at = now()
WHERE work_item_id IN (
  SELECT work_item_id FROM workflow_work_items WHERE status IN ('pending', 'claimed') ORDER BY updated_at LIMIT 5 FOR UPDATE SKIP LOCKED)`); err != nil {
					if trafficCtx.Err() == nil {
						t.Errorf("claim traffic: %v", err)
					}
					return
				}
				claimsDone.Add(1)
				if d := int64(time.Since(start)); d > slowestClaim.Load() {
					slowestClaim.Store(d)
				}
			}
		}()
	}

	const replicas, rounds = 4, 6
	var okByModel, skippedByModel, errored atomic.Int64
	var lastAsOf atomic.Int64
	for range rounds {
		var wg sync.WaitGroup
		for range replicas {
			wg.Add(1)
			go func() {
				defer wg.Done()
				writer := newLiveWriterWithCoordinator(database)
				pass := writer.RunOnce(ctx)
				for _, model := range pass.Models {
					switch model.Outcome {
					case statussummary.OutcomeOK:
						if model.ModelKey == store.ModelCoordinatorSnapshot {
							okByModel.Add(1)
						}
					case statussummary.OutcomeSkippedLock, statussummary.OutcomeRejectedGuard:
						if model.ModelKey == store.ModelCoordinatorSnapshot {
							skippedByModel.Add(1)
						}
					default:
						errored.Add(1)
						t.Errorf("model %s outcome %s: %v", model.ModelKey, model.Outcome, model.Err)
					}
				}
			}()
		}
		wg.Wait()
		var asOf time.Time
		if err := database.QueryRowContext(ctx, `SELECT as_of FROM status_summary_snapshots WHERE model_key = $1`, store.ModelCoordinatorSnapshot).Scan(&asOf); err != nil {
			t.Fatal(err)
		}
		if prev := lastAsOf.Swap(asOf.UnixNano()); asOf.UnixNano() < prev {
			t.Fatalf("stored as_of went backwards: %s after %s", asOf, time.Unix(0, prev))
		}
	}
	stopTraffic()
	traffic.Wait()

	if errored.Load() != 0 {
		t.Fatalf("%d model transactions errored", errored.Load())
	}
	if okByModel.Load() < rounds || okByModel.Load()+skippedByModel.Load() != replicas*rounds {
		t.Fatalf("coordinator outcomes ok=%d skipped=%d, want at least one ok per round and %d in all", okByModel.Load(), skippedByModel.Load(), replicas*rounds)
	}
	if claimsDone.Load() == 0 {
		t.Fatal("claim traffic made no progress while the writers ran")
	}
	if slowest := time.Duration(slowestClaim.Load()); slowest > 5*time.Second {
		t.Fatalf("a claim took %s beside the writers; the passes must not block queue traffic", slowest)
	}
	var rows int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM status_summary_snapshots WHERE model_key = $1`, store.ModelCoordinatorSnapshot).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("coordinator rows = %d (%v), want exactly one", rows, err)
	}
	// The queue stopped, so the final row equals the live read at its as_of
	// only if no work item changed after the last pass; settle with one pass.
	if pass := newLiveWriterWithCoordinator(database).RunOnce(ctx); pass.Models[1].Outcome != statussummary.OutcomeOK {
		t.Fatalf("settling pass = %+v", pass)
	}
	row, live := coordinatorRowAndLive(ctx, t, database)
	if !reflect.DeepEqual(row.Entries, live) {
		t.Fatal("the final stored row differs from the live read at its as_of")
	}
}
