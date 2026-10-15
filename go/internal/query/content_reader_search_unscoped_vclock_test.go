// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/search/unscoped"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The phase decisions of the unscoped walk (continuation cap, tail floor,
// tail timeout) are functions of Searcher.Now alone. The live proofs inject a
// virtual clock and charge each window and tail statement a fixed virtual
// cost, so which phases run depends on the fixture and the cost list, never
// on how fast the host executes the real SQL (#8141). The SQL work, the rows
// it returns and the span attributes stay real.

const (
	// virtualBudget is the work budget every virtual walk runs under: the
	// shipped default, so the phase caps are the figures the contract states.
	virtualBudget = unscoped.DefaultBudget
	// virtualStepRows is the window size the per-window cost is stated for. A
	// window of n rows is charged cost*n/virtualStepRows, so the probe (200
	// rows) costs less than a continuation step (500 rows) as it does in
	// production.
	virtualStepRows = 500
	// virtualTailCost is what the trigram tail is charged. It is well under the
	// tail timeout (tail floor 150 ms at the default budget), so a virtual walk
	// never sees a cancelled tail; the real cancel path has its own live proof.
	virtualTailCost = 50 * time.Millisecond
	// realStatementTimeout replaces the walk's statement_timeout so the server
	// never cancels on real elapsed time: only the virtual clock decides phases.
	realStatementTimeout = "60000ms"
)

// virtualWindowCosts are the virtual costs of one 500-row key-ordered window
// that searchLiveUntil tries, in order, until one exercises the wanted phases.
// With the file shape's figures at the default budget (continuation cap
// 440 ms, 12,000-row fixture in 24 windows) any cost above about 18 ms keeps
// the continuation from covering the table, which is what runs the tail; the
// list spans the bands where two or more continuation steps and then the tail
// fit inside the budget.
var virtualWindowCosts = []time.Duration{
	50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond,
	25 * time.Millisecond, 400 * time.Millisecond,
}

// virtualClock is the only clock a virtual walk sees.
type virtualClock struct{ now time.Time }

func newVirtualClock() *virtualClock { return &virtualClock{now: time.Unix(1_700_000_000, 0)} }

func (c *virtualClock) Now() time.Time { return c.now }

func (c *virtualClock) advance(d time.Duration) { c.now = c.now.Add(d) }

// virtualStore wraps the real read store: every transaction it opens runs real
// SQL and advances the virtual clock by the cost of the bounded statements.
type virtualStore struct {
	inner      unscoped.Beginner
	clock      *virtualClock
	windowCost time.Duration
}

// BeginReadOnlySnapshot implements unscoped.Beginner.
func (s *virtualStore) BeginReadOnlySnapshot(ctx context.Context) (db.ReadTransaction, error) {
	tx, err := s.inner.BeginReadOnlySnapshot(ctx)
	if err != nil {
		return nil, err
	}
	statements := unscoped.Statements()
	return &virtualTx{
		ReadTransaction: tx,
		store:           s,
		windows:         map[string]bool{statements.StepFirst: true, statements.StepAfter: true},
		tails:           map[string]bool{statements.TailFirst: true, statements.TailAfter: true},
	}, nil
}

// virtualTx charges virtual time for the window and tail statements and keeps
// the server from cancelling on real time.
type virtualTx struct {
	db.ReadTransaction
	store   *virtualStore
	windows map[string]bool
	tails   map[string]bool
}

// QueryContext runs the statement for real, then advances the virtual clock.
func (tx *virtualTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	switch {
	case strings.Contains(query, "set_config('statement_timeout'") && len(args) > 0:
		args = append(append([]any{}, args[:len(args)-1]...), realStatementTimeout)
	case tx.windows[query]:
		// A window statement binds (execMode, pattern, size, asked, ...).
		size := int64(virtualStepRows)
		if len(args) > 2 {
			if n, ok := args[2].(int64); ok {
				size = n
			}
		}
		defer tx.store.clock.advance(time.Duration(int64(tx.store.windowCost) * size / virtualStepRows))
	case tx.tails[query]:
		defer tx.store.clock.advance(virtualTailCost)
	}
	return tx.ReadTransaction.QueryContext(ctx, query, args...)
}

