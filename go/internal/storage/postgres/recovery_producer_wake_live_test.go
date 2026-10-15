// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
)

// wakeFixture is the seeded queue state of the #7597 wake proof.
type wakeFixture struct {
	scope, gen string
	wmEntity   string
	wmID       string
}

// wakeRow is one seeded terminal or retrying reducer row.
type wakeRow struct {
	id, scope, gen string
	domain         reducer.Domain
	status, class  string
}

// seedWakeFixture seeds a dead-lettered deployment_mapping producer, its two
// producer-terminal consumers, and controls the wake must not touch: the same
// consumer class on an older generation and on another scope, another class
// on the same generation, and a retrying consumer.
func seedWakeFixture(t *testing.T, ctx context.Context, db *sql.DB, at time.Time) wakeFixture {
	t.Helper()
	f := wakeFixture{scope: "git-repository-scope:acme/app", gen: "gen-1", wmEntity: "repo:repo-edge-api"}
	f.wmID = reducerWorkItemID(runtime.ReducerIntent{
		ScopeID: f.scope, GenerationID: f.gen, Domain: reducer.DomainWorkloadMaterialization, EntityKey: f.wmEntity,
	})
	seedOwnGateScope(t, ctx, db, f.scope, f.gen, at)
	addOwnGateGeneration(t, ctx, db, f.scope, "gen-0", at.Add(-time.Hour), "superseded")
	seedOwnGateScope(t, ctx, db, "git-repository-scope:acme/other", "gen-other", at)
	duc := crossrepo.DeployableUnitCorrelationProducerDeadLetteredFailureClass
	rows := []wakeRow{
		{"dm-1", f.scope, f.gen, reducer.DomainDeploymentMapping, "dead_letter", "retry_exhausted"},
		{"duc-1", f.scope, f.gen, reducer.DomainDeployableUnitCorrelation, "dead_letter", duc},
		{
			f.wmID, f.scope, f.gen, reducer.DomainWorkloadMaterialization, "dead_letter",
			crossrepo.WorkloadMaterializationProducerDeadLetteredFailureClass,
		},
		{"duc-old-gen", f.scope, "gen-0", reducer.DomainDeployableUnitCorrelation, "dead_letter", duc},
		{"duc-other-scope", "git-repository-scope:acme/other", "gen-other", reducer.DomainDeployableUnitCorrelation, "dead_letter", duc},
		{"code-call-1", f.scope, f.gen, reducer.DomainCodeCallMaterialization, "dead_letter", "graph_write_timeout"},
		{
			"duc-retrying", f.scope, f.gen, reducer.DomainDeployableUnitCorrelation, "retrying",
			reducer.DeployableUnitCorrelationResolutionNotReadyFailureClass,
		},
	}
	for _, row := range rows {
		addOwnGateItem(t, ctx, db, row.id, row.scope, row.gen, row.domain, at)
		if _, err := db.ExecContext(ctx, `UPDATE fact_work_items
SET status = $2, failure_class = $3, failure_message = 'seeded', attempt_count = 3
WHERE work_item_id = $1`, row.id, row.status, row.class); err != nil {
			t.Fatalf("seed %s: %v", row.id, err)
		}
	}
	return f
}

// wakeRowState is the post-replay state of one row.
type wakeRowState struct {
	status   string
	class    string
	attempts int
	reopened sql.NullTime
}

