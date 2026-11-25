// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// boundTestActivePage builds one full raw page of active supports rows, fact
// ids base+0000..base+0499, so a drain of several such pages models a hot key
// without a live database.
func boundTestActivePage(base int) [][]any {
	rows := make([][]any, 0, listFactsByKindPageSize)
	for i := 0; i < listFactsByKindPageSize; i++ {
		rows = append(rows, supportsPageRowForTest(
			fmt.Sprintf("fact-%04d", base+i), true,
			`{"repository_id":"hotspot","package_id":"pkg:hot","cve_id":"CVE-HOT","impact_status":"affected_exact"}`))
	}
	return rows
}

// withDrainMaxActiveRows shrinks the package drain cap for one test. The cap is
// a package var (not a const) so tests can model a hot key with three pages
// instead of two hundred; the shipped binary keeps the default. Callers must not run in
// parallel while the cap is shrunk.
func withDrainMaxActiveRows(t *testing.T, max int) {
	t.Helper()
	old := supplyChainImpactSupportsDrainMaxActiveRows
	supplyChainImpactSupportsDrainMaxActiveRows = max
	t.Cleanup(func() { supplyChainImpactSupportsDrainMaxActiveRows = old })
}

// TestSupplyChainImpactSupportsDrainTruncatesOverCapWithResumeCursor is the
// producer-side OOM regression test: an unbounded multi-page drain must stop at
// the active-row cap and report where it stopped, instead of appending every
// active envelope with its full JSON payload into one slice until the engine
// OOMKills (11 OOMKills of the general engine, 7.6GB heap, ~91% under
// encoding/json from drain via page).
func TestSupplyChainImpactSupportsDrainTruncatesOverCapWithResumeCursor(t *testing.T) {
	withDrainMaxActiveRows(t, 2*listFactsByKindPageSize)

	database := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: boundTestActivePage(0)},
		{rows: boundTestActivePage(500)},
		{rows: boundTestActivePage(1000)},
	}}
	drain := newSupplyChainImpactSupportsDrain(nil, "bound-test",
		supplyChainAdvisoriesForReposArms, []string{"hotspot"})

	loaded, stats, err := drain.drain(context.Background(), database)

	var truncated *SupplyChainImpactSupportsDrainTruncatedError
	if !errors.As(err, &truncated) {
		t.Fatalf("drain() error = %v, want *SupplyChainImpactSupportsDrainTruncatedError (the unbounded drain must stop at the cap)", err)
	}
	if loaded != nil {
		t.Fatalf("drain() loaded = %d envelopes, want nil on truncation (the caller must resume, not serve a prefix as complete)", len(loaded))
	}
	// The cap is hit exactly at the end of page 2, so the third full raw page
	// must not be served: the resume cursor is the last delivered fact id and a
	// resume from it re-reads page 3 without loss or duplication.
	if want := fmt.Sprintf("fact-%04d", 2*listFactsByKindPageSize-1); truncated.ResumeCursorFactID != want {
		t.Fatalf("resume cursor = %q, want %q (last delivered fact id)", truncated.ResumeCursorFactID, want)
	}
	if !truncated.Retryable() {
		t.Fatal("truncation must be retryable: the engine defers the hot key instead of OOMKilling on it")
	}
	if got := truncated.FailureClass(); got != "supply_chain_impact_supports_drain_truncated" {
		t.Fatalf("failure class = %q, want the truncated class", got)
	}
	if stats.activeRows != 2*listFactsByKindPageSize || stats.rawRows != 3*listFactsByKindPageSize {
		t.Fatalf("stats = {active %d, raw %d}, want {active %d, raw %d} (delivered rows capped, raw rows read)",
			stats.activeRows, stats.rawRows, 2*listFactsByKindPageSize, 3*listFactsByKindPageSize)
	}
	if !stats.truncated || stats.pages != 3 {
		t.Fatalf("stats = {truncated %v, pages %d}, want {truncated true, pages 3}", stats.truncated, stats.pages)
	}
	// The drain still pages on the last RAW id: page 2 carries the cursor of
	// the end of page 1, page 3 the cursor of the end of page 2.
	var cursors []any
	for _, q := range database.queries {
		if len(q.args) > 1 {
			cursors = append(cursors, q.args[len(q.args)-1])
		}
	}
	if len(cursors) != 2 || cursors[0] != "fact-0499" || cursors[1] != "fact-0999" {
		t.Fatalf("page cursors = %v, want [fact-0499 fact-0999] (last RAW id of each page)", cursors)
	}
}

// TestSupplyChainImpactSupportsDrainServesUnderCapUnchanged pins the other half
// of the bound: a drain under the cap behaves exactly as before (fact_id order,
// active-only, stops on a short raw page), so the cap changes nothing for
// ordinary keys.
func TestSupplyChainImpactSupportsDrainServesUnderCapUnchanged(t *testing.T) {
	withDrainMaxActiveRows(t, 2*listFactsByKindPageSize)

	database := &fakeExecQueryer{queryResponses: []queueFakeRows{
		{rows: boundTestActivePage(0)},
		{rows: [][]any{supportsPageRowForTest("fact-0500", false, "")}},
	}}
	drain := newSupplyChainImpactSupportsDrain(nil, "bound-test",
		supplyChainAdvisoriesForReposArms, []string{"hotspot"})

	loaded, stats, err := drain.drain(context.Background(), database)
	if err != nil {
		t.Fatalf("drain() error = %v, want nil under the cap", err)
	}
	if len(loaded) != listFactsByKindPageSize {
		t.Fatalf("loaded = %d envelopes, want one full page (the inactive raw row is dropped)", len(loaded))
	}
	for i, envelope := range loaded {
		if want := fmt.Sprintf("fact-%04d", i); envelope.FactID != want {
			t.Fatalf("loaded[%d].FactID = %q, want %q (fact_id order)", i, envelope.FactID, want)
		}
		if envelope.FactKind != facts.ReducerSupplyChainImpactFindingFactKind {
			t.Fatalf("loaded[%d].FactKind = %q, want the impact kind (active-only)", i, envelope.FactKind)
		}
	}
	if stats.truncated || stats.failed {
		t.Fatalf("stats = %+v, want neither truncated nor failed", stats)
	}
}
