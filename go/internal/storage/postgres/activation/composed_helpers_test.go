// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	lockstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/lock"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// composedWaitingID is the deployment_mapping row a quiet owed generation
// leaves waiting on its backward-evidence phase.
func composedWaitingID(generationID string) string {
	return generationID + "/deployment_mapping"
}

// seedComposedCorpus seeds the #7584 step-4 corpus on one fully bootstrapped
// schema: payments-service (git:tgt), orders-api (git:dep), an unrelated
// ledger-svc (git:solo), and billing-ui (git:in) referencing payments-service
// (one inbound source), each with one succeeded item per reopen domain, then
// the real corpus-wide backfill so every active partition has evidence, a
// phase and a memo at the current catalog.
func seedComposedCorpus(t *testing.T, ctx context.Context, database *sql.DB) *corpus {
	t.Helper()
	p := &corpus{t: t, ctx: ctx, db: database}
	p.gitRepo("git:tgt", "tgt-1", "repo-tgt", "payments-service")
	p.workItems("git:tgt", "tgt-1")
	p.gitRepo("git:dep", "dep-1", "repo-dep", "orders-api")
	p.workItems("git:dep", "dep-1")
	p.gitRepo("git:solo", "solo-1", "repo-solo", "ledger-svc")
	p.workItems("git:solo", "solo-1")
	p.gitRepo("git:in", "in-1", "repo-in", "billing-ui")
	p.terraformRef("in-1-ref", "git:in", "in-1", "repo-in", "in.tf", "payments-service")
	p.workItems("git:in", "in-1")
	p.prepass()
	return p
}

// quietOwed activates a new generation of a one-repository git scope with no
// maintenance pass, as a quiet projector Ack leaves it: the repository, one
// Terraform reference to alias, one succeeded item per reopen domain except
// deployment_mapping, and that domain's row waiting (retrying, not-ready
// class, visible in an hour) on the generation's backward-evidence phase.
func quietOwed(p *corpus, scopeID, generationID, repoID, name, alias string) {
	p.t.Helper()
	p.generation(scopeID, generationID, 90*time.Minute, true)
	p.repo(scopeID, generationID, repoID, name)
	p.terraformRef(generationID+"-ref-"+alias, scopeID, generationID, repoID, "main.tf", alias)
	for _, domain := range reopenDomains() {
		if domain == "deployment_mapping" {
			continue
		}
		p.exec(`INSERT INTO fact_work_items
  (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', $4, 'succeeded', 1, $5, $5)`,
			generationID+"/"+domain, scopeID, generationID, domain, corpusBase)
	}
	p.exec(`INSERT INTO fact_work_items
  (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count,
   visible_at, next_attempt_at, failure_class, failure_message, payload, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', 'deployment_mapping', 'retrying', 1,
   clock_timestamp() + interval '1 hour', clock_timestamp() + interval '1 hour',
   'cross_repo_backward_evidence_not_ready', 'backward evidence not ready', '{}'::jsonb, $4, $4)`,
		composedWaitingID(generationID), scopeID, generationID, corpusBase)
}

// owePending runs the production catch-up so every active generation with a
// repository fact and no phase gets its obligation, and returns how many
// were inserted.
func owePending(t *testing.T, ctx context.Context, database *sql.DB) int {
	t.Helper()
	page, err := activation.NewStore(postgres.SQLDB{DB: database}).CatchUp(ctx, "", 500)
	if err != nil {
		t.Fatalf("activation catch-up: %v", err)
	}
	return page.Inserted
}

