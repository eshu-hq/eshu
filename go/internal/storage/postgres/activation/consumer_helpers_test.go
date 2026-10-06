// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
)

// claimActivationObligation claims obligations until the wanted identity is
// returned. Other eligible obligations (the source's) may come first.
func claimActivationObligation(t *testing.T, ctx context.Context, store activation.Store,
	owner string, lease time.Duration, scopeID, generationID string,
) *activation.Obligation {
	t.Helper()
	claimed := make([]string, 0, 8)
	for n := 0; n < 8; n++ {
		work, err := store.Claim(ctx, owner, lease)
		if err != nil {
			t.Fatalf("obligation Claim: %v; prior=%v", err, claimed)
		}
		if work == nil {
			break
		}
		claimed = append(claimed, work.ScopeID+"/"+work.GenerationID)
		if work.ScopeID == scopeID && work.GenerationID == generationID {
			return work
		}
	}
	t.Fatalf("obligation %s/%s not claimed; claims=%v", scopeID, generationID, claimed)
	return nil
}

// activationDatabaseClock makes a test reducer queue read the database clock.
// The wake writes visible_at = clock_timestamp() while ReducerQueue.Claim
// admits visible_at <= its own Now; a disposable container whose clock runs
// tens of milliseconds ahead of the host otherwise leaves a just-woken row
// invisible to an immediate Claim (13 of 25 runs failed that way). In
// production the same skew only delays a woken row by the skew.
func activationDatabaseClock(t *testing.T, ctx context.Context, database *sql.DB) func() time.Time {
	t.Helper()
	return func() time.Time {
		var now time.Time
		if err := database.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
			t.Errorf("read database clock: %v", err)
			return time.Now()
		}
		return now.UTC()
	}
}

// finalizeActivation reports whether Finalize completed the obligation.
func finalizeActivation(ctx context.Context, store activation.Store, work *activation.Obligation) (bool, error) {
	result, err := store.Finalize(ctx, *work)
	return result.Outcome == activation.OutcomeCompleted, err
}

// activationMatrix is setupActivationConsumer plus the source Ack (and
// optionally the target Ack); the old target generation is already
// superseded once the target Ack runs.
type activationMatrix struct {
	ctx      context.Context
	db       *sql.DB
	store    postgres.IngestionStore
	oblig    activation.Store
	source   string
	scope    string
	gen      string
	oldGen   string
	reducerQ postgres.ReducerQueue
	target   projector.ScopeGenerationWork
}

func newActivationMatrix(t *testing.T, prefix string, ackTarget bool) activationMatrix {
	t.Helper()
	ctx, database, store, sourceWork, targetWork := setupActivationConsumer(t, prefix)
	pq := postgres.NewProjectorQueue(postgres.SQLDB{DB: database}, "7584-consumer-projector", time.Minute)
	if err := pq.Ack(ctx, sourceWork, projectorruntime.Result{}); err != nil {
		t.Fatal(err)
	}
	if ackTarget {
		if err := pq.Ack(ctx, targetWork, projectorruntime.Result{}); err != nil {
			t.Fatal(err)
		}
	}
	rq := postgres.NewReducerQueue(postgres.SQLDB{DB: database}, "7584-matrix-reducer", time.Minute)
	rq.Now = activationDatabaseClock(t, ctx, database)
	rq.ClaimDomains = []reducer.Domain{reducer.DomainDeploymentMapping}
	rq.RetryDelay = time.Minute
	return activationMatrix{
		ctx: ctx, db: database, store: store, oblig: activation.NewStore(postgres.SQLDB{DB: database}),
		source: sourceWork.Scope.ScopeID, scope: targetWork.Scope.ScopeID,
		gen: targetWork.Generation.GenerationID, oldGen: "gen-consumer-target-old",
		reducerQ: rq, target: targetWork,
	}
}

// enqueueClaim enqueues one row of domain and claims exactly that row with a
// queue restricted to the domain.
func (f activationMatrix) enqueueClaim(t *testing.T, domain reducer.Domain, scope, gen, entity string) reducer.Intent {
	t.Helper()
	queue := f.reducerQ
	queue.ClaimDomains = []reducer.Domain{domain}
	if _, err := queue.Enqueue(f.ctx, []projectorruntime.ReducerIntent{{
		ScopeID: scope, GenerationID: gen, Domain: domain,
		EntityKey: entity, SourceSystem: "git",
	}}); err != nil {
		t.Fatal(err)
	}
	var seen []string
	for n := 0; n < 4; n++ {
		intent, ok, err := queue.Claim(f.ctx)
		if err != nil || !ok {
			break
		}
		var got string
		if err := f.db.QueryRowContext(f.ctx,
			"SELECT COALESCE(payload->>'entity_key','') FROM fact_work_items WHERE work_item_id=$1",
			intent.IntentID).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got == entity && intent.ScopeID == scope && intent.GenerationID == gen {
			return intent
		}
		seen = append(seen, got)
	}
	t.Fatalf("row %s/%s/%s/%s not claimed; other claims=%v", domain, scope, gen, entity, seen)
	return reducer.Intent{}
}

// notReady leaves one deployment_mapping row retrying with the
// backward-evidence-not-ready class through the real reducer Fail path.
func (f activationMatrix) notReady(t *testing.T, scope, gen, entity string) string {
	t.Helper()
	return f.notReadyDomain(t, reducer.DomainDeploymentMapping, scope, gen, entity)
}

