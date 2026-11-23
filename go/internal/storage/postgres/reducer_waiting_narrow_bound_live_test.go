// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// #11297 live proofs: a narrowed corpus wait for a large fence parks through
// the real ReducerQueue.Fail and wakes through the real activation path, on
// the production bootstrap schema, on a disposable database.

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11297 bounded narrowed corpus wait key: a 600-scope fence parks through ReducerQueue.Fail under the btree row limit, the worst-case incompressible key fits, an unrepresentable key parks nothing, a prefix activation wakes and an outside-prefix one does not, adversarial-order and audit convergence re-park on fresh prefixes, a repeated Fail keeps one row, and a cut key races an activation without a lost wake; disposable database per test, about 40 s locally
//eshu:readiness:env ESHU_REDUCER_FAIRNESS_PROOF_DSN ESHU_POSTGRES_DSN

const (
	boundWaiterScope = "scope-bw-waiter"
	boundWaiterGen   = "gen-bw-waiter"
)

// boundFleet seeds one active waiter scope and n holder scopes whose
// relationship generation is pending (they hold the fence). holders are
// returned in sorted order, so the key's sorted prefix is holders[:k].
func boundFleet(t *testing.T, ctx context.Context, db *sql.DB, at time.Time, ids []string) {
	t.Helper()
	seedNarrowWaitScope(t, ctx, db, boundWaiterScope, boundWaiterGen, "active", at)
	for _, id := range ids {
		seedNarrowWaitScope(t, ctx, db, id, "gen-"+id, "pending", at)
	}
}

func sequentialHolderIDs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("git-repository-scope:repository:r_%05d", i)
	}
	return out
}

// randomHolderIDs are incompressible: random hex with no shared prefix beyond
// a short tag.
func randomHolderIDs(t *testing.T, n int) []string {
	t.Helper()
	out := make([]string, n)
	for i := range out {
		buf := make([]byte, 20)
		if _, err := rand.Read(buf); err != nil {
			t.Fatalf("random holder id: %v", err)
		}
		out[i] = "h" + hex.EncodeToString(buf)
	}
	return out
}

// boundQueue is a real ReducerQueue with a movable clock.
type boundQueue struct {
	queue ReducerQueue
	clock *time.Time
	id    string
}

func newBoundQueue(t *testing.T, ctx context.Context, db *sql.DB, workItemID string, at time.Time) *boundQueue {
	t.Helper()
	clock := at
	addOwnGateItem(t, ctx, db, workItemID, boundWaiterScope, boundWaiterGen, reducer.DomainDeployableUnitCorrelation, at)
	return &boundQueue{queue: newOwnGateQueue(db, &clock, reducer.DomainDeployableUnitCorrelation), clock: &clock, id: workItemID}
}

// failWith claims the row (advancing the clock past any backstop) and Fails
// it with a narrowed deferral naming holders, through the real queue.
func (b *boundQueue) failWith(t *testing.T, ctx context.Context, holders []string) {
	t.Helper()
	*b.clock = b.clock.Add(WaitingBackstopDelay + time.Minute)
	intent, ok, err := b.queue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("claim %s: ok=%v err=%v", b.id, ok, err)
	}
	if err := b.queue.Fail(ctx, intent, narrowCapCause{repos: []string{"repo-bw"}, blocking: holders}); err != nil {
		t.Fatalf("Fail(%s) with %d holders: %v", b.id, len(holders), err)
	}
}

// waitKey reads the one parked wait's key for a queue row.
func waitKey(t *testing.T, ctx context.Context, db *sql.DB, workItemID string) (waitingKey, int, int) {
	t.Helper()
	var raw []byte
	var size int
	err := db.QueryRowContext(ctx,
		`SELECT waits_for_key, octet_length(waits_for_key::text) FROM reducer_waiting_items WHERE work_item_id = $1`,
		workItemID).Scan(&raw, &size)
	if err == sql.ErrNoRows {
		return waitingKey{}, 0, 0
	}
	if err != nil {
		t.Fatalf("read wait key %s: %v", workItemID, err)
	}
	var key waitingKey
	if err := json.Unmarshal(raw, &key); err != nil {
		t.Fatalf("decode wait key %s: %v", workItemID, err)
	}
	return key, size, countNarrowWaits(t, ctx, db, workItemID)
}

func activateHolder(t *testing.T, ctx context.Context, store *RelationshipStore, id string) {
	t.Helper()
	if err := store.ActivateResolutionGeneration(ctx, "gen-"+id, id); err != nil {
		t.Fatalf("activate %s: %v", id, err)
	}
}

