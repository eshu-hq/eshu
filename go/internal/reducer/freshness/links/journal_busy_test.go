// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package links

import (
	"context"
	"strings"
	"testing"

	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// busyJournal is a fakeJournal whose pass skips while busy is true, as the
// store's Journal does when a projector Ack holds the journal key shared or
// another replica holds it exclusive.
type busyJournal struct {
	fakeJournal
	busy bool
}

func (j *busyJournal) Journal(ctx context.Context, backfill int) (store.JournalResult, error) {
	if j.busy {
		j.journalCalls++
		return store.JournalResult{Skipped: true}, nil
	}
	return j.fakeJournal.Journal(ctx, backfill)
}

// TestSkippedJournalPassIsCountedAndLinkingContinues proves a skipped
// journal pass (#7127 PR-3b, review F1) is an operator signal:
// retries_total{reason="journal_busy"} counts it and an INFO line names it.
// The cycle still links the rows already journaled and counts no link retry,
// so a busy key does not slow the drain.
func TestSkippedJournalPassIsCountedAndLinkingContinues(t *testing.T) {
	linker := &fakeLinker{calls: map[string]int{}, scripts: map[string][]linkStep{"a": {linkedStep(store.LinkKindRoot)}}}
	r, reader, logs := observedRunner(t, linker, "a")
	r.Journal = &busyJournal{fakeJournal: fakeJournal{scopes: []string{"a"}}, busy: true}
	result, err := r.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !result.Journal.Skipped || result.Linked != 1 || result.Retries != 0 {
		t.Fatalf("cycle = %+v, want a skipped pass, one link, and no link retry", result)
	}
	if got := counterPoints(t, reader, "eshu_dp_changed_since_link_retries_total", "reason"); got[string(store.RetryJournalBusy)] != 1 {
		t.Fatalf("retries_total by reason = %v, want journal_busy 1", got)
	}
	if levels := logLevels(t, logs, "changed-since journal pass skipped"); len(levels) != 1 || levels[0] != "INFO" {
		t.Fatalf("skip log levels = %v, want one INFO line:\n%s", levels, logs.String())
	}
	if !strings.Contains(logs.String(), `"journal_skipped":true`) {
		t.Fatalf("no log line carries journal_skipped=true:\n%s", logs.String())
	}
}

// TestRanJournalPassIsNotCountedAsBusy proves a pass that ran adds nothing
// to retries_total{reason="journal_busy"} and logs no skip.
func TestRanJournalPassIsNotCountedAsBusy(t *testing.T) {
	linker := &fakeLinker{calls: map[string]int{}, scripts: map[string][]linkStep{"a": {linkedStep(store.LinkKindRoot)}}}
	r, reader, logs := observedRunner(t, linker, "a")
	if _, err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got := counterPoints(t, reader, "eshu_dp_changed_since_link_retries_total", "reason"); got[string(store.RetryJournalBusy)] != 0 {
		t.Fatalf("retries_total by reason = %v, want no journal_busy", got)
	}
	if levels := logLevels(t, logs, "changed-since journal pass"); len(levels) != 0 {
		t.Fatalf("journal pass log lines = %v, want none for a pass that ran:\n%s", levels, logs.String())
	}
}

// TestSkipStreakLogsOnceThenOnResume proves the skip log does not flood an
// idle runner: a streak of skips counts every skip but logs one line, and
// the first pass that runs after it logs one resume line with the streak.
func TestSkipStreakLogsOnceThenOnResume(t *testing.T) {
	r, reader, logs := observedRunner(t, &fakeLinker{calls: map[string]int{}})
	journal := &busyJournal{busy: true}
	r.Journal = journal
	for range 3 {
		if _, err := r.RunOnce(context.Background()); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
	}
	journal.busy = false
	if _, err := r.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got := counterPoints(t, reader, "eshu_dp_changed_since_link_retries_total", "reason"); got[string(store.RetryJournalBusy)] != 3 {
		t.Fatalf("retries_total by reason = %v, want journal_busy 3", got)
	}
	if levels := logLevels(t, logs, "changed-since journal pass skipped"); len(levels) != 1 {
		t.Fatalf("skip log lines = %v, want one for the streak:\n%s", levels, logs.String())
	}
	if levels := logLevels(t, logs, "changed-since journal pass resumed"); len(levels) != 1 || levels[0] != "INFO" {
		t.Fatalf("resume log lines = %v, want one INFO line:\n%s", levels, logs.String())
	}
	if !strings.Contains(logs.String(), `"skipped_cycles":3`) {
		t.Fatalf("resume line does not carry the streak length:\n%s", logs.String())
	}
}
