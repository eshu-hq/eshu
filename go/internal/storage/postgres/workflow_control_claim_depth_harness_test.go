// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Claim-issuance-under-depth harness (#8468, epic #8467).
//
// The harness drives the shipped WorkflowControlStore.ClaimNextEligible
// statement (claimNextWorkflowWorkItemQuery: one candidate row
// FOR UPDATE SKIP LOCKED, then the item UPDATE and claim INSERT in the same
// statement) with N concurrent claimers against a seeded backlog on real
// Postgres. It exists so the fairness (#8469), depth-pacing (#8470), and
// failure-proof (#8473) leaves share one regression floor instead of each
// rebuilding a drain loop.
//
// Lock-order and contention contract the harness relies on, and which its
// callers prove:
//   - Claim vs claim: two claimers contend only on one workflow_work_items
//     row. SKIP LOCKED hands a claimer that meets a locked head row the next
//     row instead of blocking; a row a rival claimed and committed between
//     snapshot and lock is dropped by the Read Committed EvalPlanQual
//     recheck of the predicate-bearing locking SELECT (status is no longer
//     'pending'), so no work item is granted twice.
//   - Claim vs complete: the claim path locks no existing workflow_claims
//     row (it inserts a new one), while CompleteClaim locks item then claim
//     (FOR UPDATE OF item, claim). There is no second lock order to invert,
//     so claim and complete cannot deadlock against each other.
//   - Lease safety: every completion is fenced by (claim_id, fencing_token,
//     owner_id); the drain completes each grant exactly once under its own
//     fence and a rejection fails the test.
//
// Run with a throwaway Postgres:
//
//	ESHU_POSTGRES_DSN='postgres://postgres:postgres@localhost:<port>/eshu?sslmode=disable' \
//	  go test ./internal/storage/postgres -run 'ClaimDepth' -count=1 -v

// claimDepthClaimers is the concurrent claimer count section 6 of the #4588
// claim-pacing design requires for every fairness measurement.
const claimDepthClaimers = 8

// claimDepthStartStagger is the per-claimer start delay applied after the
// drain barrier. Tests set it for the production-like desynchronized case
// and restore zero (the barrier-synchronized worst case) when done.
var claimDepthStartStagger time.Duration

// claimDepthKeyBacklog seeds Count work items under one fairness key. Items
// are created at the harness base time plus CreatedOffset, so FIFO claim
// order across keys is set by the offsets, never by insert timing. Priority
// defaults to zero; #8469 sets it to seed mixed priority tiers.
type claimDepthKeyBacklog struct {
	FairnessKey   string
	Count         int
	CreatedOffset time.Duration
	Priority      int
}

// claimDepthRecord is one successful grant observed by the drain.
type claimDepthRecord struct {
	WorkItemID  string
	FairnessKey string
	ClaimID     string
	OwnerID     string
	BackendPID  int
	// Seq is the global grant-return order (1-based). It is assigned when
	// ClaimNextEligible returns, so under concurrency it can trail the true
	// grant order by at most the number of grants in flight; assertions
	// must leave that much slack.
	Seq int64
	// Elapsed is the wall time from drain start to this grant returning.
	Elapsed time.Duration
}

// claimDepthResult is the full measured outcome of one drain.
type claimDepthResult struct {
	Records []claimDepthRecord
	// BackendPIDs holds the pg_backend_pid of each claimer's pinned
	// connection, in claimer order.
	BackendPIDs []int
	// PoolLimit is the raised MaxOpenConns the drain ran under.
	PoolLimit int
	// StatsBefore is the pool snapshot taken just before the drain started.
	StatsBefore sql.DBStats
	// StatsPinned is the pool snapshot taken after every claimer finished
	// claiming but before any claimer released its pinned connection, so
	// InUse counts exactly the connections the claim loops held.
	StatsPinned sql.DBStats
	// EmptyAttempts counts ClaimNextEligible calls that returned no work,
	// including transient all-heads-locked races a claimer retries through
	// before it idles; #8470 uses it for the work-conserving counter.
	EmptyAttempts int64
	Duration      time.Duration
}

