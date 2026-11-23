// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11256 wide verdict sets through the real queue: ReducerQueue.Fail parks one wait row for 600 blockers and for one blocker beside 600 read-set repositories, Service.Run keeps going past a wide deferral and stops cleanly on shutdown, and a wait row parked before the key bound still wakes and audits
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

// realShapeScopeIDs returns n distinct sorted scope IDs of the shape the git
// collector mints, git-repository-scope:repository:r_<8 hex>, with random hex
// so the text does not compress the way sequential IDs do.
func realShapeScopeIDs(t testing.TB, n int) []string {
	t.Helper()
	seen := make(map[string]struct{}, n)
	out := make([]string, 0, n)
	buf := make([]byte, 4)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			t.Fatalf("random scope id: %v", err)
		}
		id := "git-repository-scope:repository:r_" + hex.EncodeToString(buf)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// wideWaitCause is a retryable workload-materialization readiness deferral
// carrying the verdict-time sets, the shape the reducer's own error has.
type wideWaitCause struct {
	repos    []string
	blocking []string
}

func (wideWaitCause) Error() string   { return "wide verdict deferral" }
func (wideWaitCause) Retryable() bool { return true }
func (wideWaitCause) FailureClass() string {
	return reducer.WorkloadMaterializationResolutionNotReadyFailureClass
}
func (e wideWaitCause) CorpusWaitRepoIDs() []string          { return e.repos }
func (e wideWaitCause) CorpusWaitBlockingScopeIDs() []string { return e.blocking }

// claimAndFailWide seeds one workload_materialization item, claims it through
// the real queue, and fails it with cause. It returns the claimed intent and
// the Fail error.
func claimAndFailWide(t *testing.T, ctx context.Context, db *sql.DB, itemID string, cause error) (reducercontract.Intent, error) {
	t.Helper()
	clock := time.Now().UTC()
	scope := "git-repository-scope:repository:r_waiter" + itemID
	seedOwnGateScope(t, ctx, db, scope, "gen-"+itemID, clock.Add(-time.Hour))
	addOwnGateItem(t, ctx, db, itemID, scope, "gen-"+itemID, reducer.DomainWorkloadMaterialization, clock.Add(-time.Minute))
	queue := newOwnGateQueue(db, &clock, reducer.DomainWorkloadMaterialization)
	intent, ok, err := queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim %s: ok=%v err=%v", itemID, ok, err)
	}
	return intent, queue.Fail(ctx, intent, cause)
}

// TestFailParksAWideVerdictSetLive reproduces the btree key limit through the
// real ReducerQueue.Fail on the migrated schema (#11256). A narrowed
// deferral's wait key carries the verdict-time blocking set and read set.
// The key is part of two btree indexes (the primary key and the wake index,
// migration 316), so a set of a few hundred git scope IDs, or one blocker
// beside a few hundred read-set repositories, fails the insert with SQLSTATE
// 54000 after the retry UPDATE already committed; the Fail error stops the
// reducer. The park must instead succeed with one wait row, leave the queue
// row retrying in its non-counting class, and not move the attempt count.
func TestFailParksAWideVerdictSetLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #11256 key-bound proof")
	}
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		cause wideWaitCause
	}{
		{"600 blocking scopes", wideWaitCause{repos: []string{"repo-1"}, blocking: realShapeScopeIDs(t, 600)}},
		{"one blocker and 600 read-set repositories", wideWaitCause{blocking: realShapeScopeIDs(t, 1), repos: realShapeScopeIDs(t, 600)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
			intent, err := claimAndFailWide(t, ctx, db, "wide-1", tc.cause)
			var pgErr interface{ SQLState() string }
			if err != nil && errors.As(err, &pgErr) {
				t.Fatalf("Fail returned SQLSTATE %s: %v", pgErr.SQLState(), err)
			}
			if err != nil {
				t.Fatalf("Fail = %v, want nil: a wide verdict set must park, not fail the reducer", err)
			}
			if got := countNarrowWaits(t, ctx, db, "wide-1"); got != 1 {
				t.Fatalf("wait rows = %d, want 1", got)
			}
			status, attempts, _ := narrowQueueState(t, ctx, db, "wide-1")
			if status != "retrying" || attempts != intent.AttemptCount {
				t.Fatalf("queue row = %s attempts %d, want retrying at the claimed attempt count %d", status, attempts, intent.AttemptCount)
			}
			var class string
			if err := db.QueryRowContext(ctx, `SELECT failure_class FROM fact_work_items WHERE work_item_id = 'wide-1'`).Scan(&class); err != nil {
				t.Fatalf("read failure class: %v", err)
			}
			if class != reducer.WorkloadMaterializationResolutionNotReadyFailureClass {
				t.Fatalf("failure class = %q, want the non-counting readiness class", class)
			}
		})
	}
}

// wideThenNormalExecutor fails the wide item with its wide verdict cause and
// succeeds every other item.
type wideThenNormalExecutor struct {
	wideID string
	cause  wideWaitCause
}

func (e wideThenNormalExecutor) Execute(_ context.Context, intent reducercontract.Intent) (reducercontract.Result, error) {
	if intent.IntentID == e.wideID {
		return reducercontract.Result{}, e.cause
	}
	return reducercontract.Result{
		IntentID: intent.IntentID, Domain: intent.Domain,
		Status: reducercontract.ResultStatusSucceeded, CompletedAt: time.Now().UTC(),
	}, nil
}

