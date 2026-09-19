// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossscope

import (
	"fmt"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

const (
	waitTestScope  = "aws:123456789012:us-east-1:iam"
	waitTestDomain = reducercontract.DomainIAMCanPerformMaterialization
)

var (
	waitTestT0    = time.Date(2026, time.September, 19, 9, 0, 0, 0, time.UTC)
	waitTestCycle = waitTestT0.Add(-time.Minute)
)

func waitInput(existing *ReadinessWait, generation string, cycle time.Time, now time.Time, missing ...string) WaitInput {
	in := WaitInput{
		ScopeID:        waitTestScope,
		Domain:         waitTestDomain,
		GenerationID:   generation,
		CycleStartedAt: cycle,
		Missing:        missing,
		Now:            now,
		MaxWait:        30 * time.Minute,
	}
	if existing != nil {
		in.Existing = *existing
		in.Found = true
	}
	return in
}

// committedRow is the ledger row a first evaluation of gen-1 at t0 leaves behind.
func committedRow(missing ...string) ReadinessWait {
	return DecideWait(waitInput(nil, "gen-1", waitTestCycle, waitTestT0, missing...)).Row
}

func TestDecideWaitNoMissingCommitsAndClears(t *testing.T) {
	t.Parallel()
	fresh := DecideWait(waitInput(nil, "gen-1", waitTestCycle, waitTestT0))
	if !fresh.Commit || fresh.Defer || fresh.Clear || fresh.Upsert || fresh.Outcome != "" {
		t.Fatalf("empty missing, no row: got %+v, want commit only", fresh)
	}
	row := committedRow("arn:a")
	withRow := DecideWait(waitInput(&row, "gen-1", waitTestCycle, waitTestT0.Add(time.Minute)))
	if !withRow.Commit || withRow.Defer || !withRow.Clear || withRow.Upsert {
		t.Fatalf("empty missing with row: got %+v, want commit and clear", withRow)
	}
}

func TestDecideWaitFirstEvaluationCommitsThenDefers(t *testing.T) {
	t.Parallel()
	got := DecideWait(waitInput(nil, "gen-1", waitTestCycle, waitTestT0, "arn:b", "arn:a", "arn:a"))
	if !got.Commit || !got.Defer || !got.Upsert || got.Clear || got.Outcome != ReadinessWaitDeferred {
		t.Fatalf("first evaluation: got %+v, want commit, defer, upsert, outcome deferred", got)
	}
	if !got.Row.FirstDeferredAt.Equal(waitTestT0) {
		t.Fatalf("anchor = %v, want %v", got.Row.FirstDeferredAt, waitTestT0)
	}
	if want := []string{"arn:a", "arn:b"}; fmt.Sprint(got.Row.MissingKeys) != fmt.Sprint(want) || got.Row.MissingCount != 2 {
		t.Fatalf("missing keys = %v (count %d), want sorted distinct %v", got.Row.MissingKeys, got.Row.MissingCount, want)
	}
	if got.Row.CommittedGenerationID != "gen-1" || !got.Row.CommittedCycleStartedAt.Equal(waitTestCycle) ||
		got.Row.CommittedFingerprint != got.Row.MissingFingerprint {
		t.Fatalf("committed marker = %+v, want gen-1 at the claim cycle and the current fingerprint", got.Row)
	}
}

func TestDecideWaitPollWithUnchangedMissingNeitherCommitsNorWrites(t *testing.T) {
	t.Parallel()
	row := committedRow("arn:a")
	got := DecideWait(waitInput(&row, "gen-1", waitTestCycle, waitTestT0.Add(time.Minute), "arn:a"))
	if got.Commit || !got.Defer || got.Upsert || got.Outcome != ReadinessWaitDeferred {
		t.Fatalf("unchanged poll: got %+v, want defer with no commit and no ledger write", got)
	}
	if !PollEligible(row, true, "gen-1", waitTestCycle) {
		t.Fatal("PollEligible = false for a committed, unsettled, complete row of this generation and cycle")
	}
}

func TestDecideWaitPollAfterOneKeyResolvesRecommitsOnce(t *testing.T) {
	t.Parallel()
	row := committedRow("arn:a", "arn:b")
	got := DecideWait(waitInput(&row, "gen-1", waitTestCycle, waitTestT0.Add(time.Minute), "arn:b"))
	if !got.Commit || !got.Defer || !got.Upsert {
		t.Fatalf("shrunk missing set: got %+v, want one re-commit then defer", got)
	}
	if !got.Row.FirstDeferredAt.Equal(waitTestT0) {
		t.Fatalf("anchor moved to %v, want it kept at %v", got.Row.FirstDeferredAt, waitTestT0)
	}
	again := DecideWait(waitInput(&got.Row, "gen-1", waitTestCycle, waitTestT0.Add(2*time.Minute), "arn:b"))
	if again.Commit || again.Upsert {
		t.Fatalf("second poll at the new set: got %+v, want no re-commit and no write", again)
	}
}

// TestDecideWaitAnchorSurvivesSupersession is the R2-F1 case: gen-2 replaces
// gen-1 before the bound with the same target still missing. The new
// generation commits at its first claim and keeps gen-1's anchor, so the bound
// fires on wall time since the FIRST defer, not since gen-2's row was created.
func TestDecideWaitAnchorSurvivesSupersession(t *testing.T) {
	t.Parallel()
	row := committedRow("arn:a")
	gen2Cycle := waitTestT0.Add(20 * time.Minute)
	first := DecideWait(waitInput(&row, "gen-2", gen2Cycle, gen2Cycle, "arn:a"))
	if !first.Commit || !first.Defer || !first.Row.FirstDeferredAt.Equal(waitTestT0) {
		t.Fatalf("gen-2 first claim: got %+v, want commit, defer, anchor kept at %v", first, waitTestT0)
	}
	atBound := DecideWait(waitInput(&first.Row, "gen-2", gen2Cycle, waitTestT0.Add(30*time.Minute), "arn:a"))
	if atBound.Commit || atBound.Defer || !atBound.Upsert || atBound.Outcome != ReadinessWaitAbandoned {
		t.Fatalf("at anchor+MaxWait: got %+v, want settle (abandoned) with no re-commit", atBound)
	}
	if !atBound.Row.SettledAt.Equal(waitTestT0.Add(30 * time.Minute)) {
		t.Fatalf("settled_at = %v, want the bound time", atBound.Row.SettledAt)
	}
}

func TestDecideWaitSettledSameSetCommitsWithoutDefer(t *testing.T) {
	t.Parallel()
	row := committedRow("arn:a")
	row.SettledAt = waitTestT0.Add(30 * time.Minute)
	got := DecideWait(waitInput(&row, "gen-3", waitTestT0.Add(time.Hour), waitTestT0.Add(time.Hour), "arn:a"))
	if !got.Commit || got.Defer || got.Upsert || got.Clear || got.Outcome != ReadinessWaitSettledMissing {
		t.Fatalf("settled, same set: got %+v, want commit and succeed as settled_missing", got)
	}
}

func TestDecideWaitSettledChangedSetRestartsAnchor(t *testing.T) {
	t.Parallel()
	row := committedRow("arn:a")
	row.SettledAt = waitTestT0.Add(30 * time.Minute)
	now := waitTestT0.Add(time.Hour)
	got := DecideWait(waitInput(&row, "gen-3", now, now, "arn:a", "arn:c"))
	if !got.Commit || !got.Defer || !got.Upsert || !got.ResetAnchor {
		t.Fatalf("settled, new set: got %+v, want commit, defer, reset anchor", got)
	}
	if !got.Row.FirstDeferredAt.Equal(now) || !got.Row.SettledAt.IsZero() {
		t.Fatalf("restart row = %+v, want anchor %v and settled_at cleared", got.Row, now)
	}
}

func TestDecideWaitReopenedCycleRecommits(t *testing.T) {
	t.Parallel()
	// A graph rebuild or maintenance reopen gives the same generation a new
	// cycle. The ledger's commit marker is for the old cycle, so the edges may
	// be gone: the evaluation must commit again rather than poll.
	row := committedRow("arn:a")
	reopened := waitTestT0.Add(10 * time.Minute)
	if PollEligible(row, true, "gen-1", reopened) {
		t.Fatal("PollEligible = true across a reopened cycle, want false")
	}
	got := DecideWait(waitInput(&row, "gen-1", reopened, reopened, "arn:a"))
	if !got.Commit {
		t.Fatalf("reopened cycle: got %+v, want a re-commit", got)
	}
}

func TestDecideWaitCapsStoredKeysButFingerprintsTheFullSet(t *testing.T) {
	t.Parallel()
	missing := make([]string, 0, ReadinessWaitMaxKeys+3)
	for i := 0; i < ReadinessWaitMaxKeys+3; i++ {
		missing = append(missing, fmt.Sprintf("arn:%04d", i))
	}
	got := DecideWait(waitInput(nil, "gen-1", waitTestCycle, waitTestT0, missing...))
	if len(got.Row.MissingKeys) != ReadinessWaitMaxKeys || got.Row.MissingCount != len(missing) {
		t.Fatalf("stored %d keys (count %d), want %d stored and count %d",
			len(got.Row.MissingKeys), got.Row.MissingCount, ReadinessWaitMaxKeys, len(missing))
	}
	if PollEligible(got.Row, true, "gen-1", waitTestCycle) {
		t.Fatal("PollEligible = true for a truncated key set, want the full-evaluation fallback")
	}
	shorter := DecideWait(waitInput(nil, "gen-1", waitTestCycle, waitTestT0, missing[:len(missing)-1]...))
	if shorter.Row.MissingFingerprint == got.Row.MissingFingerprint {
		t.Fatal("fingerprint ignores keys beyond the cap, want it over the full set")
	}
}

func TestDecideWaitZeroMaxWaitUsesDefaultBound(t *testing.T) {
	t.Parallel()
	in := waitInput(nil, "gen-1", waitTestCycle, waitTestT0, "arn:a")
	in.MaxWait = 0
	row := DecideWait(in).Row
	in = waitInput(&row, "gen-1", waitTestCycle, waitTestT0.Add(ProducerReadinessMaxWait-time.Second), "arn:a")
	in.MaxWait = 0
	if got := DecideWait(in); !got.Defer {
		t.Fatalf("zero MaxWait inside the default bound: got %+v, want defer", got)
	}
}