// TestNarrowedWaitLargeFenceParksThroughFailLive: a 600-scope narrowed
// deferral parked through ReducerQueue.Fail leaves one wait row under the
// index-row cap, the queue row retrying on the 10-minute backstop, and a
// repeated Fail with the same set keeps one row. RED before the bound: the
// park INSERT fails with SQLSTATE 54000 and Fail returns the error.
func TestNarrowedWaitLargeFenceParksThroughFailLive(t *testing.T) {
	ctx := context.Background()
	db, _ := openNarrowFenceDB(t, ctx)
	at := time.Now().UTC().Truncate(time.Microsecond)
	holders := randomHolderIDs(t, 600)
	boundFleet(t, ctx, db, at, holders)
	q := newBoundQueue(t, ctx, db, "wait-bw-large", at)

	q.failWith(t, ctx, holders)
	key, size, waits := waitKey(t, ctx, db, "wait-bw-large")
	if waits != 1 {
		t.Fatalf("wait rows after Fail = %d, want 1", waits)
	}
	if len(key.BlockingScopeIDs) == 0 || len(key.BlockingScopeIDs) >= len(holders) {
		t.Fatalf("blocking prefix = %d ids for a %d-scope fence, want a non-empty cut", len(key.BlockingScopeIDs), len(holders))
	}
	if row := len("wait-bw-large") + len(WaitingKindRelationshipGenerationActive) + size; row > waitingIndexRowBudgetBytes {
		t.Fatalf("index row = %d bytes, want <= %d", row, waitingIndexRowBudgetBytes)
	}
	status, attempts, visibleAt := narrowQueueState(t, ctx, db, "wait-bw-large")
	if status != "retrying" || attempts != 1 {
		t.Fatalf("queue row = %s attempts %d, want retrying/1", status, attempts)
	}
	if horizon := visibleAt.Sub(*q.clock); horizon < 9*time.Minute || horizon > 11*time.Minute {
		t.Fatalf("visible_at horizon = %v, want the ~10m backstop (a parked, unsatisfied wait)", horizon)
	}

	// Idempotency: the same set failing again leaves one row.
	q.failWith(t, ctx, holders)
	if _, _, waits := waitKey(t, ctx, db, "wait-bw-large"); waits != 1 {
		t.Fatalf("wait rows after a repeated Fail = %d, want 1", waits)
	}
}

// TestNarrowedWaitWorstCaseKeyFitsLive: maximum-budget key built from random
// incompressible ids, with a realistic and then an extreme work_item_id. The
// realistic row inserts; the extreme one is unrepresentable and parks nothing
// while the queue row stays retrying on its backstop.
func TestNarrowedWaitWorstCaseKeyFitsLive(t *testing.T) {
	ctx := context.Background()
	db, _ := openNarrowFenceDB(t, ctx)
	at := time.Now().UTC().Truncate(time.Microsecond)
	holders := randomHolderIDs(t, 300)
	boundFleet(t, ctx, db, at, holders)

	realistic := "reducer_git-repository-scope_repository_r_5d621369_" + strings.Repeat("ab", 32) +
		"_workload_materialization_repo_r_5d621369"
	q := newBoundQueue(t, ctx, db, realistic, at)
	q.failWith(t, ctx, holders)
	if _, _, waits := waitKey(t, ctx, db, realistic); waits != 1 {
		t.Fatalf("realistic work_item_id (%d bytes): wait rows = %d, want 1", len(realistic), waits)
	}
	t.Logf("measured longest realistic work_item_id %d bytes", len(realistic))

	extreme := strings.Repeat("w", 3000)
	q = newBoundQueue(t, ctx, db, extreme, at)
	q.failWith(t, ctx, holders)
	if _, _, waits := waitKey(t, ctx, db, extreme); waits != 0 {
		t.Fatalf("extreme work_item_id: wait rows = %d, want 0 (unrepresentable parks nothing)", waits)
	}
	status, attempts, visibleAt := narrowQueueState(t, ctx, db, extreme)
	if status != "retrying" || attempts != 1 {
		t.Fatalf("extreme row = %s attempts %d, want retrying/1", status, attempts)
	}
	if horizon := visibleAt.Sub(*q.clock); horizon < 9*time.Minute || horizon > 11*time.Minute {
		t.Fatalf("extreme row visible_at horizon = %v, want the ~10m backstop, not an immediate coarse reopen", horizon)
	}
}