// TestServiceSurvivesAWideVerdictDeferralLive runs the production Service
// loop on the real queue with one wide-verdict item (claimed first) and one
// normal item (#11256). Before the bound, the wide item's park failed the
// btree key insert, Fail returned the error after the retry UPDATE had
// committed, and Service.Run returned it, which exits the whole reducer; the
// normal item was never acknowledged. Now the wide item parks and the normal
// item is acknowledged while Run keeps going until the context ends.
func TestServiceSurvivesAWideVerdictDeferralLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #11256 key-bound proof")
	}
	ctx := context.Background()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	now := time.Now().UTC()
	for _, item := range []struct {
		id string
		at time.Time
	}{{"svc-wide", now.Add(-2 * time.Hour)}, {"svc-normal", now.Add(-time.Hour)}} {
		scope := "git-repository-scope:repository:r_" + item.id
		seedOwnGateScope(t, ctx, db, scope, "gen-"+item.id, now.Add(-3*time.Hour))
		addOwnGateItem(t, ctx, db, item.id, scope, "gen-"+item.id, reducer.DomainWorkloadMaterialization, item.at)
	}
	clock := now
	queue := newOwnGateQueue(db, &clock, reducer.DomainWorkloadMaterialization)
	queue.Now = time.Now
	service := reducer.Service{
		PollInterval: 5 * time.Millisecond, WorkSource: queue, WorkSink: queue,
		Executor: wideThenNormalExecutor{
			wideID: "svc-wide",
			cause:  wideWaitCause{repos: []string{"repo-1"}, blocking: realShapeScopeIDs(t, 600)},
		},
	}
	runCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- service.Run(runCtx) }()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		if err := db.QueryRowContext(ctx,
			`SELECT status FROM fact_work_items WHERE work_item_id = 'svc-normal'`).Scan(&status); err != nil {
			t.Fatalf("read normal item: %v", err)
		}
		select {
		case err := <-done:
			t.Fatalf("Service.Run returned %v before the normal item was acknowledged (status %s): a wide verdict must not stop the reducer", err, status)
		default:
		}
		if status == "succeeded" {
			cancel()
			if err := <-done; err != nil {
				t.Fatalf("Service.Run after shutdown = %v, want nil", err)
			}
			if got := countNarrowWaits(t, ctx, db, "svc-wide"); got != 1 {
				t.Fatalf("wide item wait rows = %d, want 1", got)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the normal item was not acknowledged within 15s")
}

// TestOldShapeWideWaitRowStillWakesAndAuditsLive pins compatibility (#11256):
// rows parked before the key bound carry a wide blocking set and the read set
// in repo_ids. Under the new code the activation wake still reopens one by a
// named scope, and the audit still reopens one whose blockers are all active.
func TestOldShapeWideWaitRowStillWakesAndAuditsLive(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the #11256 key-bound proof")
	}
	ctx := context.Background()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)
	at := time.Now().UTC().Truncate(time.Microsecond)
	store := NewRelationshipStore(SQLDB{DB: db})
	ids := realShapeScopeIDs(t, 40)
	plant := func(itemID, scope string, blocking []string) {
		seedNarrowWaitScope(t, ctx, db, scope, "gen-"+scope, "active", at)
		seedNarrowRetryRow(t, ctx, db, itemID, scope, "gen-"+scope,
			"workload_materialization", reducer.WorkloadMaterializationResolutionNotReadyFailureClass, at)
		raw, err := json.Marshal(waitingKey{
			ScopeID: scope, GenerationID: "gen-" + scope, BlockingScopeIDs: blocking, RepoIDs: realShapeScopeIDs(t, 5),
		})
		if err != nil {
			t.Fatalf("marshal old-shape key: %v", err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO reducer_waiting_items (work_item_id, waits_for_kind, waits_for_key, scope_id, generation_id,
    domain, failure_class, first_deferred_at, updated_at)
VALUES ($1, 'relationship_generation_active', $2::jsonb, $3, $4, 'workload_materialization', $5, $6, $6)`,
			itemID, string(raw), scope, "gen-"+scope, reducer.WorkloadMaterializationResolutionNotReadyFailureClass, at); err != nil {
			t.Fatalf("plant old-shape wait %s: %v", itemID, err)
		}
	}
	// Wake: the item names ids[10]; activating it reopens the item.
	seedNarrowWaitScope(t, ctx, db, ids[10], "gen-"+ids[10], "pending", at)
	plant("old-wake", "git-repository-scope:repository:r_oldwake", ids)
	if err := store.ActivateResolutionGeneration(ctx, "gen-"+ids[10], ids[10]); err != nil {
		t.Fatalf("activate named blocker: %v", err)
	}
	if got := countNarrowWaits(t, ctx, db, "old-wake"); got != 0 {
		t.Fatalf("old-shape wait rows after a named activation = %d, want 0", got)
	}
	// Audit: the blockers are all complete (active generations), so the audit
	// reopens the row with no activation firing.
	for _, id := range ids[:3] {
		if id != ids[10] {
			seedNarrowWaitScope(t, ctx, db, id, "gen-"+id, "active", at)
		}
	}
	plant("old-audit", "git-repository-scope:repository:r_oldaudit", ids[:3])
	reopened, _, _, err := AuditWaitingItems(ctx, SQLDB{DB: db}, nil, time.Now().UTC(), DefaultWaitingAuditInterval, 100)
	if err != nil {
		t.Fatalf("AuditWaitingItems: %v", err)
	}
	if reopened < 1 {
		t.Fatalf("audit reopened = %d, want the old-shape satisfied wait reopened", reopened)
	}
	if got := countNarrowWaits(t, ctx, db, "old-audit"); got != 0 {
		t.Fatalf("old-shape wait rows after the audit = %d, want 0", got)
	}
}
