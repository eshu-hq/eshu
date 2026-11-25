// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// This file proves issue #11460: the corpus-wide relationship-backfill reads
// must never hold one unbounded snapshot for the whole load. A 3h37m
// anchor-scoped read with no timeout stalled a production migration whose
// concurrent index build waited for the loader's old snapshot. The tests pin
// the replacement contract: keyset-bounded pages, each page its own short
// transaction carrying SET LOCAL statement_timeout and a path-distinct
// application_name, closed before the next page begins, with timeouts failing
// loudly through ErrRelationshipFactLoadBudgetExceeded.

// fakeSnapshotRegistry models the backend-snapshot state a concurrent
// CREATE INDEX CONCURRENTLY build observes: every open page transaction is a
// snapshot the build must wait out.
type fakeSnapshotRegistry struct {
	mu     sync.Mutex
	open   map[*fakeBackfillPageTx]time.Time
	events []string
}

func newFakeSnapshotRegistry() *fakeSnapshotRegistry {
	return &fakeSnapshotRegistry{open: make(map[*fakeBackfillPageTx]time.Time)}
}

func (r *fakeSnapshotRegistry) begin(tx *fakeBackfillPageTx) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.open[tx] = time.Now()
	r.events = append(r.events, "open")
}

func (r *fakeSnapshotRegistry) end(tx *fakeBackfillPageTx, outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.open, tx)
	r.events = append(r.events, outcome)
}

// openCount reports how many page transactions are open right now.
func (r *fakeSnapshotRegistry) openCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.open)
}

// checkpoint captures the transactions open right now.
func (r *fakeSnapshotRegistry) checkpoint() map[*fakeBackfillPageTx]struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[*fakeBackfillPageTx]struct{}, len(r.open))
	for tx := range r.open {
		out[tx] = struct{}{}
	}
	return out
}

// waitForDrain blocks until every checkpointed transaction closes, the way a
// concurrent build's "waiting for old snapshots" phase does. It returns false
// on timeout: the snapshots never drained, so the build stalls.
func (r *fakeSnapshotRegistry) waitForDrain(checkpoint map[*fakeBackfillPageTx]struct{}, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		r.mu.Lock()
		drained := true
		for tx := range checkpoint {
			if _, ok := r.open[tx]; ok {
				drained = false
				break
			}
		}
		r.mu.Unlock()
		if drained {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// fakePageConnProvider is a PageConnProvider whose scripted row pages are
// shared across every page transaction, in query order. It also answers the
// version-1 fallback probe with zero missing rows without consuming a page,
// mirroring the fakeExecQueryer contract.
type fakePageConnProvider struct {
	mu              sync.Mutex
	pages           []queueFakeRows
	queryErrs       map[int]error
	queryCount      int
	conns           []*fakeBackfillPageConn
	txns            []*fakeBackfillPageTx
	registry        *fakeSnapshotRegistry
	querySleep      time.Duration
	blockFirstQuery chan struct{}
	entered         chan struct{}
	enteredOnce     sync.Once
	openErr         error
}

func (p *fakePageConnProvider) OpenBackfillPageConn(context.Context) (PageConn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.openErr != nil {
		return nil, p.openErr
	}
	conn := &fakeBackfillPageConn{provider: p}
	p.conns = append(p.conns, conn)
	return conn, nil
}

// QueryContext answers only the version-1 fallback probe, which the partition
// loader issues on the handle itself outside the page transaction.
func (p *fakePageConnProvider) QueryContext(
	_ context.Context, query string, _ ...any,
) (db.Rows, error) {
	if query == DeferredRelationshipKeyFallbackProbeSQL {
		return &queueFakeRows{rows: [][]any{{int64(0)}}}, nil
	}
	return nil, errors.New("fakePageConnProvider answers only the fallback probe")
}

type fakeBackfillPageConn struct {
	provider *fakePageConnProvider
	closed   bool
}

func (c *fakeBackfillPageConn) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("fakeBackfillPageConn runs queries only inside a page transaction")
}

func (c *fakeBackfillPageConn) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, errors.New("fakeBackfillPageConn runs queries only inside a page transaction")
}

