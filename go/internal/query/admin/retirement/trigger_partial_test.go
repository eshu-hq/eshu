// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retirement

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/admin/audit"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// twoRepoInput builds a two-repository operator request: the partial
// mark-failure path needs a marked repository ahead of the failing one.
func twoRepoInput() TriggerRequest {
	return TriggerRequest{
		Retirements: []TriggerRetirement{
			{RetirementID: "rr_part01", RepoID: "PARTITION:part-repo-a", ScopeID: "part-scope-a"},
			{RetirementID: "rr_part02", RepoID: "PARTITION:part-repo-b", ScopeID: "part-scope-b"},
		},
		Reason:         "deselected repositories",
		ActorClass:     "operator",
		IdempotencyKey: "key-8602-partial",
	}
}

// TestTriggerCountsTerminalOnlyForMarkedReposOnPartialMarkFailure is the
// #8626 P2 regression: when the mark loop fails on the second repository,
// the never-attempted repository must not gain a terminal count. Only the
// marked repository carries accepted plus exactly one terminal, so the
// funnel still adds up.
func TestTriggerCountsTerminalOnlyForMarkedReposOnPartialMarkFailure(t *testing.T) {
	inst, reader := countingInstruments(t)
	boom := errors.New("retirement insert: connection refused")
	trigger := &Trigger{
		Marker: &scriptedMarker{
			next:      pgstorage.RetirementRecord{RetirementID: "rr_part01", State: pgstorage.RetirementStatePending},
			failRepos: map[string]error{"PARTITION:part-repo-b": boom},
		},
		Fencer:      &scriptedFencer{},
		Purger:      &scriptedPurger{},
		Instruments: inst,
	}
	if _, err := trigger.TriggerRetirement(context.Background(), twoRepoInput()); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the mark error", err)
	}
	counts := outcomeCounts(t, reader)
	if got, want := counts[TriggerOutcomeAccepted], int64(1); got != want {
		t.Fatalf("accepted = %d, want %d: only the marked repository counts", got, want)
	}
	if got, want := counts[TriggerOutcomeFailed], int64(1); got != want {
		t.Fatalf("failed = %d, want %d: the never-attempted repository stays counter-silent", got, want)
	}
	if got := terminalCounts(counts); got != counts[TriggerOutcomeAccepted] {
		t.Fatalf("terminals = %d, accepted = %d: funnel diverged on the partial-failure path",
			got, counts[TriggerOutcomeAccepted])
	}
}

// TestTriggerCountsTerminalOnlyForMarkedReposOnMarkConflict pins the same
// rule for the conflict arm: a mid-loop ErrRetirementAlreadyOpen counts
// refused only for repositories this call actually marked.
func TestTriggerCountsTerminalOnlyForMarkedReposOnMarkConflict(t *testing.T) {
	inst, reader := countingInstruments(t)
	trigger := &Trigger{
		Marker: &scriptedMarker{
			next: pgstorage.RetirementRecord{RetirementID: "rr_part01", State: pgstorage.RetirementStatePending},
			failRepos: map[string]error{
				"PARTITION:part-repo-b": pgstorage.ErrRetirementAlreadyOpen,
			},
		},
		Fencer:      &scriptedFencer{},
		Purger:      &scriptedPurger{},
		Instruments: inst,
	}
	if _, err := trigger.TriggerRetirement(context.Background(), twoRepoInput()); !errors.Is(err, pgstorage.ErrRetirementAlreadyOpen) {
		t.Fatalf("err = %v, want the mark conflict", err)
	}
	counts := outcomeCounts(t, reader)
	if got, want := counts[TriggerOutcomeAccepted], int64(1); got != want {
		t.Fatalf("accepted = %d, want %d", got, want)
	}
	if got, want := counts[TriggerOutcomeRefused], int64(1); got != want {
		t.Fatalf("refused = %d, want %d: the never-attempted repository stays counter-silent", got, want)
	}
	if got := terminalCounts(counts); got != counts[TriggerOutcomeAccepted] {
		t.Fatalf("terminals = %d, accepted = %d: funnel diverged on the conflict path",
			got, counts[TriggerOutcomeAccepted])
	}
}