// ClaimsPerSecond reports aggregate drain throughput.
func (r claimDepthResult) ClaimsPerSecond() float64 {
	if r.Duration <= 0 {
		return 0
	}
	return float64(len(r.Records)) / r.Duration.Seconds()
}

// maxClaimDepthConsecutiveEmpties bounds one claimer's consecutive empty
// ClaimNextEligible returns before it may idle. #8725 raised the original 50
// to 200: a 50-empty budget idled a claimer mid-drain under a lock-hot window
// (all-heads-locked empties while peers still granted), stranding it at 9
// grants against the 10-grant floor. The larger budget covers all-stalled
// windows; the quiescence proof in claimDepthQuiescent is the primary guard
// for windows where peers keep granting.
const maxClaimDepthConsecutiveEmpties = 200

// claimDepthStreakAction is the idle-exit step decision for a claimer that
// has just observed a consecutive run of empty ClaimNextEligible returns.
type claimDepthStreakAction int

const (
	// claimDepthStreakPoll keeps polling: the streak is below budget.
	claimDepthStreakPoll claimDepthStreakAction = iota
	// claimDepthStreakReset restarts the streak: the budget was reached but a
	// global grant landed during it, so the streak is head-of-line
	// contention, not a drained backlog. Each reset consumes at least one
	// finite global grant, so the loop still ends once peers stop granting.
	claimDepthStreakReset
	// claimDepthStreakIdle idles the claimer: quiescence is proven.
	claimDepthStreakIdle
)

// claimDepthStreakStep decides idle, reset, or poll for a streak of
// consecutiveEmpties consecutive empty returns, where progressed reports
// whether a global grant landed during the streak. Idling requires
// quiescence proof: the streak reached the budget AND no grant landed
// globally during it. A streak that overlaps any grant is head-of-line
// contention, not a drained backlog, so the claimer must keep polling (#8725).
func claimDepthStreakStep(consecutiveEmpties int, progressed bool) claimDepthStreakAction {
	if consecutiveEmpties < maxClaimDepthConsecutiveEmpties {
		return claimDepthStreakPoll
	}
	if progressed {
		return claimDepthStreakReset
	}
	return claimDepthStreakIdle
}

// claimDepthQuiescent reports whether a claimer that has just observed
// consecutiveEmpties consecutive empty ClaimNextEligible returns may idle.
// It is the quiescence leg of claimDepthStreakStep, kept for the predicate
// truth-table test: idle requires the budget AND a quiet streak.
func claimDepthQuiescent(consecutiveEmpties int, seqAtStreakStart, seqNow int64) bool {
	if consecutiveEmpties < maxClaimDepthConsecutiveEmpties {
		return false
	}
	return seqNow == seqAtStreakStart
}

// openClaimDepthHarness opens the isolated-schema workflow store and raises
// its pool so concurrent claim loops each hold their own connection.
func openClaimDepthHarness(t *testing.T, claimers int) (*sql.DB, *WorkflowControlStore, context.Context, int) {
	t.Helper()
	database, store := openWorkflowControlIntegrationStore(t)
	poolLimit := postgresproof.RaisePoolForConcurrentClaimers(database, claimers)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	return database, store, ctx, poolLimit
}

// seedClaimDepthBacklog creates one run and enqueues the per-key backlogs
// under a single (collector_kind, collector_instance_id), which is the layer
// FairClaimDispatcher hands off to and does not itself pace. It returns the
// seeded work item ids in seed order.
func seedClaimDepthBacklog(
	t *testing.T,
	store *WorkflowControlStore,
	ctx context.Context,
	runID string,
	base time.Time,
	keys []claimDepthKeyBacklog,
) []string {
	t.Helper()
	mustCreateRun(t, store, ctx, workflow.Run{
		RunID:       runID,
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusCollectionPending,
		CreatedAt:   base,
		UpdatedAt:   base,
	})
	total := 0
	for _, key := range keys {
		total += key.Count
	}
	items := make([]workflow.WorkItem, 0, total)
	seeded := make([]string, 0, total)
	for _, key := range keys {
		for i := 0; i < key.Count; i++ {
			id := fmt.Sprintf("%s:%s:%04d", runID, key.FairnessKey, i)
			created := base.Add(key.CreatedOffset).Add(time.Duration(i) * time.Millisecond)
			items = append(items, workflow.WorkItem{
				WorkItemID:          id,
				RunID:               runID,
				CollectorKind:       scope.CollectorGit,
				CollectorInstanceID: "claim-depth-instance",
				SourceSystem:        string(scope.CollectorGit),
				ScopeID:             "scope-" + id,
				AcceptanceUnitID:    "scope-" + id,
				SourceRunID:         id + "-generation",
				GenerationID:        id + "-generation",
				FairnessKey:         key.FairnessKey,
				ClaimPriority:       key.Priority,
				Status:              workflow.WorkItemStatusPending,
				CreatedAt:           created,
				UpdatedAt:           created,
			})
			seeded = append(seeded, id)
		}
	}
	if err := store.EnqueueWorkItems(ctx, items); err != nil {
		t.Fatalf("EnqueueWorkItems(%d items) error = %v, want nil", len(items), err)
	}
	return seeded
}

