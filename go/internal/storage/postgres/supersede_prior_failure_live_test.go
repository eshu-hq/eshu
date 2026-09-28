// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
)

// #7320: every statement that supersedes a fact_work_items row overwrote its
// failure_class, message and details, erasing why a failed or dead-lettered row
// failed. These proofs drive the five writers through their production entry
// points against real PostgreSQL (set ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN)
// and assert the folded prior_failure by value.

const (
	pfProjectorClass   = "projector_superseded_by_newer_generation"
	pfProjectorMessage = "projector work superseded by newer same-scope generation"
)

type pfCase struct {
	name      string
	seed      pfSeed
	wantPrior bool
}

// pfPresenceCases is the presence rule over the states a sweep can hit.
func pfPresenceCases() []pfCase {
	return []pfCase{
		{"dead_letter", pfDeadLetter(), true},
		{"failed", pfFailed(), true},
		{"retrying_with_evidence", pfRetrying(), true},
		{"failed_all_null", pfFailedNull(), true},
		{"pending_never_failed", pfNeverFailed(), false},
		{"pending_blank_class", pfBlank(), false},
	}
}

func (c pfCase) want(class, message string, keys map[string]any) pfWant {
	w := pfWant{class: class, message: message, detailKeys: keys}
	if c.wantPrior {
		seed := c.seed
		w.prior = &seed
	}
	return w
}

// TestSupersedeFoldsPriorFailureClaimSweep covers site 1 branch 1: the claim
// sweeps a stale pending or failed generation once a newer one has work.
func TestSupersedeFoldsPriorFailureClaimSweep(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	cases := pfPresenceCases()
	for i, tc := range cases {
		scopeID := "pf-claim-" + tc.name
		genStatus := "failed"
		if i%2 == 1 {
			genStatus = "pending"
		}
		pfSeedScope(t, database, scopeID, genStatus, "pending")
		pfInsertWork(t, database, "old-"+scopeID, scopeID, scopeID+"-gen-old", "projector", tc.seed)
		pfInsertWork(t, database, "new-"+scopeID, scopeID, scopeID+"-gen-new", "projector", pfNeverFailed())
	}
	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	if _, ok, err := queue.Claim(context.Background()); err != nil || !ok {
		t.Fatalf("Claim() ok=%v err=%v, want one newer-generation row claimed", ok, err)
	}
	for i, tc := range cases {
		scopeID := "pf-claim-" + tc.name
		genStatus := "failed"
		if i%2 == 1 {
			genStatus = "pending"
		}
		t.Run(tc.name, func(t *testing.T) {
			pfAssertSuperseded(t, pfRead(t, database, "old-"+scopeID), tc.want(pfProjectorClass, pfProjectorMessage, map[string]any{
				"scope_id": scopeID, "work_item_id": "old-" + scopeID,
				"generation_id": scopeID + "-gen-old", "generation_status": genStatus,
			}), tc.seed)
		})
	}
}

// TestSupersedeFoldsPriorFailureClaimSupersededGeneration covers site 1
// branch 2 (#7130): rows left on an already superseded generation, including
// an expired-lease zombie that still carries its last retry's evidence.
func TestSupersedeFoldsPriorFailureClaimSupersededGeneration(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	expired := -time.Minute
	cases := []pfCase{
		{"retrying_with_evidence", pfRetrying(), true},
		{"pending_never_failed", pfNeverFailed(), false},
		{"expired_claimed_carrying_retry", pfWithLease(pfRetrying(), "claimed", "zombie-worker", expired), true},
		{"expired_running_carrying_retry", pfWithLease(pfRetrying(), "running", "zombie-worker", expired), true},
		{"expired_running_never_failed", pfWithLease(pfNeverFailed(), "running", "zombie-worker", expired), false},
	}
	for _, tc := range cases {
		scopeID := "pf-supg-" + tc.name
		id := "old-" + scopeID
		seededStatus := tc.seed.status
		row := supersededClaimRow{workItemID: id, status: seededStatus}
		if tc.seed.claimUntil != nil {
			row.claimUntil = tc.seed.claimUntil
		}
		seedSupersededClaimScope(t, database, scopeID, row)
		pfApplySeed(t, database, id, tc.seed)
	}
	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	if _, ok, err := queue.Claim(context.Background()); err != nil || ok {
		t.Fatalf("Claim() ok=%v err=%v, want no claim of a retired generation", ok, err)
	}
	for _, tc := range cases {
		scopeID := "pf-supg-" + tc.name
		t.Run(tc.name, func(t *testing.T) {
			pfAssertSuperseded(t, pfRead(t, database, "old-"+scopeID), tc.want(pfProjectorClass, pfProjectorMessage, map[string]any{
				"scope_id": scopeID, "work_item_id": "old-" + scopeID,
				"generation_id": scopeID + "-gen-old", "generation_status": "superseded",
			}), tc.seed)
		})
	}
}