func (c *fakeBackfillPageConn) Begin(context.Context) (db.Transaction, error) {
	tx := &fakeBackfillPageTx{provider: c.provider}
	c.provider.mu.Lock()
	c.provider.txns = append(c.provider.txns, tx)
	c.provider.mu.Unlock()
	if c.provider.registry != nil {
		c.provider.registry.begin(tx)
	}
	return tx, nil
}

func (c *fakeBackfillPageConn) Close() error {
	c.closed = true
	return nil
}

type fakeBackfillPageTx struct {
	provider  *fakePageConnProvider
	mu        sync.Mutex
	execs     []fakeExecCall
	queries   []fakeQueryCall
	committed bool
	ended     bool
}

func (tx *fakeBackfillPageTx) end(outcome string) {
	tx.mu.Lock()
	already := tx.ended
	tx.ended = true
	if outcome == "commit" {
		tx.committed = true
	}
	tx.mu.Unlock()
	if already {
		return
	}
	if tx.provider.registry != nil {
		tx.provider.registry.end(tx, outcome)
	}
}

func (tx *fakeBackfillPageTx) ExecContext(
	_ context.Context, query string, args ...any,
) (sql.Result, error) {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	tx.execs = append(tx.execs, fakeExecCall{query: query, args: args})
	return fakeResult{}, nil
}

func (tx *fakeBackfillPageTx) QueryContext(
	ctx context.Context, query string, args ...any,
) (db.Rows, error) {
	p := tx.provider
	p.mu.Lock()
	index := p.queryCount
	p.queryCount++
	err, hasErr := p.queryErrs[index]
	block := p.blockFirstQuery
	sleep := p.querySleep
	p.mu.Unlock()

	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if block != nil {
		p.enteredOnce.Do(func() { close(p.entered) })
		select {
		case <-block:
		case <-time.After(30 * time.Second):
			return nil, errors.New("fakeBackfillPageTx: first query block never released")
		}
	}
	if sleep > 0 {
		time.Sleep(sleep)
	}
	if hasErr {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	tx.mu.Lock()
	tx.queries = append(tx.queries, fakeQueryCall{query: query, args: args})
	tx.mu.Unlock()
	if len(p.pages) == 0 {
		return &queueFakeRows{}, nil
	}
	page := p.pages[0]
	p.pages = p.pages[1:]
	return &page, nil
}

func (tx *fakeBackfillPageTx) Commit() error {
	tx.end("commit")
	return nil
}

func (tx *fakeBackfillPageTx) Rollback() error {
	tx.end("rollback")
	return nil
}

// scriptAnchorPage builds one scripted result page of content rows sharing one
// observed_at, so the keyset cursor advances on fact_id alone.
func scriptAnchorPage(payload string, ids ...string) queueFakeRows {
	rows := make([][]any, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, contentFactRow(id, "scope-1", "gen-1", "content", payload))
	}
	return queueFakeRows{rows: rows}
}

func anchorPayload() string {
	return `{"content_path":"main.tf","content_body":"module \"x\" { source = \"./payments-service\" }"}`
}

func assertPageGuards(t *testing.T, tx *fakeBackfillPageTx, wantTimeout, wantApp string) {
	t.Helper()
	if len(tx.execs) != 2 {
		t.Fatalf("page transaction execs = %d, want 2 (SET LOCAL statement_timeout + application_name): %#v", len(tx.execs), tx.execs)
	}
	if got, want := tx.execs[0].query, "SET LOCAL statement_timeout = '"+wantTimeout+"'"; got != want {
		t.Fatalf("guard[0] = %q, want %q", got, want)
	}
	if got, want := tx.execs[1].query, "SET LOCAL application_name = '"+wantApp+"'"; got != want {
		t.Fatalf("guard[1] = %q, want %q", got, want)
	}
	if !tx.committed {
		t.Fatalf("page transaction was not committed")
	}
}

