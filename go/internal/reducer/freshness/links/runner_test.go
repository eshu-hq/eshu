// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package links

import (
	"context"
	"errors"
	"sync"
	"testing"

	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// fakeLinker replays a scripted sequence of LinkNext results per scope and
// records the calls.
type fakeLinker struct {
	mu       sync.Mutex
	scripts  map[string][]linkStep
	calls    map[string]int
	recorded []*store.FailureError
	poison   bool
}

func (f *fakeLinker) RecordFailure(_ context.Context, failure *store.FailureError, _ int) (store.FailureRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recorded = append(f.recorded, failure)
	return store.FailureRecord{Counted: true, Attempts: 1, Poisoned: f.poison}, nil
}

type linkStep struct {
	result store.LinkResult
	err    error
}

func (f *fakeLinker) LinkNext(_ context.Context, scopeID string) (store.LinkResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[scopeID]++
	steps := f.scripts[scopeID]
	if len(steps) == 0 {
		return store.LinkResult{Idle: true, ScopeID: scopeID}, nil
	}
	f.scripts[scopeID] = steps[1:]
	return steps[0].result, steps[0].err
}

type fakeJournal struct {
	scopes        []string
	journalCalls  int
	backfillLimit int
	orphans       []string
	deleted       []string
}

func (f *fakeJournal) Journal(_ context.Context, backfill int) (store.JournalResult, error) {
	f.journalCalls++
	f.backfillLimit = backfill
	return store.JournalResult{SweeperRows: 1}, nil
}

func (f *fakeJournal) BacklogScopes(context.Context, int) ([]string, error) { return f.scopes, nil }
func (f *fakeJournal) Stats(context.Context) (store.LedgerStats, error) {
	return store.LedgerStats{}, nil
}
func (f *fakeJournal) OrphanScopes(context.Context, int) ([]string, error) { return f.orphans, nil }
func (f *fakeJournal) DeleteOrphanScope(_ context.Context, scopeID string) (bool, error) {
	f.deleted = append(f.deleted, scopeID)
	return true, nil
}

func linkedStep(kind store.LinkKind) linkStep {
	return linkStep{result: store.LinkResult{ScopeID: "s", GenerationID: "g", Kind: kind}}
}

func TestRunOnceDrainsScopesAndStopsAScopeOnRetry(t *testing.T) {
	linker := &fakeLinker{calls: map[string]int{}, scripts: map[string][]linkStep{
		"a": {linkedStep(store.LinkKindRoot), linkedStep(store.LinkKindIncremental)},
		"b": {
			{result: store.LinkResult{ScopeID: "b", Kind: store.LinkKindNone, Break: store.BreakOverlayUnproven}},
			{err: &store.RetryError{Reason: store.RetrySlotBusy}},
			linkedStep(store.LinkKindIncremental), // must not be reached this cycle
		},
		"c": {{err: errors.New("boom")}},
	}}
	journal := &fakeJournal{scopes: []string{"a", "b", "c"}, orphans: []string{"gone"}}
	runner := &Runner{Linker: linker, Journal: journal, Config: Config{Workers: 2, BackfillScopesPerCycle: 3}}

	result, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.Linked != 2 || result.Breaks != 1 || result.Retries != 1 || result.Failures != 1 {
		t.Fatalf("RunOnce = %+v, want linked 2, breaks 1, retries 1, failures 1", result)
	}
	if linker.calls["a"] != 3 {
		t.Fatalf("scope a LinkNext calls = %d, want 3 (two links and the idle probe)", linker.calls["a"])
	}
	if linker.calls["b"] != 2 {
		t.Fatalf("scope b LinkNext calls = %d, want 2: a retry must stop the scope for this cycle", linker.calls["b"])
	}
	if journal.journalCalls != 1 || journal.backfillLimit != 3 {
		t.Fatalf("Journal calls = %d with limit %d, want 1 with 3", journal.journalCalls, journal.backfillLimit)
	}
	if len(journal.deleted) != 1 || journal.deleted[0] != "gone" || result.Orphans != 1 {
		t.Fatalf("orphans deleted = %v (%d), want [gone]", journal.deleted, result.Orphans)
	}
}

func TestRunOnceBoundsLinksPerScope(t *testing.T) {
	steps := make([]linkStep, 10)
	for i := range steps {
		steps[i] = linkedStep(store.LinkKindIncremental)
	}
	linker := &fakeLinker{calls: map[string]int{}, scripts: map[string][]linkStep{"a": steps}}
	runner := &Runner{Linker: linker, Journal: &fakeJournal{scopes: []string{"a"}}, Config: Config{MaxLinksPerScope: 4}}
	result, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.Linked != 4 || linker.calls["a"] != 4 {
		t.Fatalf("linked %d with %d calls, want 4 and 4", result.Linked, linker.calls["a"])
	}
}

func TestRunnerRequiresLinkerAndJournal(t *testing.T) {
	if _, err := (&Runner{}).RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce without dependencies succeeded")
	}
}

func TestCountingFailureIsRecordedAndNonCountingIsNot(t *testing.T) {
	failure := &store.FailureError{Class: store.FailureSQLError, ScopeID: "a", ActivationSeq: 7, Err: errors.New("boom")}
	linker := &fakeLinker{calls: map[string]int{}, scripts: map[string][]linkStep{
		"a": {{err: failure}},
		"b": {{err: &store.RetryError{Reason: store.RetrySlotBusy, ScopeID: "b", ActivationSeq: 3}}},
		"c": {{err: errors.New("begin failed")}},
	}}
	runner := &Runner{Linker: linker, Journal: &fakeJournal{scopes: []string{"a", "b", "c"}}}
	result, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(linker.recorded) != 1 || linker.recorded[0] != failure {
		t.Fatalf("recorded failures = %v, want only the counting failure of a", linker.recorded)
	}
	if result.Failures != 2 || result.Retries != 1 || result.Poisoned != 0 {
		t.Fatalf("RunOnce = %+v, want 2 failures (one counted), 1 retry", result)
	}
}

func TestPoisonedFailureCountsAsABreakAndDrainsOn(t *testing.T) {
	failure := &store.FailureError{Class: store.FailureStatementTimeout, ScopeID: "a", ActivationSeq: 1, Err: errors.New("timeout")}
	linker := &fakeLinker{poison: true, calls: map[string]int{}, scripts: map[string][]linkStep{
		"a": {{err: failure}, linkedStep(store.LinkKindIncremental)},
	}}
	runner := &Runner{Linker: linker, Journal: &fakeJournal{scopes: []string{"a"}}}
	result, err := runner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.Poisoned != 1 || result.Breaks != 1 || result.Linked != 1 {
		t.Fatalf("RunOnce = %+v, want one poisoned break then the next activation linked", result)
	}
}