func readWakeRow(t *testing.T, ctx context.Context, db *sql.DB, id string) wakeRowState {
	t.Helper()
	var s wakeRowState
	var class sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT status, failure_class, attempt_count, reopened_at
FROM fact_work_items WHERE work_item_id = $1`, id).Scan(&s.status, &class, &s.attempts, &s.reopened); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	s.class = class.String
	return s
}

// TestReplayDeadLetteredDeploymentMappingWakesProducerTerminalConsumersLive is
// RED on main: replaying the dead-lettered deployment_mapping row leaves its
// producer-terminal consumers dead-lettered forever, and the repo-dependency
// runner's workload replay reports NotScheduled (fail closed, #7670).
func TestReplayDeadLetteredDeploymentMappingWakesProducerTerminalConsumersLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #7597 wake proof")
	}
	for _, limit := range []int{0, 10} {
		t.Run(map[int]string{0: "unbounded", 10: "bounded"}[limit], func(t *testing.T) {
			ctx := context.Background()
			db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
			clock := time.Now().UTC().Truncate(time.Microsecond)
			f := seedWakeFixture(t, ctx, db, clock.Add(-2*time.Hour))
			q := newOwnGateQueue(db, &clock)

			before, err := q.ReplayWorkloadMaterializationOutcome(ctx, f.scope, f.gen, f.wmEntity)
			if err != nil {
				t.Fatalf("pre-wake workload replay: %v", err)
			}
			if before != reducer.WorkloadMaterializationReplayNotScheduled {
				t.Fatalf("pre-wake workload replay outcome = %v, want NotScheduled", before)
			}

			result, err := NewRecoveryStore(SQLDB{DB: db}).ReplayFailedWorkItems(ctx, recovery.ReplayFilter{
				Stage: recovery.StageReducer, ScopeIDs: []string{f.scope}, FailureClass: "retry_exhausted", Limit: limit,
			}, clock)
			if err != nil {
				t.Fatalf("ReplayFailedWorkItems: %v", err)
			}
			if !slices.Equal(result.WorkItemIDs, []string{"dm-1"}) {
				t.Fatalf("replayed = %v, want [dm-1]", result.WorkItemIDs)
			}
			wantWoken := []string{"duc-1", f.wmID}
			slices.Sort(wantWoken)
			for _, id := range append(slices.Clone(wantWoken), "dm-1") {
				got := readWakeRow(t, ctx, db, id)
				if got.status != "pending" || got.class != "" || !got.reopened.Valid || !got.reopened.Time.Equal(clock) {
					t.Fatalf("%s after replay = %+v, want pending, no class, reopened_at=%s", id, got, clock)
				}
			}
			for _, id := range append(slices.Clone(wantWoken), "dm-1") {
				if got := readWakeRow(t, ctx, db, id); got.attempts != 3 {
					t.Fatalf("%s attempt_count = %d, want 3 (replay keeps retry evidence)", id, got.attempts)
				}
			}

			if !slices.Equal(result.WokenConsumerIDs, wantWoken) {
				t.Fatalf("woken = %v, want %v", result.WokenConsumerIDs, wantWoken)
			}
			for id, want := range map[string]string{
				"duc-old-gen": "dead_letter", "duc-other-scope": "dead_letter",
				"code-call-1": "dead_letter", "duc-retrying": "retrying",
			} {
				if got := readWakeRow(t, ctx, db, id); got.status != want || got.reopened.Valid {
					t.Fatalf("control %s = %+v, want untouched %s", id, got, want)
				}
			}

			after, err := q.ReplayWorkloadMaterializationOutcome(ctx, f.scope, f.gen, f.wmEntity)
			if err != nil {
				t.Fatalf("post-wake workload replay: %v", err)
			}
			if after != reducer.WorkloadMaterializationReplayScheduled {
				t.Fatalf("post-wake workload replay outcome = %v, want Scheduled", after)
			}
			runProducerThenConsumer(t, ctx, db, &clock, f)
		})
	}
}

// runProducerThenConsumer runs the replayed deployment_mapping to success
// (activating the own relationship generation), then the woken workload item
// through the production own gate, and acks both.
func runProducerThenConsumer(t *testing.T, ctx context.Context, db *sql.DB, clock *time.Time, f wakeFixture) {
	t.Helper()
	dmq := newOwnGateQueue(db, clock, reducer.DomainDeploymentMapping)
	dm, ok, err := dmq.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim replayed deployment_mapping: ok=%v err=%v", ok, err)
	}
	if err := NewRelationshipStore(SQLDB{DB: db}).ActivateResolutionGeneration(ctx, f.gen, f.scope); err != nil {
		t.Fatalf("activate resolution generation: %v", err)
	}
	if err := dmq.Ack(ctx, dm, reducer.Result{IntentID: dm.IntentID, Domain: dm.Domain, Status: reducer.ResultStatusSucceeded}); err != nil {
		t.Fatalf("ack deployment_mapping: %v", err)
	}
	wmq := newOwnGateQueue(db, clock, reducer.DomainWorkloadMaterialization)
	wm, ok, err := wmq.Claim(ctx)
	if err != nil || !ok || wm.IntentID != f.wmID {
		t.Fatalf("claim woken workload item: ok=%v id=%s err=%v", ok, wm.IntentID, err)
	}
	if err := consumerFor(db, reducer.DomainWorkloadMaterialization).run(ctx, wm); err != nil {
		t.Fatalf("woken workload materialization after the producer activated: %v", err)
	}
	if err := wmq.Ack(ctx, wm, reducer.Result{IntentID: wm.IntentID, Domain: wm.Domain, Status: reducer.ResultStatusSucceeded}); err != nil {
		t.Fatalf("ack workload item: %v", err)
	}
	if got := readWakeRow(t, ctx, db, f.wmID); got.status != "succeeded" {
		t.Fatalf("workload item = %+v, want succeeded", got)
	}
}