// TestBackfillApplicationNamesAreDistinctPerPath pins scope (c): every
// backfill query path carries its own application_name so an operator can
// tell the ingester per-commit load, the deferred worker load, and the
// targeted closure load apart in pg_stat_activity.
func TestBackfillApplicationNamesAreDistinctPerPath(t *testing.T) {
	t.Parallel()

	names := map[string]string{
		"per-commit": BackfillApplicationNamePerCommit,
		"deferred":   BackfillApplicationNameDeferred,
		"targeted":   BackfillApplicationNameTargeted,
	}
	seen := make(map[string]string)
	for path, name := range names {
		if name == "" {
			t.Fatalf("%s application name is empty", path)
		}
		if other, dup := seen[name]; dup {
			t.Fatalf("application name %q shared by %s and %s: backfill paths must be distinct", name, other, path)
		}
		seen[name] = path
	}
}

// TestPagedAnchorLoadUsesBoundedPagesWithGuards is the core #11460 contract:
// the anchor-scoped load pages by keyset with a bounded LIMIT, runs every
// page in its own short transaction guarded by statement_timeout and the
// per-commit application_name, and loads every row exactly once, in order.
func TestPagedAnchorLoadUsesBoundedPagesWithGuards(t *testing.T) {
	t.Parallel()

	payload := anchorPayload()
	provider := &fakePageConnProvider{
		pages: []queueFakeRows{
			scriptAnchorPage(payload, "fact-1", "fact-2"),
			scriptAnchorPage(payload, "fact-3", "fact-4"),
			scriptAnchorPage(payload, "fact-5"),
		},
		registry: newFakeSnapshotRegistry(),
	}

	loaded, stats, err := loadOnboardedRepoScopedRelationshipFactsBudget(
		context.Background(), provider, []string{"payments-service"}, RelationshipFactLoadBudget{}, 2,
	)
	if err != nil {
		t.Fatalf("paged anchor load: %v", err)
	}
	if len(loaded) != 5 {
		t.Fatalf("loaded %d facts, want 5", len(loaded))
	}
	for i, want := range []string{"fact-1", "fact-2", "fact-3", "fact-4", "fact-5"} {
		if loaded[i].FactID != want {
			t.Fatalf("loaded[%d].FactID = %q, want %q", i, loaded[i].FactID, want)
		}
	}
	if stats.Rows != 5 || stats.Bytes != int64(5*len(payload)) {
		t.Fatalf("stats = %+v, want rows=5 bytes=%d", stats, 5*len(payload))
	}

	if len(provider.conns) != 3 {
		t.Fatalf("dedicated connections = %d, want 1 per page", len(provider.conns))
	}
	for i, conn := range provider.conns {
		if !conn.closed {
			t.Fatalf("dedicated connection %d was not closed after its page", i)
		}
	}
	if len(provider.txns) != 3 {
		t.Fatalf("page transactions = %d, want 3 (5 rows at page size 2)", len(provider.txns))
	}
	for _, tx := range provider.txns {
		assertPageGuards(t, tx, "300s", BackfillApplicationNamePerCommit)
		if len(tx.queries) != 1 {
			t.Fatalf("page transaction issued %d queries, want exactly 1", len(tx.queries))
		}
	}
	if got := provider.txns[0].queries[0].query; got != ListOnboardedRepoScopedRelationshipFactRecordsFirstPageQuery {
		t.Fatalf("first page query is not the first-page shape:\n%s", got)
	}
	for i := 1; i <= 2; i++ {
		if got := provider.txns[i].queries[0].query; got != ListOnboardedRepoScopedRelationshipFactRecordsCursorPageQuery {
			t.Fatalf("page %d query is not the cursor-page shape:\n%s", i+1, got)
		}
	}
	firstArgs := provider.txns[0].queries[0].args
	if len(firstArgs) != 2 {
		t.Fatalf("first page args = %d, want 2 (LIKE terms + LIMIT)", len(firstArgs))
	}
	if limit, ok := firstArgs[1].(int); !ok || limit != 2 {
		t.Fatalf("first page LIMIT = %#v, want 2", firstArgs[1])
	}
	secondArgs := provider.txns[1].queries[0].args
	if len(secondArgs) != 4 {
		t.Fatalf("cursor page args = %d, want 4 (LIKE terms + observed_at + fact_id + LIMIT)", len(secondArgs))
	}
	if got, want := secondArgs[2].(string), "fact-2"; got != want {
		t.Fatalf("second page cursor fact_id = %q, want %q", got, want)
	}
	thirdArgs := provider.txns[2].queries[0].args
	if got, want := thirdArgs[2].(string), "fact-4"; got != want {
		t.Fatalf("third page cursor fact_id = %q, want %q", got, want)
	}

	// Snapshot hygiene: transactions strictly serialize — at no point are
	// two page snapshots open at once, and every page closed before the next
	// began. The registry event log must alternate open/commit with nothing
	// left open.
	if got := provider.registry.openCount(); got != 0 {
		t.Fatalf("open page transactions after load = %d, want 0", got)
	}
	if len(provider.registry.events) != 6 {
		t.Fatalf("registry events = %v, want 3 open/commit pairs", provider.registry.events)
	}
	for i := 0; i < 6; i += 2 {
		if provider.registry.events[i] != "open" || provider.registry.events[i+1] != "commit" {
			t.Fatalf("registry events = %v, want strict open/commit alternation", provider.registry.events)
		}
	}
}