// TestTriggerPartialMarkConflictReturnsPerRepoOutcomes is the #8614 T1
// regression: a two-repository retire whose second mark conflicts returns
// the refused terminal for both repositories in request order — the marked
// one (counted) and the failing one (counter-silent) — and never reaches
// the fence or the purge.
func TestTriggerPartialMarkConflictReturnsPerRepoOutcomes(t *testing.T) {
	inst, reader := countingInstruments(t)
	fencer := &scriptedFencer{}
	purger := &scriptedPurger{}
	trigger := &Trigger{
		Marker: &scriptedMarker{
			next: pgstorage.RetirementRecord{RetirementID: "rr_part01", State: pgstorage.RetirementStatePending},
			failRepos: map[string]error{
				"PARTITION:part-repo-b": pgstorage.ErrRetirementAlreadyOpen,
			},
		},
		Fencer:      fencer,
		Purger:      purger,
		Instruments: inst,
	}
	result, err := trigger.TriggerRetirement(context.Background(), twoRepoInput())
	if !errors.Is(err, pgstorage.ErrRetirementAlreadyOpen) {
		t.Fatalf("err = %v, want the mark conflict", err)
	}
	if len(fencer.calls) != 0 {
		t.Fatalf("fence calls = %d, want 0: the mark failure stops the request", len(fencer.calls))
	}
	if len(purger.calls) != 0 {
		t.Fatalf("purge calls = %d, want 0: the mark failure stops the request", len(purger.calls))
	}
	if result.Duplicate {
		t.Fatal("Duplicate = true, want false: no row resumed on this attempt")
	}
	want := []RepoOutcome{
		{RetirementID: "rr_part01", RepoID: "PARTITION:part-repo-a", ScopeID: "part-scope-a", Outcome: TriggerOutcomeRefused},
		{RetirementID: "rr_part02", RepoID: "PARTITION:part-repo-b", ScopeID: "part-scope-b", Outcome: TriggerOutcomeRefused},
	}
	if len(result.Repos) != len(want) {
		t.Fatalf("repos = %+v, want both repositories refused in request order", result.Repos)
	}
	for i, wantOutcome := range want {
		if result.Repos[i] != wantOutcome {
			t.Fatalf("repos[%d] = %+v, want %+v", i, result.Repos[i], wantOutcome)
		}
	}
	counts := outcomeCounts(t, reader)
	if got, wantCount := counts[TriggerOutcomeAccepted], int64(1); got != wantCount {
		t.Fatalf("accepted = %d, want %d", got, wantCount)
	}
	if got, wantCount := counts[TriggerOutcomeRefused], int64(1); got != wantCount {
		t.Fatalf("refused = %d, want %d", got, wantCount)
	}
}

// TestTriggerPartialMarkErrorReturnsPerRepoOutcomes is the #8614 T2
// regression: a two-repository retire whose second mark errors returns the
// failed terminal for both repositories in request order — the marked one
// (counted) and the failing one (counter-silent) — and never reaches the
// fence or the purge.
func TestTriggerPartialMarkErrorReturnsPerRepoOutcomes(t *testing.T) {
	inst, reader := countingInstruments(t)
	boom := errors.New("retirement insert: connection refused")
	fencer := &scriptedFencer{}
	purger := &scriptedPurger{}
	trigger := &Trigger{
		Marker: &scriptedMarker{
			next:      pgstorage.RetirementRecord{RetirementID: "rr_part01", State: pgstorage.RetirementStatePending},
			failRepos: map[string]error{"PARTITION:part-repo-b": boom},
		},
		Fencer:      fencer,
		Purger:      purger,
		Instruments: inst,
	}
	result, err := trigger.TriggerRetirement(context.Background(), twoRepoInput())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the mark error", err)
	}
	if len(fencer.calls) != 0 {
		t.Fatalf("fence calls = %d, want 0: the mark failure stops the request", len(fencer.calls))
	}
	if len(purger.calls) != 0 {
		t.Fatalf("purge calls = %d, want 0: the mark failure stops the request", len(purger.calls))
	}
	want := []RepoOutcome{
		{RetirementID: "rr_part01", RepoID: "PARTITION:part-repo-a", ScopeID: "part-scope-a", Outcome: TriggerOutcomeFailed},
		{RetirementID: "rr_part02", RepoID: "PARTITION:part-repo-b", ScopeID: "part-scope-b", Outcome: TriggerOutcomeFailed},
	}
	if len(result.Repos) != len(want) {
		t.Fatalf("repos = %+v, want both repositories failed in request order", result.Repos)
	}
	for i, wantOutcome := range want {
		if result.Repos[i] != wantOutcome {
			t.Fatalf("repos[%d] = %+v, want %+v", i, result.Repos[i], wantOutcome)
		}
	}
	counts := outcomeCounts(t, reader)
	if got, wantCount := counts[TriggerOutcomeAccepted], int64(1); got != wantCount {
		t.Fatalf("accepted = %d, want %d", got, wantCount)
	}
	if got, wantCount := counts[TriggerOutcomeFailed], int64(1); got != wantCount {
		t.Fatalf("failed = %d, want %d", got, wantCount)
	}
}

