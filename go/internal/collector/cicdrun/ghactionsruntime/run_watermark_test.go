// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun/runwatermark"
)

// This file holds the pure detection-logic tests for the #5429 cross-cycle
// run-watermark gap. See source_watermark_test.go for the NextClaimed
// end-to-end wiring tests (Store load/save, warning emission, telemetry).

// TestDetectRunBackfillGapFiresWhenWatermarkOlderThanTruncatedWindowFloor is
// the RED->GREEN proof for #5429: with a watermark from a prior cycle
// (LastRunID=1000), a truncated fetched window whose OLDEST run (3000) is
// newer than the watermark means every run between 1000 and 3000 was never
// fetched by either cycle. detectRunBackfillGap must report true.
func TestDetectRunBackfillGapFiresWhenWatermarkOlderThanTruncatedWindowFloor(t *testing.T) {
	t.Parallel()

	page := RunPage{
		Truncated: true,
		Snapshots: []RunSnapshot{
			minimalRunSnapshot("5000"),
			minimalRunSnapshot("4000"),
			minimalRunSnapshot("3000"),
		},
	}
	watermark := runwatermark.Watermark{LastRunID: "1000"}

	gap, err := detectRunBackfillGap(page, watermark, true)
	if err != nil {
		t.Fatalf("detectRunBackfillGap() error = %v, want nil", err)
	}
	if !gap {
		t.Fatal("detectRunBackfillGap() = false, want true (watermark 1000 < window floor 3000, truncated)")
	}
}

// TestDetectRunBackfillGapDoesNotFireWhenWindowCoversWatermark proves the
// steady-state non-gap case: the fetched window's oldest run (1000) is
// exactly the watermark, so nothing was skipped even though the page is
// truncated (more runs may exist further back, but those were already
// covered by a prior cycle -- or don't need to be, since #5429 only cares
// about runs NEWER than the last observed one).
func TestDetectRunBackfillGapDoesNotFireWhenWindowCoversWatermark(t *testing.T) {
	t.Parallel()

	page := RunPage{
		Truncated: true,
		Snapshots: []RunSnapshot{
			minimalRunSnapshot("3000"),
			minimalRunSnapshot("2000"),
			minimalRunSnapshot("1000"),
		},
	}
	watermark := runwatermark.Watermark{LastRunID: "1000"}

	gap, err := detectRunBackfillGap(page, watermark, true)
	if err != nil {
		t.Fatalf("detectRunBackfillGap() error = %v, want nil", err)
	}
	if gap {
		t.Fatal("detectRunBackfillGap() = true, want false (window floor == watermark, nothing skipped)")
	}
}

// TestDetectRunBackfillGapDoesNotFireWithoutPriorWatermark proves the
// first-ever-claim case: with no prior watermark there is nothing to compare
// against, so no gap claim can be made.
func TestDetectRunBackfillGapDoesNotFireWithoutPriorWatermark(t *testing.T) {
	t.Parallel()

	page := RunPage{
		Truncated: true,
		Snapshots: []RunSnapshot{minimalRunSnapshot("3000"), minimalRunSnapshot("1000")},
	}

	gap, err := detectRunBackfillGap(page, runwatermark.Watermark{}, false)
	if err != nil {
		t.Fatalf("detectRunBackfillGap() error = %v, want nil", err)
	}
	if gap {
		t.Fatal("detectRunBackfillGap() = true, want false (no prior watermark to compare against)")
	}
}

// TestDetectRunBackfillGapDoesNotFireWhenPageNotTruncated proves that an
// UNtruncated page (the provider reports no more runs exist beyond the
// window) can never be a gap, even if the window floor happens to be newer
// than a stale watermark: an untruncated page means every currently existing
// run was fetched, so there is nothing left un-fetched between them.
func TestDetectRunBackfillGapDoesNotFireWhenPageNotTruncated(t *testing.T) {
	t.Parallel()

	page := RunPage{
		Truncated: false,
		Snapshots: []RunSnapshot{minimalRunSnapshot("3000"), minimalRunSnapshot("2500")},
	}
	watermark := runwatermark.Watermark{LastRunID: "1000"}

	gap, err := detectRunBackfillGap(page, watermark, true)
	if err != nil {
		t.Fatalf("detectRunBackfillGap() error = %v, want nil", err)
	}
	if gap {
		t.Fatal("detectRunBackfillGap() = true, want false (page not truncated: nothing left to fetch)")
	}
}

func TestDetectRunBackfillGapWithEmptyWindowIsSafe(t *testing.T) {
	t.Parallel()

	gap, err := detectRunBackfillGap(RunPage{Truncated: true}, runwatermark.Watermark{LastRunID: "1000"}, true)
	if err != nil {
		t.Fatalf("detectRunBackfillGap() error = %v, want nil", err)
	}
	if gap {
		t.Fatal("detectRunBackfillGap() = true, want false for an empty fetched window")
	}
}

func TestWindowNewestRunIDReturnsFirstSnapshot(t *testing.T) {
	t.Parallel()

	page := RunPage{Snapshots: []RunSnapshot{minimalRunSnapshot("3000"), minimalRunSnapshot("1000")}}
	got, err := windowNewestRunID(page)
	if err != nil {
		t.Fatalf("windowNewestRunID() error = %v, want nil", err)
	}
	if got != "3000" {
		t.Fatalf("windowNewestRunID() = %q, want %q", got, "3000")
	}
}

func TestWindowNewestRunIDEmptyWindowReturnsEmptyString(t *testing.T) {
	t.Parallel()

	got, err := windowNewestRunID(RunPage{})
	if err != nil {
		t.Fatalf("windowNewestRunID() error = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("windowNewestRunID() = %q, want empty string for an empty window", got)
	}
}

func TestAttachBackfillGapWarningAppendsReasonToNewestRun(t *testing.T) {
	t.Parallel()

	snapshots := []RunSnapshot{minimalRunSnapshot("3000"), minimalRunSnapshot("1000")}
	got := attachBackfillGapWarning(snapshots, true)
	if len(got) != 2 {
		t.Fatalf("attachBackfillGapWarning() len = %d, want 2", len(got))
	}
	if len(got[0].Warnings) != 1 {
		t.Fatalf("newest snapshot Warnings = %v, want exactly 1 entry", got[0].Warnings)
	}
	if reason := got[0].Warnings[0]["reason"]; reason != "runs_backfill_gap" {
		t.Fatalf("warning reason = %v, want %q", reason, "runs_backfill_gap")
	}
	if len(got[1].Warnings) != 0 {
		t.Fatalf("older snapshot Warnings = %v, want none (only the newest run carries the gap warning)", got[1].Warnings)
	}
	// The input slice must not be mutated.
	if len(snapshots[0].Warnings) != 0 {
		t.Fatalf("input snapshots mutated: Warnings = %v, want untouched", snapshots[0].Warnings)
	}
}

func TestAttachBackfillGapWarningNoOpWhenNoGap(t *testing.T) {
	t.Parallel()

	snapshots := []RunSnapshot{minimalRunSnapshot("3000")}
	got := attachBackfillGapWarning(snapshots, false)
	if len(got[0].Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none when gapDetected is false", got[0].Warnings)
	}
}
