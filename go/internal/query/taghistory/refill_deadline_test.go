// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package taghistory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// refillDeadlineFakeGraph is a minimal querycontract.GraphQuery that records
// the ctx of every call RefillScopedPage's loop makes, and can block one call
// until ctx is done. The blocking branch mirrors neo4j_read_policy.go's own
// graphReadResult mapping -- check querycontract.IsBoundedGraphReadDeadline,
// return the wrapped querycontract.ErrGraphReadDeadline sentinel when it is
// the graph-read policy's own budget that fired -- so this fake models the
// real GraphQuery boundary a production Neo4jReader presents, not a shortcut
// invented for the test.
type refillDeadlineFakeGraph struct {
	t *testing.T

	mu          sync.Mutex
	calls       int
	deadlines   []time.Time
	sawDeadline []bool
	blockOnCall int // 1-based index of the call to block on ctx.Done(); 0 disables blocking
}

func (f *refillDeadlineFakeGraph) Run(ctx context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()

	if f.blockOnCall != 0 && n == f.blockOnCall {
		<-ctx.Done()
		if querycontract.IsBoundedGraphReadDeadline(ctx) {
			return nil, querycontract.ErrGraphReadDeadline
		}
		return nil, ctx.Err()
	}

	deadline, ok := ctx.Deadline()
	if !ok {
		f.t.Errorf("call %d: ctx has no deadline; RefillScopedPage's loop must derive one shared "+
			"bounded-read deadline before its first iteration", n)
	}
	f.mu.Lock()
	f.deadlines = append(f.deadlines, deadline)
	f.sawDeadline = append(f.sawDeadline, ok)
	f.mu.Unlock()

	if strings.Contains(cypher, "BUILT_FROM") {
		return nil, nil // no edges: every digest on this window is withheld
	}
	return refillDeadlineWindowRows(n, MaxLimit+1), nil
}

func (*refillDeadlineFakeGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// refillDeadlineWindowRows builds count synthetic raw graph rows for call
// index n, each carrying a distinct resolved_digest that no BUILT_FROM edge
// will resolve -- exactly the fully-withheld page shape that drives the
// refill loop through every one of MaxRefillReads windows.
func refillDeadlineWindowRows(n, count int) []map[string]any {
	rows := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		uid := fmt.Sprintf("call%02d-row%04d", n, i)
		rows = append(rows, map[string]any{
			"tag":               uid,
			"resolved_digest":   "sha256:" + uid,
			"previous_digest":   "",
			"mutated":           false,
			"first_observed_at": fmt.Sprintf("2026-01-01T00:%02d:%04dZ", n%60, i),
			"repository_id":     "",
			"identity_strength": "",
			"uid":               uid,
		})
	}
	return rows
}

func refillDeadlineTestAccess() querycontract.RepositoryAccessFilter {
	return querycontract.RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{"repo-granted"},
		Allowed:              map[string]struct{}{"repo-granted": {}},
	}
}

