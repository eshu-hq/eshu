// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11256 bounded narrowed wait key: a wide verdict set parks through the real ReducerQueue.Fail on the migrated schema instead of failing the btree key insert (SQLSTATE 54000) after the retry UPDATE committed
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
