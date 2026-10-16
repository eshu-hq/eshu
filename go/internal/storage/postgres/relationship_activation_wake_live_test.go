// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
)

// The #7597 activation-wake live proofs: every statement that activates a
// relationship generation reopens, in the same transaction, the own-gate
// consumers of that scope generation that dead-lettered in an own-gate
// counting class. Gated on ESHU_REDUCER_FAIRNESS_PROOF_DSN or
// ESHU_POSTGRES_DSN.

// wakeFixture is the seeded queue state of one activation-wake proof.
type wakeFixture struct {
	scope, gen string
	wmEntity   string
	wmID       string
}

// wakeRowState is the state of one row the proofs assert.
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

// setWakeRow moves one seeded row to a status and class.
func setWakeRow(t *testing.T, ctx context.Context, db *sql.DB, id, status, class string, attempts int) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items
SET status = $2, failure_class = NULLIF($3, ''), failure_message = 'seeded', attempt_count = $4
WHERE work_item_id = $1`, id, status, class, attempts); err != nil {
		t.Fatalf("set %s: %v", id, err)
	}
}

// seedWakeFixture seeds a scope generation with its deployment_mapping row,
// three dead-lettered own-gate consumers (two producer-dead-lettered, one
// producer-missing), and controls the wake must not touch: the same class on
// an older generation and on another scope, another class on the same
// generation, and a retrying consumer.
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
	for _, row := range []struct {
		id, scope, gen string
		domain         reducer.Domain
		status, class  string
	}{
		{"dm-1", f.scope, f.gen, reducer.DomainDeploymentMapping, "pending", ""},
		{"duc-1", f.scope, f.gen, reducer.DomainDeployableUnitCorrelation, "dead_letter", duc},
		{
			f.wmID, f.scope, f.gen, reducer.DomainWorkloadMaterialization, "dead_letter",
			crossrepo.WorkloadMaterializationProducerDeadLetteredFailureClass,
		},
		{
			"duc-missing", f.scope, f.gen, reducer.DomainDeployableUnitCorrelation, "dead_letter",
			crossrepo.DeployableUnitCorrelationProducerMissingFailureClass,
		},
		{"duc-old-gen", f.scope, "gen-0", reducer.DomainDeployableUnitCorrelation, "dead_letter", duc},
		{"duc-other-scope", "git-repository-scope:acme/other", "gen-other", reducer.DomainDeployableUnitCorrelation, "dead_letter", duc},
		{"code-call-1", f.scope, f.gen, reducer.DomainCodeCallMaterialization, "dead_letter", "graph_write_timeout"},
		{
			"duc-retrying", f.scope, f.gen, reducer.DomainDeployableUnitCorrelation, "retrying",
			reducer.DeployableUnitCorrelationResolutionNotReadyFailureClass,
		},
	} {
		addOwnGateItem(t, ctx, db, row.id, row.scope, row.gen, row.domain, at)
		if row.status != "pending" {
			setWakeRow(t, ctx, db, row.id, row.status, row.class, 3)
		}
	}
	return f
}

// activationPath runs one of the three production activation writers.
type activationPath struct {
	name     string
	activate func(t *testing.T, ctx context.Context, db *sql.DB, clock *time.Time, f wakeFixture) reducer.Intent
}

func activationPaths() []activationPath {
	return []activationPath{
		{"ActivateResolutionGenerationForClaim", func(t *testing.T, ctx context.Context, db *sql.DB, clock *time.Time, f wakeFixture) reducer.Intent {
			dm, ok, err := newOwnGateQueue(db, clock, reducer.DomainDeploymentMapping).Claim(ctx)
			if err != nil || !ok {
				t.Fatalf("claim deployment_mapping: ok=%v err=%v", ok, err)
			}
			if err := NewRelationshipStore(SQLDB{DB: db}).ActivateResolutionGenerationForClaim(
				ctx, f.gen, f.scope, dm.IntentID, *dm.ClaimedAt); err != nil {
				t.Fatalf("activate for claim: %v", err)
			}
			return dm
		}},
		{"ActivateResolutionGeneration", func(t *testing.T, ctx context.Context, db *sql.DB, _ *time.Time, f wakeFixture) reducer.Intent {
			if err := NewRelationshipStore(SQLDB{DB: db}).ActivateResolutionGeneration(ctx, f.gen, f.scope); err != nil {
				t.Fatalf("activate: %v", err)
			}
			return reducer.Intent{}
		}},
		{"CommitGeneration", func(t *testing.T, ctx context.Context, db *sql.DB, _ *time.Time, f wakeFixture) reducer.Intent {
			if _, err := db.ExecContext(ctx, `INSERT INTO relationship_generations (generation_id, scope, status, created_at)