func (f activationMatrix) notReadyDomain(t *testing.T, domain reducer.Domain, scope, gen, entity string) string {
	t.Helper()
	intent := f.enqueueClaim(t, domain, scope, gen, entity)
	if err := f.reducerQ.Fail(f.ctx, intent,
		crossrepo.BackwardEvidenceNotReadyError{ScopeID: scope, GenerationID: gen}); err != nil {
		t.Fatal(err)
	}
	return intent.IntentID
}

func (f activationMatrix) claimObligation(t *testing.T, owner string, lease time.Duration, gen string) *activation.Obligation {
	t.Helper()
	return claimActivationObligation(t, f.ctx, f.oblig, owner, lease, f.scope, gen)
}

func (f activationMatrix) finalize(work *activation.Obligation) (bool, error) {
	return finalizeActivation(f.ctx, f.oblig, work)
}

// maintenance is the labelled control arm: the whole native deferred
// relationship maintenance. It is a test fixture only, never the shipped
// consumer callback (#7584).
func (f activationMatrix) maintenance(t *testing.T) {
	t.Helper()
	if err := f.store.RunDeferredRelationshipMaintenance(f.ctx, nil, nil); err != nil {
		t.Fatal(err)
	}
}

// row returns a queue row, optionally without the three wake-owned columns.
func (f activationMatrix) row(t *testing.T, id string, ignoreWake bool) map[string]any {
	t.Helper()
	raw, err := activationQueueRow(f.ctx, f.db, id)
	if err != nil {
		t.Fatal(err)
	}
	row := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		t.Fatal(err)
	}
	if ignoreWake {
		for _, key := range []string{"visible_at", "next_attempt_at", "updated_at"} {
			delete(row, key)
		}
	}
	return row
}

func (f activationMatrix) mustEqual(t *testing.T, what string, id string, want map[string]any) {
	t.Helper()
	if got := f.row(t, id, false); !reflect.DeepEqual(got, want) {
		t.Fatalf("%s changed:\n got=%v\nwant=%v", what, got, want)
	}
}

func (f activationMatrix) obligationRow(t *testing.T, gen string) string {
	t.Helper()
	var row string
	if err := f.db.QueryRowContext(f.ctx,
		"SELECT row_to_json(o)::text FROM activation_obligations o WHERE scope_id=$1 AND generation_id=$2",
		f.scope, gen).Scan(&row); err != nil {
		t.Fatal(err)
	}
	return row
}

func (f activationMatrix) obligationState(t *testing.T, gen string) string {
	t.Helper()
	var state string
	if err := f.db.QueryRowContext(f.ctx,
		"SELECT state FROM activation_obligations WHERE scope_id=$1 AND generation_id=$2",
		f.scope, gen).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

// digest hashes every obligation, work item and phase row, so any durable
// write by a refused Finalize shows up.
func (f activationMatrix) digest(t *testing.T) string {
	t.Helper()
	var sum string
	if err := f.db.QueryRowContext(f.ctx, `SELECT md5(
 (SELECT COALESCE(string_agg(o::text,'|' ORDER BY o::text),'') FROM activation_obligations o) ||
 (SELECT COALESCE(string_agg(w::text,'|' ORDER BY w::text),'') FROM fact_work_items w) ||
 (SELECT COALESCE(string_agg(p::text,'|' ORDER BY p::text),'') FROM graph_projection_phase_state p))`).Scan(&sum); err != nil {
		t.Fatal(err)
	}
	return sum
}

// mustClaimable requires the woken row to be the first natively claimable
// deployment_mapping row.
func (f activationMatrix) mustClaimable(t *testing.T, id string) {
	t.Helper()
	intent, ok, err := f.reducerQ.Claim(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("woken row %s not natively claimable: nothing claimable", id)
	}
	if intent.IntentID != id {
		t.Fatalf("native Claim returned %s (%s/%s), want woken row %s",
			intent.IntentID, intent.ScopeID, intent.GenerationID, id)
	}
}

// awaitLeaseExpiry waits on the database clock; fixtures never rewrite lease
// timestamps.
func (f activationMatrix) awaitLeaseExpiry(t *testing.T, gen string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var expired bool
		if err := f.db.QueryRowContext(f.ctx,
			"SELECT lease_until<=clock_timestamp() FROM activation_obligations WHERE scope_id=$1 AND generation_id=$2",
			f.scope, gen).Scan(&expired); err != nil {
			t.Fatal(err)
		}
		if expired {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("lease did not expire on the database clock")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// activationRetryable is retryable with no failure class, so the native Fail
// path leaves it retrying with the generic retryable class.
type activationRetryable struct{}

func (activationRetryable) Error() string   { return "unrelated retryable failure" }
func (activationRetryable) Retryable() bool { return true }

// requireOtherClassRetrying pins a fixture row to retrying, future-visible,
// and a class other than the wake class.
func (f activationMatrix) requireOtherClassRetrying(t *testing.T, id string) {
	t.Helper()
	var status, class string
	var future bool
	if err := f.db.QueryRowContext(f.ctx,
		"SELECT status,COALESCE(failure_class,''),visible_at>clock_timestamp() FROM fact_work_items WHERE work_item_id=$1",
		id).Scan(&status, &class, &future); err != nil {
		t.Fatal(err)
	}
	if status != "retrying" || !future || class == crossrepo.CrossRepoBackwardEvidenceNotReadyFailureClass {
		t.Fatalf("other-class fixture status=%q class=%q futureVisible=%v", status, class, future)
	}
}