// claimDepthPacingHook, when non-nil, paces one drain claim attempt: it
// observes the attempt's store, selector, and timestamp and returns the
// grant delay the claimer must wait before claiming, or zero to claim
// immediately. Nil (the default) claims with no pacing consult, which is the
// shape every pre-pacing drain asserts on. #8470 sets it to consult
// CheckClaimPacing the way the production runner does.
var claimDepthPacingHook func(store *WorkflowControlStore, selector workflow.ClaimSelector, now time.Time) time.Duration

// claimDepthConn adapts one pinned *sql.Conn to the store's ExecQueryer
// surface, mirroring SQLDB for a single connection.
type claimDepthConn struct {
	conn *sql.Conn
}

func (c claimDepthConn) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return c.conn.QueryContext(ctx, query, args...)
}

func (c claimDepthConn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.conn.ExecContext(ctx, query, args...)
}

// drainClaimDepthBacklog runs concurrent claim loops from one start barrier.
// Each claimer pins its own pool connection for the whole drain, claims
// through its own WorkflowControlStore over that connection, records the
// grant, and completes it under its fence. A claimer idles only on
// quiescence proof (claimDepthQuiescent): a full budget of consecutive
// empties with no global grant landing during the streak. Pending work only
// shrinks during a drain (nothing requeues), and each streak reset requires
// at least one global grant, so the loop still ends once peers stop
// granting.
func drainClaimDepthBacklog(
	t *testing.T,
	database *sql.DB,
	ctx context.Context,
	claimers int,
	poolLimit int,
) claimDepthResult {
	t.Helper()
	result := claimDepthResult{PoolLimit: poolLimit, StatsBefore: database.Stats()}
	ready := make(chan struct{}, claimers)
	start := make(chan struct{})
	release := make(chan struct{})
	errs := make(chan error, claimers)
	perClaimer := make([][]claimDepthRecord, claimers)
	backendPIDs := make([]int, claimers)
	var seq atomic.Int64
	var emptyAttempts atomic.Int64
	// claimDepthStartStagger desynchronizes claimer starts for the
	// production-like timing case: each claimer waits claimer*stagger after
	// the start barrier. Zero (the default) keeps the barrier-synchronized
	// worst case every existing drain asserts on.
	stagger := claimDepthStartStagger
	var claiming sync.WaitGroup
	var released sync.WaitGroup
	startedAt := time.Now()
	for c := 0; c < claimers; c++ {
		claiming.Add(1)
		released.Add(1)
		go func(claimer int) {
			defer released.Done()
			conn, err := database.Conn(ctx)
			if err != nil {
				errs <- fmt.Errorf("claimer %d pin connection: %w", claimer, err)
				ready <- struct{}{}
				claiming.Done()
				return
			}
			defer func() { _ = conn.Close() }()
			var pid int
			if err := conn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				errs <- fmt.Errorf("claimer %d read backend pid: %w", claimer, err)
				ready <- struct{}{}
				claiming.Done()
				<-release
				return
			}
			backendPIDs[claimer] = pid
			store := NewWorkflowControlStore(claimDepthConn{conn: conn})
			owner := fmt.Sprintf("claim-depth-owner-%d", claimer)
			ready <- struct{}{}
			<-start
			if stagger > 0 {
				time.Sleep(time.Duration(claimer) * stagger)
			}
			consecutiveEmpties := 0
			var seqAtStreakStart int64
		drainLoop:
			for attempt := 0; ; attempt++ {
				selector := workflow.ClaimSelector{
					CollectorKind:       scope.CollectorGit,
					CollectorInstanceID: "claim-depth-instance",
					OwnerID:             owner,
					ClaimID:             fmt.Sprintf("claim-depth-%d-%d", claimer, attempt),
				}
				if claimDepthPacingHook != nil {
					if delay := claimDepthPacingHook(store, selector, time.Now().UTC()); delay > 0 {
						time.Sleep(delay)
					}
				}
				item, claim, found, err := store.ClaimNextEligible(ctx, selector, time.Now().UTC(), 5*time.Minute)
				if err != nil {
					errs <- fmt.Errorf("claimer %d ClaimNextEligible attempt %d: %w", claimer, attempt, err)
					break
				}
				if !found {
					// A transient all-heads-locked race returns empty
					// while eligible work exists. Production collectors
					// poll again rather than exit, so the harness
					// retries a bounded run of consecutive empties
					// before it idles the claimer. Idling additionally
					// requires quiescence proof (claimDepthQuiescent):
					// a streak that overlaps any global grant is
					// contention, not a drained backlog (#8725). The
					// retry sleeps a jittered 0-5ms: a fixed sleep
					// re-fires every collided claimer on the same
					// millisecond and turns one collision into a
					// synchronized retry storm. #8470 owns the
					// no-pacing empty-attempt budget.
					emptyAttempts.Add(1)
					if consecutiveEmpties == 0 {
						seqAtStreakStart = seq.Load()
					}
					consecutiveEmpties++
					switch claimDepthStreakStep(consecutiveEmpties, seq.Load() != seqAtStreakStart) {
					case claimDepthStreakIdle:
						break drainLoop
					case claimDepthStreakReset:
						// Budget reached but grants landed during the
						// streak: contention, not quiescence. Start a
						// fresh streak from the new baseline.
						consecutiveEmpties = 0
					case claimDepthStreakPoll:
						// Below budget: keep polling.
					}
					time.Sleep(time.Duration(rand.Int63n(5)) * time.Millisecond)
					continue
				}
				consecutiveEmpties = 0
				perClaimer[claimer] = append(perClaimer[claimer], claimDepthRecord{
					WorkItemID:  item.WorkItemID,
					FairnessKey: item.FairnessKey,
					ClaimID:     claim.ClaimID,
					OwnerID:     claim.OwnerID,
					BackendPID:  pid,
					Seq:         seq.Add(1),
					Elapsed:     time.Since(startedAt),
				})
				if err := store.CompleteClaim(ctx, workflow.ClaimMutation{
					WorkItemID:   item.WorkItemID,
					ClaimID:      claim.ClaimID,
					FencingToken: claim.FencingToken,
					OwnerID:      claim.OwnerID,
					ObservedAt:   time.Now().UTC(),
				}); err != nil {
					errs <- fmt.Errorf("claimer %d CompleteClaim %q: %w", claimer, item.WorkItemID, err)
					break
				}
			}
			claiming.Done()
			<-release
		}(c)
	}
	for c := 0; c < claimers; c++ {
		<-ready
	}
	// Reset the clock once every claimer holds its connection and is parked
	// on the barrier, so elapsed values measure claiming, not pool setup.
	// This write happens-before close(start), and claimers read startedAt
	// only after <-start, so the channel close orders the access.
	startedAt = time.Now()
	close(start)
	claiming.Wait()
	result.Duration = time.Since(startedAt)
	result.StatsPinned = database.Stats()
	close(release)
	released.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("drain error: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	result.BackendPIDs = backendPIDs
	result.EmptyAttempts = emptyAttempts.Load()
	for _, records := range perClaimer {
		result.Records = append(result.Records, records...)
	}
	sort.Slice(result.Records, func(i, j int) bool { return result.Records[i].Seq < result.Records[j].Seq })
	return result
}