VALUES ($1, $2, 'pending', now())`, f.gen, f.scope); err != nil {
				t.Fatalf("seed pending relationship generation: %v", err)
			}
			if err := NewRelationshipStore(SQLDB{DB: db}).CommitGeneration(ctx, f.gen, f.scope); err != nil {
				t.Fatalf("commit generation: %v", err)
			}
			return reducer.Intent{}
		}},
	}
}

// TestActivationWakesOwnGateConsumersLive is RED before #7597's activation
// wake: activating the relationship generation leaves its dead-lettered
// consumers dead-lettered, and the repo-dependency runner's workload replay
// reports NotScheduled (fail closed, #7670) even after the producer succeeded.
func TestActivationWakesOwnGateConsumersLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #7597 activation-wake proof")
	}
	for _, path := range activationPaths() {
		t.Run(path.name, func(t *testing.T) {
			ctx := context.Background()
			db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
			clock := time.Now().UTC()
			f := seedWakeFixture(t, ctx, db, clock.Add(-2*time.Hour))
			q := newOwnGateQueue(db, &clock)
			before, err := q.ReplayWorkloadMaterializationOutcome(ctx, f.scope, f.gen, f.wmEntity)
			if err != nil || before != reducer.WorkloadMaterializationReplayNotScheduled {
				t.Fatalf("pre-activation workload replay = %v, %v; want NotScheduled", before, err)
			}

			start := time.Now().UTC().Add(-time.Second)
			dm := path.activate(t, ctx, db, &clock, f)
			end := time.Now().UTC().Add(time.Second)
			for _, id := range []string{"duc-1", f.wmID, "duc-missing"} {
				got := readWakeRow(t, ctx, db, id)
				if got.status != "pending" || got.class != "" || !got.reopened.Valid ||
					got.reopened.Time.Before(start) || got.reopened.Time.After(end) {
					t.Fatalf("%s after activation = %+v, want pending, no class, reopened_at in [%s, %s]", id, got, start, end)
				}
				if got.attempts != 3 {
					t.Fatalf("%s attempt_count = %d, want 3 (the wake keeps retry evidence like a replay)", id, got.attempts)
				}
			}
			for id, want := range map[string]string{
				"duc-old-gen": "dead_letter", "duc-other-scope": "dead_letter",
				"code-call-1": "dead_letter", "duc-retrying": "retrying",
			} {
				if got := readWakeRow(t, ctx, db, id); got.status != want || got.reopened.Valid {
					t.Fatalf("control %s = %+v, want untouched %s", id, got, want)
				}
			}
			// #7670: the wake commits with the activation, so by the time the
			// repo-dependency lane replays the workload item it is pending.
			after, err := q.ReplayWorkloadMaterializationOutcome(ctx, f.scope, f.gen, f.wmEntity)
			if err != nil || after != reducer.WorkloadMaterializationReplayScheduled {
				t.Fatalf("post-activation workload replay = %v, %v; want Scheduled", after, err)
			}
			if dm.IntentID != "" {
				dmq := newOwnGateQueue(db, &clock, reducer.DomainDeploymentMapping)
				if err := dmq.Ack(ctx, dm, reducer.Result{IntentID: dm.IntentID, Domain: dm.Domain, Status: reducer.ResultStatusSucceeded}); err != nil {
					t.Fatalf("ack deployment_mapping: %v", err)
				}
			}
			runWokenWorkload(t, ctx, db, &clock, f)
		})
	}
}

// runWokenWorkload claims the woken workload item, runs it through the
// production own gate (now open), and acks it.
func runWokenWorkload(t *testing.T, ctx context.Context, db *sql.DB, clock *time.Time, f wakeFixture) {
	t.Helper()
	wmq := newOwnGateQueue(db, clock, reducer.DomainWorkloadMaterialization)
	wm, ok, err := wmq.Claim(ctx)
	if err != nil || !ok || wm.IntentID != f.wmID {
		t.Fatalf("claim woken workload item: ok=%v id=%s err=%v", ok, wm.IntentID, err)
	}
	if err := consumerFor(db, reducer.DomainWorkloadMaterialization).run(ctx, wm); err != nil {
		t.Fatalf("woken workload materialization after activation: %v", err)
	}
	if err := wmq.Ack(ctx, wm, reducer.Result{IntentID: wm.IntentID, Domain: wm.Domain, Status: reducer.ResultStatusSucceeded}); err != nil {
		t.Fatalf("ack workload item: %v", err)
	}
	if got := readWakeRow(t, ctx, db, f.wmID); got.status != "succeeded" {
		t.Fatalf("workload item = %+v, want succeeded", got)
	}
}