// TestNarrowedWaitPrefixWakeSelectivityLive: activating a scope in the key's
// prefix reopens the waiter; activating one outside it leaves the row parked.
func TestNarrowedWaitPrefixWakeSelectivityLive(t *testing.T) {
	ctx := context.Background()
	db, _ := openNarrowFenceDB(t, ctx)
	at := time.Now().UTC().Truncate(time.Microsecond)
	holders := sequentialHolderIDs(200)
	boundFleet(t, ctx, db, at, holders)
	store := NewRelationshipStore(SQLDB{DB: db})
	q := newBoundQueue(t, ctx, db, "wait-bw-select", at)
	q.failWith(t, ctx, holders)

	key, _, waits := waitKey(t, ctx, db, "wait-bw-select")
	if waits != 1 || len(key.BlockingScopeIDs) == 0 || len(key.BlockingScopeIDs) >= len(holders) {
		t.Fatalf("park = %d rows, %d prefix ids, want 1 row and a cut prefix", waits, len(key.BlockingScopeIDs))
	}
	outside := holders[len(holders)-1]
	if slices.Contains(key.BlockingScopeIDs, outside) {
		t.Fatalf("last holder %s unexpectedly inside the prefix", outside)
	}
	activateHolder(t, ctx, store, outside)
	if _, _, waits := waitKey(t, ctx, db, "wait-bw-select"); waits != 1 {
		t.Fatalf("wait rows after an outside-prefix activation = %d, want 1: it cannot proceed while the prefix holds", waits)
	}
	activateHolder(t, ctx, store, key.BlockingScopeIDs[0])
	if _, _, waits := waitKey(t, ctx, db, "wait-bw-select"); waits != 0 {
		t.Fatalf("wait rows after a prefix activation = %d, want 0 (reopened)", waits)
	}
}

// remainingHolders returns the holders whose relationship generation is still
// pending: what the retried handler's fence reads.
func remainingHolders(t *testing.T, ctx context.Context, db *sql.DB, all []string) []string {
	t.Helper()
	var out []string
	for _, id := range all {
		var status string
		if err := db.QueryRowContext(ctx,
			`SELECT status FROM relationship_generations WHERE scope = $1 AND generation_id = $2`, id, "gen-"+id).Scan(&status); err != nil {
			t.Fatalf("read holder %s: %v", id, err)
		}
		if status != "active" {
			out = append(out, id)
		}
	}
	return out
}

// TestNarrowedWaitAdversarialOrderConvergesLive: clear the non-prefix scopes
// first, then the prefix. The waiter ends with the fence complete, every
// re-park is unsatisfied at park time (a backstop horizon, not an immediate
// reopen), and re-parks stay at or below the member count.
func TestNarrowedWaitAdversarialOrderConvergesLive(t *testing.T) {
	ctx := context.Background()
	db, _ := openNarrowFenceDB(t, ctx)
	at := time.Now().UTC().Truncate(time.Microsecond)
	holders := sequentialHolderIDs(90)
	boundFleet(t, ctx, db, at, holders)
	store := NewRelationshipStore(SQLDB{DB: db})
	q := newBoundQueue(t, ctx, db, "wait-bw-adversarial", at)

	current := holders
	reparks := 0
	for len(current) > 0 {
		q.failWith(t, ctx, current)
		reparks++
		if reparks > len(holders) {
			t.Fatalf("re-parks = %d, want <= %d members", reparks, len(holders))
		}
		key, _, waits := waitKey(t, ctx, db, "wait-bw-adversarial")
		if waits != 1 {
			t.Fatalf("park %d left %d wait rows, want 1 (a satisfied-at-park wait would have reopened at once)", reparks, waits)
		}
		_, _, visibleAt := narrowQueueState(t, ctx, db, "wait-bw-adversarial")
		if horizon := visibleAt.Sub(*q.clock); horizon < 9*time.Minute {
			t.Fatalf("park %d visible_at horizon = %v, want the backstop: the park-time check must be unsatisfied", reparks, horizon)
		}
		// Adversarial order: every scope outside the prefix clears first.
		for _, id := range current {
			if !slices.Contains(key.BlockingScopeIDs, id) {
				activateHolder(t, ctx, store, id)
			}
		}
		if _, _, waits := waitKey(t, ctx, db, "wait-bw-adversarial"); waits != 1 {
			t.Fatalf("park %d: outside-prefix activations left %d wait rows, want 1", reparks, waits)
		}
		// Then the prefix: its first activation reopens the waiter.
		activateHolder(t, ctx, store, key.BlockingScopeIDs[0])
		if _, _, waits := waitKey(t, ctx, db, "wait-bw-adversarial"); waits != 0 {
			t.Fatalf("park %d: prefix activation left %d wait rows, want 0 (reopened)", reparks, waits)
		}
		current = remainingHolders(t, ctx, db, holders)
	}
	if got := len(remainingHolders(t, ctx, db, holders)); got != 0 {
		t.Fatalf("fence still holds %d scopes after convergence", got)
	}
	t.Logf("converged in %d re-parks for %d members", reparks, len(holders))
}