// TestPagedAnchorLoadPropagatesTargetedApplicationName pins that a caller-set
// path name wins over the loader default: the targeted closure runs the same
// anchor load as per-commit but must stay identifiable.
func TestPagedAnchorLoadPropagatesTargetedApplicationName(t *testing.T) {
	t.Parallel()

	provider := &fakePageConnProvider{
		pages:    []queueFakeRows{scriptAnchorPage(anchorPayload(), "fact-1")},
		registry: newFakeSnapshotRegistry(),
	}
	ctx := WithBackfillApplicationName(context.Background(), BackfillApplicationNameTargeted)
	if _, _, err := loadOnboardedRepoScopedRelationshipFactsBudget(
		ctx, provider, []string{"payments-service"}, RelationshipFactLoadBudget{}, 2,
	); err != nil {
		t.Fatalf("paged anchor load: %v", err)
	}
	if len(provider.txns) != 1 {
		t.Fatalf("page transactions = %d, want 1", len(provider.txns))
	}
	assertPageGuards(t, provider.txns[0], "300s", BackfillApplicationNameTargeted)
}

// TestPagedAnchorLoadReleasesSnapshotBetweenPages is the migration-354
// regression pair in hermetic form. RED on the old shape: one unbounded query
// holds its snapshot for the whole load, so a concurrent build that must wait
// out the checkpointed snapshots stalls until the load finishes. GREEN on the
// paged shape: each page commits promptly, so the build's wait drains while
// the loader is still on later pages.
func TestPagedAnchorLoadReleasesSnapshotBetweenPages(t *testing.T) {
	t.Parallel()

	payload := anchorPayload()
	provider := &fakePageConnProvider{
		pages: []queueFakeRows{
			scriptAnchorPage(payload, "fact-1"),
			scriptAnchorPage(payload, "fact-2"),
			scriptAnchorPage(payload, "fact-3"),
		},
		registry:        newFakeSnapshotRegistry(),
		querySleep:      300 * time.Millisecond,
		blockFirstQuery: make(chan struct{}),
		entered:         make(chan struct{}),
	}

	loaderDone := make(chan error, 1)
	go func() {
		_, _, err := loadOnboardedRepoScopedRelationshipFactsBudget(
			context.Background(), provider, []string{"payments-service"}, RelationshipFactLoadBudget{}, 1,
		)
		loaderDone <- err
	}()

	select {
	case <-provider.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("loader never entered its first page query")
	}
	buildDone := make(chan bool, 1)
	checkpointed := make(chan struct{})
	go func() {
		checkpoint := provider.registry.checkpoint()
		close(checkpointed)
		buildDone <- provider.registry.waitForDrain(checkpoint, 5*time.Second)
	}()
	select {
	case <-checkpointed:
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent build never took its snapshot checkpoint")
	}
	close(provider.blockFirstQuery)

	select {
	case drained := <-buildDone:
		if !drained {
			t.Fatal("concurrent build stalled: the loader held a snapshot across pages (migration-354 RED)")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent build never finished while the loader ran")
	}
	select {
	case err := <-loaderDone:
		t.Fatalf("loader finished before the concurrent build drained its snapshots: %v", err)
	default:
	}
	if err := <-loaderDone; err != nil {
		t.Fatalf("paged anchor load: %v", err)
	}
}

// TestPagedAnchorLoadLegacyHoldStallsConcurrentBuild is the control for the
// pair above: it runs the same checkpoint-and-drain harness against the old
// behavior (one transaction held open for the whole load) and requires the
// build to stall. If this control ever goes green, the harness no longer
// discriminates and the GREEN above proves nothing.
func TestPagedAnchorLoadLegacyHoldStallsConcurrentBuild(t *testing.T) {
	t.Parallel()

	registry := newFakeSnapshotRegistry()
	provider := &fakePageConnProvider{registry: registry}
	conn, err := provider.OpenBackfillPageConn(context.Background())
	if err != nil {
		t.Fatalf("open dedicated connection: %v", err)
	}
	defer func() { _ = conn.Close() }()
	legacyTx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin legacy read transaction: %v", err)
	}
	// The legacy load holds this one transaction — and therefore its
	// snapshot — across all client-side processing.
	holdDone := make(chan struct{})
	go func() {
		defer close(holdDone)
		time.Sleep(900 * time.Millisecond)
		_ = legacyTx.Rollback()
	}()

	checkpoint := registry.checkpoint()
	if len(checkpoint) != 1 {
		t.Fatalf("checkpointed %d open snapshots, want the 1 legacy hold", len(checkpoint))
	}
	if registry.waitForDrain(checkpoint, 400*time.Millisecond) {
		t.Fatal("concurrent build drained while the legacy load still held its snapshot: harness does not discriminate")
	}
	<-holdDone
}

