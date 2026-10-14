// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped_test

import (
	"context"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/search/unscoped"
)

// entityPolicyClass is one token class of the #7009 entity ruling (round 1,
// 4.3) with the outcome the content_entities figures must give it. The
// elapsed and overrun bands are the fake clock's, not deployed walls.
type entityPolicyClass struct {
	name          string
	corpus        func() *fakeCorpus
	budget        time.Duration // zero means unscoped.DefaultBudget
	limit         int
	outcome       string // exact, exhausted or partial
	rows          int
	tailRan       bool
	tailCancelled bool
	endedIn       string // the search.ended_in span attribute
	minMS, maxMS  int64  // elapsed band
	maxOverrunMS  int64  // overrun ceiling; a partial also checks minOverrunMS
	minOverrunMS  int64
	largeDoc      bool   // partial reason budget_exceeded_on_large_document
	pattern       string // search pattern; empty means "needle", which has a trigram
	reason        string // partial reason when it is neither of the two above
}

// entityPolicyClasses are the S1 classes at page size 10
// unless a row says otherwise. Seeded REDs are recorded in the #7009 PR-B
// evidence note; the comment on each guard row names its mutation.
func entityPolicyClasses() []entityPolicyClass {
	at := func(tail time.Duration, positions ...int) func() *fakeCorpus {
		return func() *fakeCorpus { return entityPolicyCorpus(tail, positions...) }
	}
	large := func(tail time.Duration, positions ...int) func() *fakeCorpus {
		return func() *fakeCorpus { return withEntityLargeValue(entityPolicyCorpus(tail, positions...)) }
	}
	decode := decodePositions()
	return []entityPolicyClass{
		// Common dense class: limits 10, 50 and 100 stay exact in one call.
		{
			name: "decode-like at limit 10", corpus: large(1128*ms, decode...), limit: 10,
			outcome: "exact", rows: 10, endedIn: "continuation", maxMS: 20,
		},
		{
			name: "decode-like at limit 50", corpus: large(1128*ms, decode...), limit: 50,
			outcome: "exact", rows: 50, endedIn: "continuation", minMS: 60, maxMS: 100,
		},
		{
			name: "decode-like at limit 100", corpus: large(1128*ms, decode...), limit: 100,
			outcome: "exact", rows: 100, endedIn: "continuation", minMS: 130, maxMS: 160,
		},
		// R2.2 was the one disclosed trade: a partial of 149 rows plus one
		// resume. Since #8002 the density-gated extension fills the page in
		// one call: the last ordered window holds ~48 matches, so the gate
		// predicts the 201st inside the extension allowance and the tail
		// never runs.
		{
			name: "decode-like at limit 200 (R2.2 exact via extension)", corpus: large(1128*ms, decode...), limit: 200,
			outcome: "exact", rows: 200, endedIn: "extension",
			minMS: 200, maxMS: 300,
		},
		{
			name: "a-like at limit 200", corpus: at(3700*ms, aPositions...), limit: 200,
			outcome: "exact", rows: 200, endedIn: "probe", maxMS: 10,
		},
		{
			name: "db_-like at limit 200", corpus: at(3700*ms, dbPositions...), limit: 200,
			outcome: "exact", rows: 200, endedIn: "continuation", maxMS: 20,
		},
		// Rare bitmap classes exact under the tail cap (RA-1). The file split
		// cancels both tails.
		{
			name: "process_entries-like", corpus: large(462*ms, processEntriesPositions...), limit: 10,
			outcome: "exhausted", rows: 2, tailRan: true, endedIn: "tail", minMS: 600, maxMS: 650,
		},
		{
			name: "FeaturesTab-like", corpus: at(400*ms, featuresTabPositions...), limit: 10,
			outcome: "exhausted", rows: 4, tailRan: true, endedIn: "tail", minMS: 540, maxMS: 590,
		},
		// R2.3 row (i) and the from_event-like class: at the default budget
		// the tail timeout (638 ms) is outside the 13.1 MB block, so the
		// cancel lag stays small. Seeded RED: a 120 ms tail cap with a 50 ms
		// floor puts the timeout inside the block. An 80 ms cap also fails the
		// row, but by skipping the tail, which is under the 150 ms floor.
		{
			name: "from_event-like, 13.1 MB value, default budget (R2.3 i)", corpus: large(1236*ms, fromEventPositions...), limit: 10,
			outcome: "partial", rows: 0, tailRan: true, tailCancelled: true, endedIn: "tail",
			minMS: 790, maxMS: 810, maxOverrunMS: 10,
		},
		// R2.3 row (ii-a): at 160 ms the tail timeout is 128 ms, inside the
		// block; the cancel lands at 170 ms, 42 ms late, under the 100 ms
		// large-document threshold. Seeded RED: a 40 ms threshold.
		{
			name: "from_event-like, 13.1 MB value, 160 ms budget (R2.3 ii-a)", corpus: large(1236*ms, fromEventPositions...),
			budget: 160 * ms, limit: 10, outcome: "partial", rows: 0, tailRan: true, tailCancelled: true, endedIn: "tail",
			minMS: 190, maxMS: 205, minOverrunMS: 40, maxOverrunMS: 45,
		},
		// R2.3 row (ii-b): at the 100 ms minimum budget a 40 ms probe leaves
		// a 60 ms tail timeout; the cancel lands at 170 ms, 110 ms late.
		// Seeded RED: the walk never reports the large-document reason.
		{
			name: "from_event-like, 13.1 MB value, 100 ms budget, slow probe (R2.3 ii-b)",
			corpus: func() *fakeCorpus {
				return withEntityProbeCost(withEntityLargeValue(entityPolicyCorpus(1236*ms, fromEventPositions...)), 1000, 40*ms)
			},
			budget: unscoped.MinBudget, limit: 10, outcome: "partial", rows: 0, tailRan: true, tailCancelled: true, endedIn: "tail",
			minMS: 205, maxMS: 215, minOverrunMS: 105, maxOverrunMS: 115, largeDoc: true,
		},
		// Selective classes: the tail answers them. Seeded RED for the
		// selective long identifier: a 0.25 x budget tail cap (200 ms).
		{
			name: "getApplicationContext-like (selective)", corpus: at(226*ms, selectivePositions...), limit: 10,
			outcome: "exact", rows: 10, tailRan: true, endedIn: "tail", minMS: 370, maxMS: 410,
		},
		{
			name: "showImage-like", corpus: at(139*ms, showImagePositions...), limit: 10,
			outcome: "exact", rows: 10, tailRan: true, endedIn: "tail", minMS: 285, maxMS: 320,
		},
		{
			name: "checkPrerequisites-like", corpus: at(24*ms, checkPrerequisitesPositions...), limit: 10,
			outcome: "exact", rows: 10, tailRan: true, endedIn: "tail", minMS: 170, maxMS: 200,
		},
		{
			name: "createNewVersion-like", corpus: at(37*ms, createNewVersionPositions...), limit: 10,
			outcome: "exact", rows: 10, tailRan: true, endedIn: "tail", minMS: 185, maxMS: 215,
		},
		{
			name: "zero-match", corpus: at(4 * ms), limit: 10,
			outcome: "exhausted", rows: 0, tailRan: true, endedIn: "tail", minMS: 155, maxMS: 180,
		},
		// No-trigram class with rare matches. The S1 tail measurement showed a
		// 3.7 s parallel scan cancelled at the tail cap, 45 ms late. Since #8002 a
		// pattern with no trigram skips the tail, so the class is pinned on
		// the pattern "db_": the whole remaining budget walks the key order.
		// Seeded RED: removing the patternHasTrigram branch in walk.run runs
		// the tail again and turns both rows red.
		{
			name: "no-trigram rare, match inside the key-order walk (#8002)", pattern: "db_",
			corpus: at(3700*ms, 80_000), limit: 10,
			outcome: "exhausted", rows: 1, endedIn: "tail_skipped_no_trigram", minMS: 430, maxMS: 470,
		},
		// The same class when the match lies past what a short budget walks:
		// a partial that names the limit instead of a cancelled tail.
		{
			name: "no-trigram rare, match past the budgeted walk (#8002)", pattern: "db_",
			corpus: at(3700*ms, 80_000), budget: 150 * ms, limit: 10,
			outcome: "partial", rows: 0, endedIn: "tail_skipped_no_trigram", minMS: 130, maxMS: 150,
			maxOverrunMS: 10, reason: querycontract.SearchPartialPatternHasNoTrigram,
		},
		// Rule 1 guard against skipping the tail after a match. Seeded RED:
		// "skip the tail when a match was seen" turns it partial.
		{
			name: "SYN early-match-selective", corpus: at(300*ms, 150, 40_000, 80_000), limit: 10,
			outcome: "exhausted", rows: 3, tailRan: true, endedIn: "tail", minMS: 445, maxMS: 480,
		},
		{
			name: "SYN zero-match-mid", corpus: at(270 * ms), limit: 10,
			outcome: "exhausted", rows: 0, tailRan: true, endedIn: "tail", minMS: 415, maxMS: 450,
		},
		{
			name: "SYN zero-match-common", corpus: large(1260 * ms), limit: 10,
			outcome: "partial", rows: 0, tailRan: true, tailCancelled: true, endedIn: "tail",
			minMS: 790, maxMS: 810, maxOverrunMS: 10,
		},
		// Rule 1 guard on tail room. Seeded RED: a 0.60 x budget tail cap
		// (480 ms) cancels the 500 ms tail.
		{
			name: "SYN half-early-rare", corpus: at(500*ms, halfEarlyRarePositions...), limit: 10,
			outcome: "exact", rows: 10, tailRan: true, endedIn: "tail", minMS: 645, maxMS: 680,
		},
	}
}