// liveWalk is one live search with the span attributes that say which phases ran.
type liveWalk struct {
	page querycontract.FileSearchPage
	// budget is the virtual cost of one 500-row window the walk ran under, the
	// one dimension that varies between attempts (the walk's own budget is the
	// constant virtualBudget). The proofs' failure messages print it as the
	// budget, so a failure names the attempt that produced it.
	budget time.Duration
	steps  int64
	tail   bool
	cancel bool
}

// searchLive runs a search under the virtual clock at one virtual window cost
// and records its phases.
func searchLive(t *testing.T, ctx context.Context, db *sql.DB, cost time.Duration, token string, limit int) liveWalk {
	t.Helper()
	clock := newVirtualClock()
	spans := tracetest.NewSpanRecorder()
	searcher := &unscoped.Searcher{
		Store:  &virtualStore{inner: postgres.NewSQLReadStore(db), clock: clock, windowCost: cost},
		Budget: virtualBudget,
		Now:    clock.Now,
		Tracer: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("test"),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	page, err := searcher.Search(ctx, token, limit, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("Search(virtual window cost %d ms) error = %v", cost.Milliseconds(), err)
	}
	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	attrs := map[string]attribute.Value{}
	for _, kv := range ended[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value
	}
	return liveWalk{
		page:   page,
		budget: cost,
		steps:  attrs["search.continuation_steps"].AsInt64(),
		tail:   attrs["search.tail_ran"].AsBool(),
		cancel: attrs["search.tail_cancelled"].AsBool(),
	}
}

// searchLiveUntil runs the search at a bounded list of virtual window costs
// until accept holds for the phases that ran. The phase decisions depend on the
// virtual clock only, so the first cost that exercises the wanted phases is the
// same on every host. Rows and the look-ahead flag must be exact at every cost;
// only the choice of the cost that exercises the wanted phases is searched,
// and failing to find one is a failure, never a skip.
func searchLiveUntil(
	t *testing.T, ctx context.Context, db *sql.DB, token string, limit int,
	check func(walk liveWalk),
	accept func(walk liveWalk) bool,
) liveWalk {
	t.Helper()
	return searchLiveUntilCosts(t, ctx, db, virtualWindowCosts, token, limit, check, accept)
}

// searchLiveUntilCosts is searchLiveUntil over an explicit cost list.
func searchLiveUntilCosts(
	t *testing.T, ctx context.Context, db *sql.DB, costs []time.Duration, token string, limit int,
	check func(walk liveWalk),
	accept func(walk liveWalk) bool,
) liveWalk {
	t.Helper()
	var tried []string
	for _, cost := range costs {
		walk := searchLive(t, ctx, db, cost, token, limit)
		t.Logf("virtual window cost %d ms: steps=%d tail=%v cancelled=%v partial=%v more=%v",
			cost.Milliseconds(), walk.steps, walk.tail, walk.cancel, walk.page.Partial != nil, walk.page.More)
		check(walk)
		if accept(walk) {
			t.Logf("virtual window cost %d ms exercised the wanted phases: steps=%d tail=%v cancelled=%v partial=%v more=%v",
				cost.Milliseconds(), walk.steps, walk.tail, walk.cancel, walk.page.Partial != nil, walk.page.More)
			return walk
		}
		tried = append(tried, fmt.Sprintf("%dms(steps=%d tail=%v cancelled=%v)", cost.Milliseconds(), walk.steps, walk.tail, walk.cancel))
	}
	t.Fatalf("no virtual window cost exercised the wanted phases; tried %s", strings.Join(tried, " "))
	return liveWalk{}
}