// TestSupersedeFoldsPriorFailureAck covers site 2: ProjectorQueue.Ack of a
// newer generation supersedes older terminal-or-pending work.
func TestSupersedeFoldsPriorFailureAck(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	for i, tc := range pfPresenceCases() {
		scopeID := "pf-ack-" + tc.name
		genStatus := "failed"
		if i%2 == 1 {
			genStatus = "pending"
		}
		t.Run(tc.name, func(t *testing.T) {
			pfSeedScope(t, database, scopeID, genStatus, "pending")
			pfInsertWork(t, database, "old-"+scopeID, scopeID, scopeID+"-gen-old", "projector", tc.seed)
			pfInsertWork(t, database, "new-"+scopeID, scopeID, scopeID+"-gen-new", "projector",
				pfWithLease(pfSeed{attempts: 1}, "claimed", "claimer", time.Minute))
			if err := queue.Ack(context.Background(), pfWork(scopeID, scopeID+"-gen-new", 1), runtime.Result{}); err != nil {
				t.Fatalf("Ack() error = %v", err)
			}
			pfAssertSuperseded(t, pfRead(t, database, "old-"+scopeID), tc.want(pfProjectorClass, pfProjectorMessage, map[string]any{
				"scope_id": scopeID, "work_item_id": "old-" + scopeID,
				"generation_id": scopeID + "-gen-old", "current_generation_id": scopeID + "-gen-new",
			}), tc.seed)
		})
	}
}

// TestSupersedeFoldsPriorFailureHeartbeat covers site 3: Heartbeat stops
// running work whose generation a newer one replaced, or that is already
// superseded. The work carries its last retry's evidence, never a terminal one.
func TestSupersedeFoldsPriorFailureHeartbeat(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	for _, tc := range []struct {
		name                string
		oldStatus, newState string
		seed                pfSeed
		wantPrior           bool
		class, message      string
		generationStatus    string
	}{
		{
			"replaced_by_newer_carrying_retry", "pending", "pending", pfWithLease(pfRetrying(), "running", "claimer", time.Minute), true,
			pfProjectorClass, "running projector work superseded by newer same-scope generation", "pending",
		},
		{
			"replaced_by_newer_never_failed", "pending", "pending", pfWithLease(pfNeverFailed(), "running", "claimer", time.Minute), false,
			pfProjectorClass, "running projector work superseded by newer same-scope generation", "pending",
		},
		{
			"own_generation_superseded_carrying_retry", "superseded", "active", pfWithLease(pfRetrying(), "running", "claimer", time.Minute), true,
			projectorHeartbeatGenerationSupersededClass, "running projector work stopped: generation already superseded", "superseded",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scopeID := "pf-hb-" + tc.name
			pfSeedScope(t, database, scopeID, tc.oldStatus, tc.newState)
			pfInsertWork(t, database, "old-"+scopeID, scopeID, scopeID+"-gen-old", "projector", tc.seed)
			err := queue.Heartbeat(context.Background(), pfWork(scopeID, scopeID+"-gen-old", tc.seed.attempts))
			if !errors.Is(err, failure.ErrWorkSuperseded) {
				t.Fatalf("Heartbeat() error = %v, want ErrWorkSuperseded", err)
			}
			c := pfCase{seed: tc.seed, wantPrior: tc.wantPrior}
			pfAssertSuperseded(t, pfRead(t, database, "old-"+scopeID), c.want(tc.class, tc.message, map[string]any{
				"scope_id": scopeID, "work_item_id": "old-" + scopeID,
				"generation_id": scopeID + "-gen-old", "generation_status": tc.generationStatus,
			}), tc.seed)
		})
	}
}