// TestEntityPolicyClasses pins the content_entities figures (S1 triple A)
// against every class of the entity ruling, with no cursor.
func TestEntityPolicyClasses(t *testing.T) {
	t.Parallel()

	for _, tc := range entityPolicyClasses() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			corpus := tc.corpus()
			got := runEntityPolicyFor(t, corpus, tc.pattern, tc.budget, tc.limit, querycontract.SearchCursor{})
			page := got.page
			outcome := "exact"
			switch {
			case page.Partial != nil:
				outcome = "partial"
			case !page.More:
				outcome = "exhausted"
			}
			if outcome != tc.outcome || len(page.Hits) != tc.rows || got.tailRan != tc.tailRan ||
				got.tailCancelled != tc.tailCancelled || got.endedIn != tc.endedIn {
				t.Fatalf("got %s %d rows tail_ran=%v tail_cancelled=%v ended_in=%s at %d ms; want %s %d rows tail_ran=%v tail_cancelled=%v ended_in=%s",
					outcome, len(page.Hits), got.tailRan, got.tailCancelled, got.endedIn, got.elapsedMS,
					tc.outcome, tc.rows, tc.tailRan, tc.tailCancelled, tc.endedIn)
			}
			if want := matchEntityKeys(corpus); !equalKeys(entityKeys(page.Hits), want[:len(page.Hits)]) {
				t.Fatalf("entities are not the ordered match prefix")
			}
			if got.elapsedMS < tc.minMS || got.elapsedMS > tc.maxMS {
				t.Fatalf("elapsed = %d ms, want %d..%d ms", got.elapsedMS, tc.minMS, tc.maxMS)
			}
			if got.elapsedMS > 1000 {
				t.Fatalf("elapsed = %d ms, over the 1.0 s bar (rule 3)", got.elapsedMS)
			}
			if page.Partial == nil {
				return
			}
			p := page.Partial
			if !p.Progressed {
				t.Fatalf("Progressed = false, want true (rule 2): %+v", p)
			}
			if p.OverrunMS < tc.minOverrunMS || p.OverrunMS > tc.maxOverrunMS {
				t.Fatalf("OverrunMS = %d, want %d..%d", p.OverrunMS, tc.minOverrunMS, tc.maxOverrunMS)
			}
			wantReason := querycontract.SearchPartialCandidateBudgetExceeded
			if tc.largeDoc {
				wantReason = querycontract.SearchPartialBudgetExceededOnLargeDocument
			}
			if tc.reason != "" {
				wantReason = tc.reason
			}
			if p.Reason != wantReason {
				t.Fatalf("Reason = %q, want %q (overrun %d ms)", p.Reason, wantReason, p.OverrunMS)
			}
		})
	}
}