// TestPagedAnchorLoadStatementTimeoutFailsLoudly pins scope (a): a page that
// dies to statement_timeout fails the whole load through
// ErrRelationshipFactLoadBudgetExceeded — the existing fail-loud pattern —
// with the rows and bytes read so far, never a silent truncation.
func TestPagedAnchorLoadStatementTimeoutFailsLoudly(t *testing.T) {
	t.Parallel()

	payload := anchorPayload()
	provider := &fakePageConnProvider{
		pages: []queueFakeRows{
			scriptAnchorPage(payload, "fact-1", "fact-2"),
			scriptAnchorPage(payload, "fact-3"),
		},
		queryErrs: map[int]error{1: errors.New("canceling statement due to statement timeout")},
		registry:  newFakeSnapshotRegistry(),
	}

	loaded, stats, err := loadOnboardedRepoScopedRelationshipFactsBudget(
		context.Background(), provider, []string{"payments-service"}, RelationshipFactLoadBudget{}, 2,
	)
	if !errors.Is(err, ErrRelationshipFactLoadBudgetExceeded) {
		t.Fatalf("err = %v, want ErrRelationshipFactLoadBudgetExceeded", err)
	}
	if loaded != nil {
		t.Fatalf("loaded = %d facts, want nil on timeout failure", len(loaded))
	}
	if stats.Rows != 2 || stats.Bytes != int64(2*len(payload)) {
		t.Fatalf("stats = %+v, want the 2 first-page rows reported", stats)
	}
	if len(provider.txns) != 2 {
		t.Fatalf("page transactions = %d, want 2 (first committed, second rolled back)", len(provider.txns))
	}
	if !provider.txns[0].committed || provider.txns[1].committed {
		t.Fatal("want first page committed and timed-out page rolled back")
	}
}