// composedState is the durable state the step-4 proofs compare, as sorted
// tuples with no timestamps or transaction ids: every obligation, every
// backward-evidence phase, evidence counts per generation and repository
// pair, and every reducer work item's status, class, visibility and lease.
func composedState(t *testing.T, ctx context.Context, database *sql.DB) []string {
	t.Helper()
	rows, err := database.QueryContext(ctx, `
SELECT 'obligation|' || scope_id || '|' || generation_id || '|' || state FROM activation_obligations
UNION ALL
SELECT 'phase|' || scope_id || '|' || generation_id || '|' || keyspace || '|' || phase
FROM graph_projection_phase_state
UNION ALL
SELECT 'evidence|' || generation_id || '|' || source_repo_id || '|' || target_repo_id || '|' || count(*)
FROM relationship_evidence_facts GROUP BY generation_id, source_repo_id, target_repo_id
UNION ALL
SELECT 'work|' || work_item_id || '|' || status || '|' || COALESCE(failure_class, '') || '|' ||
    (visible_at IS NULL OR visible_at <= clock_timestamp())::text || '|' ||
    (lease_owner IS NOT NULL OR claim_until IS NOT NULL)::text
FROM fact_work_items WHERE stage = 'reducer'`)
	if err != nil {
		t.Fatalf("read composed state: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var state []string
	for rows.Next() {
		var tuple string
		if err := rows.Scan(&tuple); err != nil {
			t.Fatal(err)
		}
		state = append(state, tuple)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	slices.Sort(state)
	return state
}

// requireComposedState fails with the exact tuple difference.
func requireComposedState(t *testing.T, what string, got, want []string) {
	t.Helper()
	if slices.Equal(got, want) {
		return
	}
	var missing, extra []string
	for _, tuple := range want {
		if !slices.Contains(got, tuple) {
			missing = append(missing, tuple)
		}
	}
	for _, tuple := range got {
		if !slices.Contains(want, tuple) {
			extra = append(extra, tuple)
		}
	}
	t.Fatalf("%s: durable state differs\n missing=%v\n extra=%v", what, missing, extra)
}

// lockWaitBeginner wraps a store's beginner: it counts exclusive deferred
// maintenance lock acquisitions and those that waited, and runs an optional
// statement hook. It is safe for the concurrent batch workers of a pass.
type lockWaitBeginner struct {
	inner  db.Beginner
	waited atomic.Int32
	taken  atomic.Int32
	onExec func(ctx context.Context, query string) error
	// afterLock runs after each exclusive lock acquisition, holding it.
	afterLock func(ctx context.Context, repoKey string)
}

func (b *lockWaitBeginner) Begin(ctx context.Context) (db.Transaction, error) {
	tx, err := b.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return lockWaitTransaction{Transaction: tx, beginner: b}, nil
}

type lockWaitTransaction struct {
	db.Transaction
	beginner *lockWaitBeginner
}

func (tx lockWaitTransaction) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if tx.beginner.onExec != nil {
		if err := tx.beginner.onExec(ctx, query); err != nil {
			return nil, err
		}
	}
	if query != lockstore.DeferredMaintenancePartitionedExclusiveLockSQL {
		return tx.Transaction.ExecContext(ctx, query, args...)
	}
	started := time.Now()
	result, err := tx.Transaction.ExecContext(ctx, query, args...)
	tx.beginner.taken.Add(1)
	if time.Since(started) > 5*time.Millisecond {
		tx.beginner.waited.Add(1)
	}
	if err == nil && tx.beginner.afterLock != nil && len(args) == 2 {
		if key, ok := args[1].(string); ok {
			tx.beginner.afterLock(ctx, key)
		}
	}
	return result, err
}

// lockWaitDB is SQLDB whose transactions go through a lockWaitBeginner.
type lockWaitDB struct {
	postgres.SQLDB
	beginner *lockWaitBeginner
}

// Begin implements db.Beginner through the wrapper.
func (d lockWaitDB) Begin(ctx context.Context) (db.Transaction, error) { return d.beginner.Begin(ctx) }

// newLockWaitStore returns an ingestion store on database whose transactions
// go through a lockWaitBeginner, and that beginner.
func newLockWaitStore(database *sql.DB) (postgres.IngestionStore, *lockWaitBeginner) {
	wrapped := &lockWaitBeginner{inner: postgres.SQLDB{DB: database}}
	return postgres.NewIngestionStore(lockWaitDB{SQLDB: postgres.SQLDB{DB: database}, beginner: wrapped}), wrapped
}

// composedConsumer is one consumer replica: the production runner with the
// production partition-scoped maintainer, a callback counter and its own
// metrics.
type composedConsumer struct {
	runner *maintenance.ActivationObligationRunner
	port   *composedPort
	reader *sdkmetric.ManualReader
	locks  *lockWaitBeginner
}

// composedPort counts callbacks per generation, keeps every callback error
// and claimed obligation, and runs an optional hook before the production
// maintainer.
type composedPort struct {
	inner  maintenance.ActivationMaintainer
	before func(ctx context.Context, work maintenance.ActivationObligation) error
	mu     sync.Mutex
	calls  map[string]int
	errs   []error
	seen   []maintenance.ActivationObligation
}

func (p *composedPort) MaintainActivation(ctx context.Context, work maintenance.ActivationObligation) error {
	p.mu.Lock()
	if p.calls == nil {
		p.calls = map[string]int{}
	}
	p.calls[work.GenerationID]++
	p.seen = append(p.seen, work)
	before := p.before
	p.mu.Unlock()
	var err error
	if before != nil {
		err = before(ctx, work)
	}
	if err == nil {
		err = p.inner.MaintainActivation(ctx, work)
	}
	if err != nil {
		p.mu.Lock()
		p.errs = append(p.errs, err)
		p.mu.Unlock()
	}
	return err
}

func (p *composedPort) callsFor(generationID string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[generationID]
}

func (p *composedPort) total() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, c := range p.calls {
		n += c
	}
	return n
}

// newComposedConsumer builds one replica named owner on database.
func newComposedConsumer(t *testing.T, database *sql.DB, owner string, lease time.Duration, maxPerCycle int) *composedConsumer {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter(owner))
	if err != nil {
		t.Fatal(err)
	}
	store, locks := newLockWaitStore(database)
	port := &composedPort{inner: postgres.NewActivationMaintainer(store, nil, instruments)}
	return &composedConsumer{
		runner: &maintenance.ActivationObligationRunner{
			Store:       activation.RunnerStore{Store: activation.NewStore(postgres.SQLDB{DB: database})},
			Maintainer:  port,
			Config:      maintenance.ActivationObligationRunnerConfig{Owner: owner, Lease: lease, MaxPerCycle: maxPerCycle},
			Instruments: instruments,
		},
		port: port, reader: reader, locks: locks,
	}
}

