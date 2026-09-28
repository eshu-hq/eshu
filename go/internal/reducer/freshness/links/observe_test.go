// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package links

import (
	"context"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// TestRebaseIsALinkAndAPriorPrunedBreak proves a rebase (arbiter ruling
// arb-7127-3d, C2) reaches operators as both things it is: a committed root
// link (links_total{link_kind=root,outcome=linked}, counted as Linked, so the
// runner keeps draining) and one prior_pruned chain break, with the pruned
// prior on the log line.
func TestRebaseIsALinkAndAPriorPrunedBreak(t *testing.T) {
	linker := &fakeLinker{calls: map[string]int{}, scripts: map[string][]linkStep{
		"s": {{result: store.LinkResult{
			ScopeID: "s", GenerationID: "g1", Kind: store.LinkKindRoot,
			Break: store.BreakPriorPruned, RebasedFrom: "g0", Keys: 7,
		}}},
	}}
	r, reader, logs := observedRunner(t, linker, "s")
	result, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.Linked != 1 || result.Breaks != 0 {
		t.Fatalf("cycle = %+v, want the rebase counted as one link and no break-only outcome", result)
	}
	if got := counterPoints(t, reader, "eshu_dp_changed_since_links_total", "outcome"); got["linked"] != 1 || got["break"] != 0 {
		t.Fatalf("links_total by outcome = %v, want linked 1", got)
	}
	if got := counterPoints(t, reader, "eshu_dp_changed_since_chain_breaks_total", "reason"); got[string(store.BreakPriorPruned)] != 1 {
		t.Fatalf("chain_breaks_total by reason = %v, want prior_pruned 1", got)
	}
	if !strings.Contains(logs.String(), `"rebased_from_generation_id":"g0"`) {
		t.Fatalf("link log line does not name the pruned prior:\n%s", logs.String())
	}
}

// TestGenerationLockTimeoutIsARetryOfItsScopeOnly is W9 of arbiter ruling
// arb-7127-3e-wait for a link: generation_lock_timeout is counted on
// retries_total{reason="generation_lock_timeout"} and on the span, stops its
// scope for the cycle, and other scopes link in the same cycle.
func TestGenerationLockTimeoutIsARetryOfItsScopeOnly(t *testing.T) {
	timeout := &store.RetryError{Reason: store.RetryGenerationLockTimeout, SQLState: "55P03", ScopeID: "a", ActivationSeq: 2}
	linker := &fakeLinker{calls: map[string]int{}, scripts: map[string][]linkStep{
		"a": {{err: timeout}, linkedStep(store.LinkKindIncremental)},
		"b": {linkedStep(store.LinkKindRoot)},
	}}
	r, reader, _ := observedRunner(t, linker, "a", "b")
	spans := tracetest.NewSpanRecorder()
	r.Tracer = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("links-test")
	result, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.Retries != 1 || result.Linked != 1 || linker.calls["a"] != 1 {
		t.Fatalf("cycle = %+v, calls %v; want scope a stopped after its retry and scope b linked", result, linker.calls)
	}
	if got := counterPoints(t, reader, "eshu_dp_changed_since_link_retries_total", "reason"); got[string(store.RetryGenerationLockTimeout)] != 1 {
		t.Fatalf("retries_total by reason = %v, want generation_lock_timeout 1", got)
	}
	found := false
	for _, span := range spans.Ended() {
		for _, kv := range span.Attributes() {
			if kv.Key == "changed_since.retry_reason" && kv.Value.AsString() == string(store.RetryGenerationLockTimeout) {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("no span carries changed_since.retry_reason=generation_lock_timeout")
	}
}

// timeoutJournal is a fakeJournal whose pass gives way on a generation lock.
type timeoutJournal struct{ fakeJournal }

func (j *timeoutJournal) Journal(context.Context, int) (store.JournalResult, error) {
	return store.JournalResult{}, &store.RetryError{Reason: store.RetryGenerationLockTimeout, SQLState: "55P03"}
}

// TestJournalLockTimeoutIsARetryNotACycleFailure is W9 for the journal pass:
// the timeout is counted on the same retries counter and logged with its
// SQLSTATE at WARN, RunOnce returns no error (no bare "cycle failed"), and
// the cycle links nothing after the failed pass.
func TestJournalLockTimeoutIsARetryNotACycleFailure(t *testing.T) {
	linker := &fakeLinker{calls: map[string]int{}, scripts: map[string][]linkStep{"a": {linkedStep(store.LinkKindRoot)}}}
	r, reader, logs := observedRunner(t, linker, "a")
	r.Journal = &timeoutJournal{fakeJournal{scopes: []string{"a"}}}
	result, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce = %v, want the journal timeout reported as a retry, not a cycle failure", err)
	}
	if result.Retries != 1 || linker.calls["a"] != 0 {
		t.Fatalf("cycle = %+v, calls %v; want one retry and no link after the failed pass", result, linker.calls)
	}
	if got := counterPoints(t, reader, "eshu_dp_changed_since_link_retries_total", "reason"); got[string(store.RetryGenerationLockTimeout)] != 1 {
		t.Fatalf("retries_total by reason = %v, want generation_lock_timeout 1", got)
	}
	if !strings.Contains(logs.String(), `"sqlstate":"55P03"`) || !strings.Contains(logs.String(), `"level":"WARN"`) {
		t.Fatalf("journal timeout log does not carry the SQLSTATE at WARN:\n%s", logs.String())
	}
}