// R2.2 was a partial of 149 rows plus one resume. Since #8002 the first
// call is exact: the density-gated extension fills the page and proves the
// 201st, so no resume is needed.
func TestEntityPolicyDecodeAtLimit200ExactInOneCall(t *testing.T) {
	t.Parallel()

	corpus := withEntityLargeValue(entityPolicyCorpus(1128*ms, decodePositions()...))
	got := runEntityPolicy(t, corpus, 0, 200, querycontract.SearchCursor{})
	if got.page.Partial != nil {
		t.Fatalf("Partial = %+v, want an exact page", got.page.Partial)
	}
	if len(got.page.Hits) != 200 {
		t.Fatalf("rows = %d, want 200", len(got.page.Hits))
	}
	if !got.page.More {
		t.Fatal("More = false, want the 201st match proven")
	}
	if got.tailRan {
		t.Fatal("tail ran, want the extension to fill the page without it")
	}
	if got.endedIn != "extension" {
		t.Fatalf("ended_in = %q, want extension", got.endedIn)
	}
	if got.elapsedMS > 1000 {
		t.Fatalf("elapsed = %d ms, over the 1.0 s bar", got.elapsedMS)
	}
	if want := matchEntityKeys(corpus)[:200]; !equalKeys(entityKeys(got.page.Hits), want) {
		t.Fatalf("one call returned %d keys, want the first 200 matches", len(got.page.Hits))
	}
}