// TestNarrowedWaitAuditReopensClearedPrefixLive: prefix scopes stop holding
// without an activation, so no wake fires. The audit reopens the wait and the
// next park carries the next prefix.
func TestNarrowedWaitAuditReopensClearedPrefixLive(t *testing.T) {
	ctx := context.Background()
	db, _ := openNarrowFenceDB(t, ctx)
	at := time.Now().UTC().Truncate(time.Microsecond)
	holders := sequentialHolderIDs(120)
	boundFleet(t, ctx, db, at, holders)
	q := newBoundQueue(t, ctx, db, "wait-bw-audit", at)
	q.failWith(t, ctx, holders)
	first, _, waits := waitKey(t, ctx, db, "wait-bw-audit")
	if waits != 1 {
		t.Fatalf("wait rows = %d, want 1", waits)
	}
	for _, id := range first.BlockingScopeIDs {
		if _, err := db.ExecContext(ctx,
			`UPDATE relationship_generations SET status = 'active' WHERE scope = $1`, id); err != nil {
			t.Fatalf("clear %s without an activation: %v", id, err)
		}
	}
	reopened, _, _, err := AuditWaitingItems(ctx, SQLDB{DB: db}, nil,
		time.Now().UTC().Add(DefaultWaitingAuditInterval+time.Minute), DefaultWaitingAuditInterval, 100)
	if err != nil || reopened != 1 {
		t.Fatalf("AuditWaitingItems() = reopened %d, err %v, want 1, nil", reopened, err)
	}
	if _, _, waits := waitKey(t, ctx, db, "wait-bw-audit"); waits != 0 {
		t.Fatalf("wait rows after the audit = %d, want 0", waits)
	}
	remaining := remainingHolders(t, ctx, db, holders)
	q.failWith(t, ctx, remaining)
	next, _, waits := waitKey(t, ctx, db, "wait-bw-audit")
	if waits != 1 || len(next.BlockingScopeIDs) == 0 {
		t.Fatalf("re-park = %d rows, %d ids, want 1 row with a prefix", waits, len(next.BlockingScopeIDs))
	}
	if next.BlockingScopeIDs[0] != remaining[0] || slices.Contains(next.BlockingScopeIDs, first.BlockingScopeIDs[0]) {
		t.Fatalf("next prefix starts at %s, want the next holder %s", next.BlockingScopeIDs[0], remaining[0])
	}
}

// TestNarrowedWaitCutKeyRacesActivationLive: a cut-key park races the
// activation of every scope in its prefix. Once all of them committed, the
// wait must be gone and the queue row retrying on its original attempt: the
// activation wake or the park-time check, never neither.
func TestNarrowedWaitCutKeyRacesActivationLive(t *testing.T) {
	ctx := context.Background()
	db, _ := openNarrowFenceDB(t, ctx)
	at := time.Now().UTC().Truncate(time.Microsecond)
	holders := sequentialHolderIDs(80)
	boundFleet(t, ctx, db, at, holders)
	prefix, _, _ := boundWaitingScopeIDs(holders, narrowedWaitingBlockingBudgetBytes)
	if len(prefix) == 0 || len(prefix) >= len(holders) {
		t.Fatalf("prefix = %d of %d, want a cut", len(prefix), len(holders))
	}
	seedNarrowRetryRow(t, ctx, db, "wait-bw-race", boundWaiterScope, boundWaiterGen,
		"workload_materialization", "workload_materialization_resolution_not_ready", at)
	intent := narrowWaitIntent("wait-bw-race", boundWaiterScope, boundWaiterGen)

	var wg sync.WaitGroup
	errs := make(chan error, len(prefix)+1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := setAsideWaitingForCause(ctx, SQLDB{DB: db}, nil, intent, narrowWaitEntry(),
			narrowCapCause{repos: []string{"repo-bw"}, blocking: holders}, nil, time.Now().UTC()); err != nil {
			errs <- fmt.Errorf("park: %w", err)
		}
	}()
	for _, id := range prefix {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := NewRelationshipStore(SQLDB{DB: db}).ActivateResolutionGeneration(ctx, "gen-"+id, id); err != nil {
				errs <- fmt.Errorf("activate %s: %w", id, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("race error: %v", err)
	}
	if got := countNarrowWaits(t, ctx, db, "wait-bw-race"); got != 0 {
		t.Fatalf("wait rows after every prefix scope activated = %d, want 0: a lost wake", got)
	}
	status, attempts, _ := narrowQueueState(t, ctx, db, "wait-bw-race")
	if status != "retrying" || attempts != 1 {
		t.Fatalf("queue row = %s attempts %d, want retrying/1", status, attempts)
	}
}