// TestSupersedeFoldsPriorFailureAckRefusal covers site 4: an Ack the fence
// refuses because its generation is already superseded.
func TestSupersedeFoldsPriorFailureAckRefusal(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	queue := NewProjectorQueue(SQLDB{DB: database}, "claimer", time.Minute)
	for _, tc := range []pfCase{
		{"claimed_carrying_retry", pfWithLease(pfRetrying(), "claimed", "claimer", time.Minute), true},
		{"claimed_never_failed", pfWithLease(pfNeverFailed(), "claimed", "claimer", time.Minute), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scopeID := "pf-refuse-" + tc.name
			pfSeedScope(t, database, scopeID, "superseded", "active")
			pfInsertWork(t, database, "old-"+scopeID, scopeID, scopeID+"-gen-old", "projector", tc.seed)
			err := queue.Ack(context.Background(), pfWork(scopeID, scopeID+"-gen-old", tc.seed.attempts), runtime.Result{})
			if !errors.Is(err, failure.ErrWorkSuperseded) {
				t.Fatalf("Ack() error = %v, want ErrWorkSuperseded", err)
			}
			pfAssertSuperseded(t, pfRead(t, database, "old-"+scopeID),
				tc.want(projectorAckGenerationSupersededClass, "projector ack refused: generation already superseded", map[string]any{
					"scope_id": scopeID, "work_item_id": "old-" + scopeID, "generation_id": scopeID + "-gen-old",
				}), tc.seed)
		})
	}
}

// TestSupersedeFoldsPriorFailureReducerClaim covers site 5: the reducer claim's
// inactive-generation sweep, through both ReducerQueue.Claim and ClaimBatch.
func TestSupersedeFoldsPriorFailureReducerClaim(t *testing.T) {
	dsn := claimMaintenanceProofDSN(t)
	for _, mode := range []string{"claim", "claim_batch"} {
		t.Run(mode, func(t *testing.T) {
			database := openClaimDeadlockProofDB(t, dsn, 2)
			cases := pfPresenceCases()
			for _, tc := range cases {
				scopeID := "pf-red-" + tc.name
				pfSeedScope(t, database, scopeID, "superseded", "active")
				pfInsertWork(t, database, "old-"+scopeID, scopeID, scopeID+"-gen-old", "reducer", tc.seed)
			}
			queue := NewReducerQueue(SQLDB{DB: database}, "reducer-claimer", time.Minute)
			var err error
			if mode == "claim" {
				_, _, err = queue.Claim(context.Background())
			} else {
				_, err = queue.ClaimBatch(context.Background(), 4)
			}
			if err != nil {
				t.Fatalf("reducer %s error = %v", mode, err)
			}
			for _, tc := range cases {
				scopeID := "pf-red-" + tc.name
				t.Run(tc.name, func(t *testing.T) {
					pfAssertSuperseded(t, pfRead(t, database, "old-"+scopeID),
						tc.want("reducer_superseded_by_newer_active_generation",
							"reducer work superseded by newer active generation", map[string]any{
								"reason": "inactive_generation", "scope_id": scopeID, "work_item_id": "old-" + scopeID,
								"generation_id": scopeID + "-gen-old", "active_generation_id": scopeID + "-gen-new",
								"domain": "zz_probe_domain",
							}), tc.seed)
				})
			}
		})
	}
}