// claimDepthFirstByKey returns the lowest-Seq record per fairness key.
func claimDepthFirstByKey(records []claimDepthRecord) map[string]claimDepthRecord {
	first := make(map[string]claimDepthRecord)
	for _, record := range records {
		if current, ok := first[record.FairnessKey]; !ok || record.Seq < current.Seq {
			first[record.FairnessKey] = record
		}
	}
	return first
}

// claimDepthDuplicateWorkItems lists work item ids granted more than once,
// sorted for stable failure output.
func claimDepthDuplicateWorkItems(records []claimDepthRecord) []string {
	counts := make(map[string]int)
	for _, record := range records {
		counts[record.WorkItemID]++
	}
	var duplicates []string
	for id, count := range counts {
		if count > 1 {
			duplicates = append(duplicates, id)
		}
	}
	sort.Strings(duplicates)
	return duplicates
}

// claimDepthMissing lists seeded ids no claimer was granted, in seed order.
func claimDepthMissing(seeded []string, records []claimDepthRecord) []string {
	granted := make(map[string]bool, len(records))
	for _, record := range records {
		granted[record.WorkItemID] = true
	}
	var missing []string
	for _, id := range seeded {
		if !granted[id] {
			missing = append(missing, id)
		}
	}
	return missing
}

// assertClaimDepthExactlyOnce proves the drain granted every seeded item
// exactly once, under distinct claim ids, with nothing extra granted.
func assertClaimDepthExactlyOnce(t *testing.T, seeded []string, result claimDepthResult) {
	t.Helper()
	if got, want := len(result.Records), len(seeded); got != want {
		t.Fatalf("granted records = %d, want seeded %d", got, want)
	}
	if duplicates := claimDepthDuplicateWorkItems(result.Records); len(duplicates) > 0 {
		t.Fatalf("work items granted more than once: %v", duplicates)
	}
	if missing := claimDepthMissing(seeded, result.Records); len(missing) > 0 {
		t.Fatalf("seeded work items never granted: %v", missing)
	}
	claims := make(map[string]string, len(result.Records))
	for _, record := range result.Records {
		if other, dup := claims[record.ClaimID]; dup {
			t.Fatalf("claim id %q granted for both %q and %q", record.ClaimID, other, record.WorkItemID)
		}
		claims[record.ClaimID] = record.WorkItemID
	}
}