// TestRefillScopedPageSharesOneDeadlineAcrossReads pins the #6705 finding:
// RefillScopedPage's loop issues up to MaxRefillReads*2 sequential graph reads
// (one keyset window plus one BuiltFrom lookup per window) to answer ONE
// logical scoped request, and Neo4jReader.runRead gives EACH read its own
// fresh DefaultGraphReadTimeout window unless the caller derives one shared
// deadline before the loop starts. Without that shared budget the assembled
// path is bounded at roughly MaxRefillReads*2*DefaultGraphReadTimeout (about
// 80s) instead of the one bounded-read budget a lone graph statement gets.
//
// The ctx this test passes in carries NO deadline of its own (production's
// raw request context does not, per the #6705 claim comment), so any deadline
// the fake observes must come from RefillScopedPage itself. The fake's window
// is fully withheld (no BUILT_FROM edges resolve), which drives the loop
// through every one of MaxRefillReads windows -- 8 total graph reads -- so the
// test asserts every one of them sees a deadline, and that it is the SAME
// instant on every call.
func TestRefillScopedPageSharesOneDeadlineAcrossReads(t *testing.T) {
	t.Parallel()

	fake := &refillDeadlineFakeGraph{t: t}
	page, err := RefillScopedPage(context.Background(), fake, "oci-registry://ghcr.io/eshu-hq/demo:1.0.0", nil, 10, refillDeadlineTestAccess())
	if err != nil {
		t.Fatalf("RefillScopedPage() error = %v, want nil", err)
	}
	if !page.CapReached {
		t.Fatalf("page.CapReached = false, want true (every window is fully withheld, so the scan must hit MaxRefillReads)")
	}
	if got, want := page.Reads, MaxRefillReads; got != want {
		t.Fatalf("page.Reads = %d, want %d", got, want)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if got, want := fake.calls, MaxRefillReads*2; got != want {
		t.Fatalf("graph calls = %d, want %d (one keyset window + one BuiltFrom lookup per window)", got, want)
	}
	if len(fake.deadlines) < 2 {
		t.Fatalf("captured %d deadlines, want at least 2", len(fake.deadlines))
	}
	for i, ok := range fake.sawDeadline {
		if !ok {
			t.Fatalf("call %d: no deadline recorded (see the fake's own t.Errorf above for detail)", i+1)
		}
	}
	first := fake.deadlines[0]
	for i, d := range fake.deadlines[1:] {
		if !d.Equal(first) {
			t.Fatalf("call %d deadline = %s, want the same shared deadline as call 0 (%s) -- "+
				"each read is getting its own fresh window instead of sharing one budget", i+1, d, first)
		}
	}
}

// TestRefillScopedPageReturnsDeadlineErrorWhenBudgetExhaustedMidLoop proves
// the other required half of the #6705 fix: once the shared budget is spent
// partway through the loop -- after at least one window has already been read
// -- RefillScopedPage must return an error that errors.Is
// querycontract.ErrGraphReadDeadline, never a partial page silently presented
// as complete and never a request left to run out the full unbounded 8x
// window.
//
// The test wraps its own ctx with querycontract.WithBoundedGraphReadDeadlineFor
// and a tiny budget before calling RefillScopedPage: WithBoundedGraphReadDeadline
// only ever tightens an existing deadline, so this tiny budget survives
// RefillScopedPage's own wrap and fires fast without waiting out the real
// production window. Blocking on the THIRD call (the second window's keyset
// read, after the first window's two calls already succeeded) proves the
// exhaustion is caught mid-loop, not merely on the very first read.
func TestRefillScopedPageReturnsDeadlineErrorWhenBudgetExhaustedMidLoop(t *testing.T) {
	t.Parallel()

	ctx, cancel := querycontract.WithBoundedGraphReadDeadlineFor(context.Background(), 20*time.Millisecond)
	defer cancel()

	fake := &refillDeadlineFakeGraph{t: t, blockOnCall: 3}
	page, err := RefillScopedPage(ctx, fake, "oci-registry://ghcr.io/eshu-hq/demo:1.0.0", nil, 10, refillDeadlineTestAccess())
	if err == nil {
		t.Fatalf("RefillScopedPage() error = nil, want a deadline error; page = %+v", page)
	}
	if !errors.Is(err, querycontract.ErrGraphReadDeadline) {
		t.Fatalf("error = %v, want errors.Is(err, querycontract.ErrGraphReadDeadline)", err)
	}
	if len(page.Rows) != 0 {
		t.Fatalf("page.Rows = %d rows, want 0 -- an exhausted budget must never be presented as a complete page", len(page.Rows))
	}
	if page.CapReached {
		t.Fatalf("page.CapReached = true, want false -- CapReached means the scan finished its bounded walk, not that it errored")
	}
	if got, want := page.Reads, 1; got != want {
		t.Fatalf("page.Reads = %d, want %d (the first window succeeded before the second's keyset read hit the spent budget)", got, want)
	}
}