// ncCheck runs the no-candidate proof for one claim statement over a seed with
// staleRows stale scopes and returns what it found wrong. The before text (the
// shipped statement with the fold cut out) and the after text (as shipped) must
// run the same plan; each supersede UPDATE must write 0 rows; and a real run of
// each text must supersede nothing.
func ncCheck(t *testing.T, database *sql.DB, st ncStatement, staleRows int) []string {
	t.Helper()
	const scopes = 3500
	projector := st.name == "projector_claim"
	texts := []struct{ name, text string }{{"before", costBeforeText(t, st.name, st.text)}, {"after", st.text}}
	var problems []string
	plans := map[string]ncPlan{}
	for _, v := range texts {
		// Every EXPLAIN runs on a freshly seeded table. EXPLAIN ANALYZE of a
		// claim executes its UPDATEs and the rollback leaves dead tuples, and
		// the planner reads the table's physical size, so the reducer batch
		// claim's join order (a near tie) flips after a few runs on one seed
		// whichever text runs. Fresh state for both texts keeps the comparison
		// about the text.
		ncSeed(t, database, projector, scopes, staleRows)
		plan := ncExplain(t, database, v.text, st.args(time.Now().UTC()))
		plans[v.name] = plan
		for _, cte := range st.cteNames {
			rows, found := plan.rowsWritten(cte)
			switch {
			case !found:
				problems = append(problems, fmt.Sprintf("%s %s: plan has no UPDATE in CTE %s", st.name, v.name, cte))
			case rows != 0:
				problems = append(problems, fmt.Sprintf("%s %s: %s wrote %.0f rows; the no-candidate claim writes 0 rows", st.name, v.name, cte, rows))
			}
		}
		t.Logf("PLAN %s %s: planning_time=%.3fms top_buffers[hit=%d read=%d dirtied=%d written=%d] nodes=%d",
			st.name, v.name, plan.planning, plan.root.SharedHit, plan.root.SharedRead, plan.root.SharedDirtied, plan.root.SharedWritten, len(plan.shape()))
	}
	if b, a := plans["before"].shape(), plans["after"].shape(); !reflect.DeepEqual(b, a) {
		problems = append(problems, fmt.Sprintf("%s: before and after plans differ:\nbefore %v\nafter  %v", st.name, b, a))
	}
	for _, v := range texts {
		ncSeed(t, database, projector, scopes, staleRows)
		if _, err := costRun(context.Background(), database, v.text, st.args(time.Now().UTC())); err != nil {
			t.Fatalf("%s %s: %v", st.name, v.name, err)
		}
		var superseded int
		if err := database.QueryRow(`SELECT count(*) FROM fact_work_items WHERE status = 'superseded'`).Scan(&superseded); err != nil {
			t.Fatalf("%s %s: count superseded: %v", st.name, v.name, err)
		}
		if superseded != 0 {
			problems = append(problems, fmt.Sprintf("%s %s: a real run superseded %d work rows; the no-candidate claim writes 0 rows", st.name, v.name, superseded))
		}
	}
	return problems
}

// TestSupersedeNoCandidateClaimRunsSamePlanAndWritesNothing is the
// deterministic no-candidate proof for #7320: a claim with nothing to sweep is
// the common case, and the fold must add no node to it, write no row and
// supersede nothing. It holds on any host: it compares plan shape and row
// counts, not time. Buffers and planning time are logged, not asserted.
func TestSupersedeNoCandidateClaimRunsSamePlanAndWritesNothing(t *testing.T) {
	database := openClaimDeadlockProofDB(t, claimMaintenanceProofDSN(t), 2)
	for _, st := range ncStatements() {
		t.Run(st.name, func(t *testing.T) {
			for _, problem := range ncCheck(t, database, st, 0) {
				t.Error(problem)
			}
		})
	}
}

// TestSupersedeNoCandidateProofRejectsAStaleRow is the seeded RED for the proof
// above: one stale scope among the 3,500 makes the supersede UPDATE write a
// row, and the same checks must fail on "writes 0 rows". A proof that passes
// here would be checking nothing.
func TestSupersedeNoCandidateProofRejectsAStaleRow(t *testing.T) {
	database := openClaimDeadlockProofDB(t, claimMaintenanceProofDSN(t), 2)
	for _, st := range ncStatements() {
		t.Run(st.name, func(t *testing.T) {
			problems := ncCheck(t, database, st, 1)
			for _, p := range problems {
				t.Logf("seeded RED, the proof fails as it must: %s", p)
			}
			for _, variant := range []string{"before", "after"} {
				var hit bool
				for _, p := range problems {
					hit = hit || strings.Contains(p, st.name+" "+variant+": ") && strings.Contains(p, "writes 0 rows")
				}
				if !hit {
					t.Errorf("%s %s: seeded stale row did not fail on \"writes 0 rows\"; problems: %v", st.name, variant, problems)
				}
			}
		})
	}
}