// assertClaimDepthConcurrency proves the claim loops ran on distinct server
// backends at the same time and never waited for a pool connection: every
// claimer held a different pg_backend_pid, all of those connections were
// still in use when claiming finished, and the pool recorded no waits
// during the drain.
func assertClaimDepthConcurrency(t *testing.T, result claimDepthResult, claimers int) {
	t.Helper()
	if result.PoolLimit < claimers {
		t.Fatalf("pool limit = %d, want at least one connection per claimer (%d)", result.PoolLimit, claimers)
	}
	if got := result.StatsPinned.MaxOpenConnections; got != result.PoolLimit {
		t.Fatalf("MaxOpenConnections = %d, want raised limit %d", got, result.PoolLimit)
	}
	seen := make(map[int]int, claimers)
	for claimer, pid := range result.BackendPIDs {
		if pid == 0 {
			t.Fatalf("claimer %d recorded no backend pid", claimer)
		}
		if other, dup := seen[pid]; dup {
			t.Fatalf("claimers %d and %d shared backend pid %d; claims serialized on one connection", other, claimer, pid)
		}
		seen[pid] = claimer
	}
	if got := result.StatsPinned.InUse; got != claimers {
		t.Fatalf("pool InUse with claimers pinned = %d, want %d (one live connection per claimer)", got, claimers)
	}
	if delta := result.StatsPinned.WaitCount - result.StatsBefore.WaitCount; delta != 0 {
		t.Fatalf("pool WaitCount grew by %d during the drain, want 0 (no claimer waited for a connection)", delta)
	}
	for _, record := range result.Records {
		if _, ok := seen[record.BackendPID]; !ok {
			t.Fatalf("record %q carries backend pid %d outside the claimer set %v", record.WorkItemID, record.BackendPID, result.BackendPIDs)
		}
	}
}