// Rule 5 for the from_event-like class: each call of the resume chain is a
// progressed partial at or under 1.0 s until the page is exact. In this
// scaled key space the 11th match is reached on the second call (the QA
// model names 17 calls over the full table in S1).
func TestEntityPolicyFromEventChainEndsExact(t *testing.T) {
	t.Parallel()

	corpus := withEntityLargeValue(entityPolicyCorpus(1236*ms, fromEventPositions...))
	cursor := querycontract.SearchCursor{}
	var gathered []string
	calls := 0
	for {
		calls++
		if calls > 5 {
			t.Fatalf("no exact page after %d calls", calls-1)
		}
		got := runEntityPolicy(t, corpus, 0, 10-len(gathered), cursor)
		if got.elapsedMS > 1000 {
			t.Fatalf("call %d: elapsed = %d ms, over the 1.0 s bar", calls, got.elapsedMS)
		}
		gathered = append(gathered, entityKeys(got.page.Hits)...)
		if got.page.Partial == nil {
			break
		}
		if !got.page.Partial.Progressed {
			t.Fatalf("call %d: Progressed = false, the chain would repeat itself", calls)
		}
		cursor = got.page.Partial.Cursor
	}
	if calls != 2 {
		t.Fatalf("chain length = %d calls, want 2", calls)
	}
	if want := matchEntityKeys(corpus)[:10]; !equalKeys(gathered, want) {
		t.Fatalf("chain gathered %v, want %v", gathered, want)
	}
}

// entityPolicyRun is what one policy call returned and how its walk ended.
type entityPolicyRun struct {
	page                   unscoped.EntityPage
	elapsedMS              int64
	tailRan, tailCancelled bool
	endedIn                string
}

// runEntityPolicy runs one entity search on a fresh fake clock and reads the
// tail phases from the statement log and ended_in from the span.
func runEntityPolicy(t *testing.T, corpus *fakeCorpus, budget time.Duration, limit int, cursor querycontract.SearchCursor) entityPolicyRun {
	t.Helper()
	return runEntityPolicyFor(t, corpus, "", budget, limit, cursor)
}

// runEntityPolicyFor is runEntityPolicy with a search pattern. An empty
// pattern is "needle", which carries a trigram; a pattern with no
// three-character alphanumeric run, such as "db_", takes the #8002 skip.
func runEntityPolicyFor(t *testing.T, corpus *fakeCorpus, pattern string, budget time.Duration, limit int, cursor querycontract.SearchCursor) entityPolicyRun {
	t.Helper()
	if pattern == "" {
		pattern = "needle"
	}
	spans := tracetest.NewSpanRecorder()
	clock := newFakeClock()
	fdb := newFakeDB(corpus, clock)
	searcher := &unscoped.Searcher{
		Store: fdb, Budget: budget, Now: clock.Now, Logger: quietLogger,
		Tracer: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("policy"),
	}
	start := clock.Now()
	page, err := searcher.SearchEntities(context.Background(), pattern, limit, 0, cursor)
	if err != nil {
		t.Fatalf("SearchEntities() error = %v", err)
	}
	run := entityPolicyRun{page: page, elapsedMS: clock.Now().Sub(start).Milliseconds()}
	for i, label := range fdb.tx.log {
		if label == "entity_tail" {
			run.tailRan = true
			run.tailCancelled = i+1 < len(fdb.tx.log) && fdb.tx.log[i+1] == "rollback_to"
		}
	}
	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	for _, kv := range ended[0].Attributes() {
		if kv.Key == "search.ended_in" {
			run.endedIn = kv.Value.AsString()
		}
	}
	return run
}