func (c *composedConsumer) metrics(t *testing.T, ctx context.Context) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := c.reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}
	return rm
}

// failures returns the replica's activation failures with reason.
func (c *composedConsumer) failures(t *testing.T, ctx context.Context, reason string) int64 {
	t.Helper()
	key := ""
	if reason != "" {
		key = "reason"
	}
	return counterValue(c.metrics(t, ctx), "eshu_dp_activation_obligation_failures_total", key, reason)
}

// requireNoFailures fails when the replica recorded any failure (claim,
// finalize, maintenance, catch-up, prune or stats) or a callback error.
func (c *composedConsumer) requireNoFailures(t *testing.T, ctx context.Context) {
	t.Helper()
	if got := c.failures(t, ctx, ""); got != 0 {
		t.Fatalf("%s recorded %d activation failures, callback errors %v", c.runner.Config.Owner, got, c.port.errs)
	}
	if len(c.port.errs) > 0 {
		t.Fatalf("%s callback errors: %v", c.runner.Config.Owner, c.port.errs)
	}
}

// sqlState returns the PostgreSQL SQLSTATE of err, or an empty string.
func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// releaseTogether runs every fn on its own goroutine, releases them from one
// channel barrier at the same time, and returns their errors in order.
func releaseTogether(fns ...func() error) []error {
	start := make(chan struct{})
	errs := make([]error, len(fns))
	var wg sync.WaitGroup
	for i, fn := range fns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = fn()
		}()
	}
	close(start)
	wg.Wait()
	return errs
}

// awaitLockWaiter waits until a backend whose statement matches like is
// blocked on a heavyweight lock: the observable barrier of the scope-lock
// races (never a sleep).
func awaitLockWaiter(t *testing.T, ctx context.Context, database *sql.DB, like string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting bool
		if err := database.QueryRowContext(ctx, `SELECT EXISTS (
SELECT 1 FROM pg_stat_activity
WHERE wait_event_type = 'Lock' AND query LIKE $1 AND pid <> pg_backend_pid())`, like).Scan(&waiting); err != nil {
			t.Fatalf("read lock waiters: %v", err)
		}
		if waiting {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no backend waiting on a lock for %q", like)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// awaitScopeRowLocked waits until another transaction holds scopeID's row
// lock, probed with FOR UPDATE NOWAIT in a rolled-back transaction.
func awaitScopeRowLocked(t *testing.T, ctx context.Context, database *sql.DB, scopeID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.ExecContext(ctx, "SELECT 1 FROM ingestion_scopes WHERE scope_id = $1 FOR UPDATE NOWAIT", scopeID)
		_ = tx.Rollback()
		if sqlState(err) == "55P03" {
			return
		}
		if err != nil {
			t.Fatalf("probe scope row lock: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("scope %s row was never locked", scopeID)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// obligationOf converts a consumer's view of a claimed obligation into the
// store's.
func obligationOf(work maintenance.ActivationObligation) activation.Obligation {
	return activation.Obligation{
		ScopeID: work.ScopeID, GenerationID: work.GenerationID,
		LeaseOwner: work.LeaseOwner, LeaseToken: work.LeaseToken, LeaseUntil: work.LeaseUntil,
	}
}

// requireWoken requires the waiting row of generationID in the exact shape
// the wake leaves (retrying, not-ready class, one attempt, no lease), visible
// now when want is set and still a future retry otherwise.
func requireWoken(t *testing.T, ctx context.Context, database *sql.DB, generationID string, want bool) {
	t.Helper()
	var status, class string
	var attempts int
	var visible, leased bool
	if err := database.QueryRowContext(ctx, `SELECT status, failure_class, attempt_count,
    visible_at <= clock_timestamp(), lease_owner IS NOT NULL OR claim_until IS NOT NULL
FROM fact_work_items WHERE work_item_id = $1`, composedWaitingID(generationID)).Scan(
		&status, &class, &attempts, &visible, &leased); err != nil {
		t.Fatalf("read waiting row of %s: %v", generationID, err)
	}
	got := fmt.Sprintf("%s/%s/%d/visible=%t/leased=%t", status, class, attempts, visible, leased)
	wantText := fmt.Sprintf("retrying/cross_repo_backward_evidence_not_ready/1/visible=%t/leased=false", want)
	if got != wantText {
		t.Fatalf("waiting row of %s = %s, want %s", generationID, got, wantText)
	}
}

// sortedTuples returns a sorted copy of state.
func sortedTuples(state []string) []string {
	out := slices.Clone(state)
	slices.Sort(out)
	return out
}