// TestTriggerFirstMarkFailureLeavesZeroCounts is the #8614 boundary
// regression: when the mark loop fails on the first repository, nothing is
// marked, so no count fires at all — the failing repository carries the
// terminal counter-silent and the remainder is not_attempted.
func TestTriggerFirstMarkFailureLeavesZeroCounts(t *testing.T) {
	inst, reader := countingInstruments(t)
	boom := errors.New("retirement insert: connection refused")
	fencer := &scriptedFencer{}
	purger := &scriptedPurger{}
	trigger := &Trigger{
		Marker: &scriptedMarker{
			failRepos: map[string]error{"PARTITION:part-repo-a": boom},
		},
		Fencer:      fencer,
		Purger:      purger,
		Instruments: inst,
	}
	result, err := trigger.TriggerRetirement(context.Background(), twoRepoInput())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the mark error", err)
	}
	if len(fencer.calls) != 0 || len(purger.calls) != 0 {
		t.Fatal("fence or purge ran after a mark failure")
	}
	want := []RepoOutcome{
		{RetirementID: "rr_part01", RepoID: "PARTITION:part-repo-a", ScopeID: "part-scope-a", Outcome: TriggerOutcomeFailed},
		{RetirementID: "rr_part02", RepoID: "PARTITION:part-repo-b", ScopeID: "part-scope-b", Outcome: TriggerOutcomeNotAttempted},
	}
	if len(result.Repos) != len(want) {
		t.Fatalf("repos = %+v, want failing then not_attempted", result.Repos)
	}
	for i, wantOutcome := range want {
		if result.Repos[i] != wantOutcome {
			t.Fatalf("repos[%d] = %+v, want %+v", i, result.Repos[i], wantOutcome)
		}
	}
	if counts := outcomeCounts(t, reader); len(counts) != 0 {
		t.Fatalf("counts = %v, want zero counts: nothing was marked", counts)
	}
}

// threeRepoInput builds a three-repository operator request: the #8614 T3
// path fails on the middle repository, so the third is never attempted.
func threeRepoInput() TriggerRequest {
	return TriggerRequest{
		Retirements: []TriggerRetirement{
			{RetirementID: "rr_req3a", RepoID: "PARTITION:mark3-repo-a", ScopeID: "mark3-scope-a"},
			{RetirementID: "rr_req3b", RepoID: "PARTITION:mark3-repo-b", ScopeID: "mark3-scope-b"},
			{RetirementID: "rr_req3c", RepoID: "PARTITION:mark3-repo-c", ScopeID: "mark3-scope-c"},
		},
		Reason:         "deselected repositories",
		ActorClass:     "operator",
		ActorIDHash:    audit.IdentityHash("test-operator"),
		IdempotencyKey: "key-8614-partial-3",
	}
}

// TestTriggerNeverAttemptedRepoIsResponseOnlyNotAttempted is the #8614 T3
// regression: when the mark loop fails on the middle repository of three,
// the third carries the response-only not_attempted outcome — never counted
// and never audited — while the marked and failing repositories carry the
// counted terminal.
func TestTriggerNeverAttemptedRepoIsResponseOnlyNotAttempted(t *testing.T) {
	inst, reader := countingInstruments(t)
	recorder := &testutil.FakeGovernanceAuditAppender{}
	boom := errors.New("retirement insert: connection refused")
	fencer := &scriptedFencer{}
	purger := &scriptedPurger{}
	trigger := &Trigger{
		Marker: &scriptedMarker{
			next:      pgstorage.RetirementRecord{RetirementID: "rr_row3a", State: pgstorage.RetirementStatePending},
			failRepos: map[string]error{"PARTITION:mark3-repo-b": boom},
		},
		Fencer:      fencer,
		Purger:      purger,
		Instruments: inst,
		Audit:       recorder,
	}
	result, err := trigger.TriggerRetirement(context.Background(), threeRepoInput())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the mark error", err)
	}
	if len(fencer.calls) != 0 || len(purger.calls) != 0 {
		t.Fatal("fence or purge ran after a mark failure")
	}
	if len(result.Repos) != 3 {
		t.Fatalf("repos = %+v, want all three repositories in request order", result.Repos)
	}
	// The marked repository names its open row and carries the counted
	// failed terminal, exactly as the counter recorded it.
	if got := result.Repos[0]; got.RetirementID != "rr_row3a" || got.Outcome != TriggerOutcomeFailed {
		t.Fatalf("repos[0] = %+v, want the open row failed", got)
	}
	if got := result.Repos[1]; got.RetirementID != "rr_req3b" || got.Outcome != TriggerOutcomeFailed {
		t.Fatalf("repos[1] = %+v, want the failing repository failed", got)
	}
	if got := result.Repos[2]; got.RetirementID != "rr_req3c" || got.Outcome != TriggerOutcomeNotAttempted {
		t.Fatalf("repos[2] = %+v, want the never-attempted repository not_attempted", got)
	}
	counts := outcomeCounts(t, reader)
	if len(counts) != 2 || counts[TriggerOutcomeAccepted] != 1 || counts[TriggerOutcomeFailed] != 1 {
		t.Fatalf("counts = %v, want exactly accepted 1 and failed 1: not_attempted is response-only", counts)
	}
	for i, ev := range recorder.Events {
		if _, err := governanceaudit.NormalizeEvent(ev); err != nil {
			t.Fatalf("audit event %d invalid: %v (%+v)", i, err, ev)
		}
	}
	if len(recorder.Events) != 1 {
		t.Fatalf("audit events = %+v, want only the marked repository's accepted event", recorder.Events)
	}
	untouchedHash := audit.IdentityHash("mark3-scope-c")
	for _, ev := range recorder.Events {
		if ev.ScopeIDHash == untouchedHash {
			t.Fatalf("audit event names the never-attempted repository: %+v", ev)
		}
	}
}