// TestPagedAnchorLoadDeadlineExceededFailsLoudly pins the ctx-timeout half of
// scope (a): when the overall load deadline fires, the load fails through the
// same budget-exceeded sentinel.
func TestPagedAnchorLoadDeadlineExceededFailsLoudly(t *testing.T) {
	t.Parallel()

	provider := &fakePageConnProvider{
		pages:    []queueFakeRows{scriptAnchorPage(anchorPayload(), "fact-1")},
		registry: newFakeSnapshotRegistry(),
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, _, err := loadOnboardedRepoScopedRelationshipFactsBudget(
		ctx, provider, []string{"payments-service"}, RelationshipFactLoadBudget{}, 2,
	)
	if !errors.Is(err, ErrRelationshipFactLoadBudgetExceeded) {
		t.Fatalf("err = %v, want ErrRelationshipFactLoadBudgetExceeded", err)
	}
}

// TestPagedAnchorLoadCanceledContextPropagates pins that a plain cancellation
// is not misreported as a budget breach: it propagates as context.Canceled,
// still loud, never silent.
func TestPagedAnchorLoadCanceledContextPropagates(t *testing.T) {
	t.Parallel()

	provider := &fakePageConnProvider{
		pages:    []queueFakeRows{scriptAnchorPage(anchorPayload(), "fact-1")},
		registry: newFakeSnapshotRegistry(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := loadOnboardedRepoScopedRelationshipFactsBudget(
		ctx, provider, []string{"payments-service"}, RelationshipFactLoadBudget{}, 2,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if errors.Is(err, ErrRelationshipFactLoadBudgetExceeded) {
		t.Fatalf("err = %v, must not map a plain cancellation to the budget sentinel", err)
	}
}

// TestDeferredPartitionLoadUses120sGuard pins the per-partition half of scope
// (a): one partition load runs in its own short transaction with a 120s
// statement_timeout and the deferred application_name.
func TestDeferredPartitionLoadUses120sGuard(t *testing.T) {
	t.Parallel()

	provider := &fakePageConnProvider{
		pages: []queueFakeRows{
			{rows: [][]any{contentFactRow("fact-1", "scope-1", "gen-1", "content", anchorPayload())}},
		},
		registry: newFakeSnapshotRegistry(),
	}

	loaded, err := loadDeferredScopedRelationshipFactsForPartition(
		context.Background(), provider, DeferredScopedFactQueryParams{}, "scope-1", "gen-1",
	)
	if err != nil {
		t.Fatalf("partition load: %v", err)
	}
	if len(loaded) != 1 || loaded[0].FactID != "fact-1" {
		t.Fatalf("loaded = %v, want the 1 scripted fact", loaded)
	}
	if len(provider.txns) != 1 {
		t.Fatalf("page transactions = %d, want 1", len(provider.txns))
	}
	assertPageGuards(t, provider.txns[0], "120s", BackfillApplicationNameDeferred)
	if got, want := provider.txns[0].queries[0].query, ListDeferredScopedRelationshipFactRecordsQuery; got != want {
		t.Fatalf("partition query changed shape:\n%s", got)
	}
}

// TestPagedConfigLoadKeysPages pins that the phase-two ArgoCD config reload —
// the secondary suspect in the 3h37m incident — pages by the same keyset with
// the same guards instead of one unbounded corpus scan.
func TestPagedConfigLoadKeysPages(t *testing.T) {
	t.Parallel()

	payload := `{"repo_id":"repo-config","content_path":"app.yaml","content":"kind: Application"}`
	provider := &fakePageConnProvider{
		pages: []queueFakeRows{
			{rows: [][]any{
				contentFactRow("cfg-1", "scope-c", "gen-c", "content", payload),
				contentFactRow("cfg-2", "scope-c", "gen-c", "content", payload),
			}},
			{rows: [][]any{
				contentFactRow("cfg-3", "scope-c", "gen-c", "content", payload),
			}},
		},
		registry: newFakeSnapshotRegistry(),
	}
	ctx := WithBackfillApplicationName(context.Background(), BackfillApplicationNameDeferred)

	loaded, err := loadArgoCDGeneratorConfigFactsPaged(ctx, provider, []string{"repo-config"}, 2)
	if err != nil {
		t.Fatalf("paged config load: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("loaded %d facts, want 3", len(loaded))
	}
	for i, want := range []string{"cfg-1", "cfg-2", "cfg-3"} {
		if loaded[i].FactID != want {
			t.Fatalf("loaded[%d].FactID = %q, want %q", i, loaded[i].FactID, want)
		}
	}
	if len(provider.txns) != 2 {
		t.Fatalf("page transactions = %d, want 2", len(provider.txns))
	}
	for _, tx := range provider.txns {
		assertPageGuards(t, tx, "300s", BackfillApplicationNameDeferred)
	}
	if got := provider.txns[0].queries[0].query; got != ListArgoCDGeneratorConfigFactRecordsFirstPageQuery {
		t.Fatalf("first config page query is not the first-page shape:\n%s", got)
	}
	if got := provider.txns[1].queries[0].query; got != ListArgoCDGeneratorConfigFactRecordsCursorPageQuery {
		t.Fatalf("second config page query is not the cursor-page shape:\n%s", got)
	}
	cursorArgs := provider.txns[1].queries[0].args
	if len(cursorArgs) != 4 {
		t.Fatalf("config cursor page args = %d, want 4 (repo ids + observed_at + fact_id + LIMIT)", len(cursorArgs))
	}
	if got, want := cursorArgs[2].(string), "cfg-2"; got != want {
		t.Fatalf("config cursor fact_id = %q, want %q", got, want)
	}
}

// TestPagedFactQueryShapes pins the SQL contract for the new statements: both
// keyset pages derive from the shipped corpus query (byte-prefix, per the
// differential rule), keep the latest-generation CTE, and carry the row-value
// cursor plus LIMIT. The planner warning from #6154 applies here too: the
// first and cursor pages stay separate statements so the row comparison is a
// plain predicate under a generic plan.
func TestPagedFactQueryShapes(t *testing.T) {
	t.Parallel()

	type pageShapes struct {
		name   string
		base   string
		first  string
		cursor string
	}
	for _, shape := range []pageShapes{
		{
			name:   "anchor",
			base:   ListOnboardedRepoScopedRelationshipFactRecordsQuery,
			first:  ListOnboardedRepoScopedRelationshipFactRecordsFirstPageQuery,
			cursor: ListOnboardedRepoScopedRelationshipFactRecordsCursorPageQuery,
		},
		{
			name:   "config",
			base:   ListArgoCDGeneratorConfigFactRecordsQuery,
			first:  ListArgoCDGeneratorConfigFactRecordsFirstPageQuery,
			cursor: ListArgoCDGeneratorConfigFactRecordsCursorPageQuery,
		},
	} {
		name, base, first, cursor := shape.name, shape.base, shape.first, shape.cursor
		const orderByTail = "\nORDER BY fact.observed_at ASC, fact.fact_id ASC\n"
		if !strings.HasSuffix(base, orderByTail) {
			t.Fatalf("%s shipped query does not end with the stable ORDER BY boundary", name)
		}
		body := strings.TrimSuffix(base, orderByTail)
		if !strings.HasPrefix(first, body) {
			t.Fatalf("%s first-page query is not derived from the shipped query; differentials must derive, never copy", name)
		}
		if !strings.HasPrefix(cursor, body) {
			t.Fatalf("%s cursor-page query is not derived from the shipped query", name)
		}
		for _, want := range []string{
			"DISTINCT ON (generation.scope_id)",
			"latest.generation_id IS NOT NULL",
			"((fact.observed_at, fact.fact_id) > (",
			"ORDER BY fact.observed_at ASC, fact.fact_id ASC",
			"LIMIT $",
		} {
			if !strings.Contains(cursor, want) {
				t.Fatalf("%s cursor-page query missing %q", name, want)
			}
		}
		if strings.Contains(cursor, "IS NULL OR") || strings.Contains(cursor, "($2::timestamptz IS NULL") {
			t.Fatalf("%s cursor-page query folds the first page into a NULL-guard OR (the #6154 generic-plan regression)", name)
		}
	}
}
