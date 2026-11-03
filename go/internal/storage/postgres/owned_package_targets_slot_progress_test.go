// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ownedPackageClockStatementSHA256 pins the flag-off statement for the
// vulnerability planner's mode (version-specific, alias expansion). The
// progress-ordered selection is a second rendering: the clock statement every
// instance without the flag runs must stay byte-identical, so rollback by flag
// restores exactly the measured plan. #9622 re-baselined the ecosystem fold to
// the ASCII-only form; the hash below was taken from the statement text dumped
// after that edit, not computed from this code.
const ownedPackageClockStatementSHA256 = "03fa01616bf8f5f7c3eb551a76494292372734903a88d8384f80df82b4f6e3bb"

func TestOwnedPackageClockStatementIsByteIdenticalToTheShippedOne(t *testing.T) {
	t.Parallel()

	sum := sha256.Sum256([]byte(listOwnedPackageDependencyTargetsQuery(true, true)))
	if got := hex.EncodeToString(sum[:]); got != ownedPackageClockStatementSHA256 {
		t.Fatalf("flag-off owned-package statement changed: sha256 %s, want %s (#9776 must not touch the clock statement)",
			got, ownedPackageClockStatementSHA256)
	}
}

// TestOwnedPackageSlotProgressStatementShape is plan-shape T1: the flag-on
// statement shares the measured CTE chain byte for byte up to the slot
// partition, renders the pick CTE once, and filters by the picked virtual
// slots once.
func TestOwnedPackageSlotProgressStatementShape(t *testing.T) {
	t.Parallel()

	for _, versionSpecific := range []bool{true, false} {
		clock := listOwnedPackageDependencyTargetsQuery(versionSpecific, true)
		progress := listOwnedPackageDependencySlotProgressQuery(versionSpecific, true)

		const boundary = "slotted_targets AS (\n"
		clockHead, _, ok := strings.Cut(clock, boundary)
		if !ok {
			t.Fatalf("clock statement has no %q boundary", boundary)
		}
		if !strings.HasPrefix(progress, clockHead+boundary) {
			t.Fatalf("progress statement does not share the clock statement's CTE chain up to the slot partition")
		}
		if got := strings.Count(progress, ownedPackageSlotSelectMarker); got != 1 {
			t.Fatalf("final SELECT marker appears %d times in the progress statement, want 1", got)
		}
		for _, once := range []string{
			"slot_pick AS (",
			"= ANY (slot_pick.picked_slots)",
			"FROM owned_target_slot_progress AS reused",
			"LEFT JOIN owned_target_slot_progress AS progress",
		} {
			if got := strings.Count(progress, once); got != 1 {
				t.Fatalf("progress statement contains %q %d times, want exactly once:\n%s", once, got, progress)
			}
		}
		for _, want := range []string{
			"reused.collector_instance_id = $6",
			"reused.derivation_kind = $7",
			"reused.last_planned_window_start = $8::bigint",
			"ORDER BY COALESCE(progress.last_planned_window_start, -1) ASC, slots.virtual_slot ASC",
			"MOD(key_hash_low24::bigint, $5::bigint) AS virtual_slot_of_key",
			"WHEN window_stats.slot_count > $5::bigint THEN 'clock_fallback'",
			"ORDER BY window_targets.virtual_slot_of_key ASC, window_targets.target_rank ASC\nLIMIT $2",
		} {
			if !strings.Contains(progress, want) {
				t.Fatalf("progress statement missing %q:\n%s", want, progress)
			}
		}
		// P keeps one SQL definition in this rendering too: per target, and on
		// the one-row window stats.
		if got := strings.Count(progress, ownedPackageSlotCountExpression); got != 2 {
			t.Fatalf("slot count expression rendered %d times in the progress statement, want 2", got)
		}
	}
}

func TestParsePickedVirtualSlots(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw     string
		want    []int
		wantErr bool
	}{
		{raw: "", want: nil},
		{raw: "7", want: []int{7}},
		{raw: "3,9,40", want: []int{3, 9, 40}},
		{raw: "3,x", wantErr: true},
		{raw: "-1", wantErr: true},
		{raw: "1024", wantErr: true},
	}
	for _, tc := range cases {
		got, err := parsePickedVirtualSlots(tc.raw)
		if (err != nil) != tc.wantErr {
			t.Fatalf("parsePickedVirtualSlots(%q) error = %v, wantErr %v", tc.raw, err, tc.wantErr)
		}
		if !tc.wantErr && !slices.Equal(got, tc.want) {
			t.Fatalf("parsePickedVirtualSlots(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

// TestOwnedPackageSlotProgressSelectionTreatsTheLookaheadRowAsTruncation pins
// the P3-1 review finding. The read limit is the target limit plus one
// lookahead row, so a window of exactly L+1 rows is not reader-truncated
// (slot_size equals rows_read), yet the derivation drops row L+1 as budget
// exhausted. The slot holding that row must stay oldest instead of being
// recorded as planned.
func TestOwnedPackageSlotProgressSelectionTreatsTheLookaheadRowAsTruncation(t *testing.T) {
	t.Parallel()

	const targetLimit = 4
	columns := slotProgressColumns{
		mode: workflow.SlotSelectionOldest, picked: "3,7", progressRows: 1024,
		oldestWindowStart: sql.NullInt64{Int64: 90, Valid: true},
	}
	rowSlots := []int{3, 3, 3, 7, 7} // five rows: L+1, the last one in slot 7
	window := workflow.OwnedPackageDependencyTargetWindow{SlotSize: 5, RowsRead: 5}

	got, err := ownedPackageSlotProgressSelection(columns, rowSlots, window, 100, targetLimit)
	if err != nil {
		t.Fatalf("ownedPackageSlotProgressSelection() error = %v", err)
	}
	if !got.Truncated || !slices.Equal(got.AdvanceSlots, []int{3}) {
		t.Fatalf("selection = %#v, want Truncated and only slot 3 advanced (slot 7 holds the dropped row)", got)
	}

	within, err := ownedPackageSlotProgressSelection(columns, rowSlots[:4], workflow.OwnedPackageDependencyTargetWindow{SlotSize: 4, RowsRead: 4},
		100, targetLimit)
	if err != nil {
		t.Fatalf("ownedPackageSlotProgressSelection() error = %v", err)
	}
	if within.Truncated || !slices.Equal(within.AdvanceSlots, []int{3, 7}) {
		t.Fatalf("selection at exactly L rows = %#v, want not truncated and both slots advanced", within)
	}
}

// TestOwnedPackageSlotProgressSelectionAgesBySecondsWhateverTheInterval pins the
// second review's P1 (#9776) in the age arithmetic. Both ends of the age are
// window start times in unix seconds, so a row written under a 30 minute
// interval and read under a 1 hour one (or the reverse) reports the true
// elapsed time, and a row newer than the read window clamps to zero instead of
// going negative.
func TestOwnedPackageSlotProgressSelectionAgesBySecondsWhateverTheInterval(t *testing.T) {
	t.Parallel()

	hour := int64(time.Hour.Seconds())
	cases := []struct {
		name         string
		oldestStart  int64
		readStart    int64
		wantAge      time.Duration
		wantOldestAt int64
	}{
		{"oldest row written under a narrower interval", 1000 * hour, 1032 * hour, 32 * time.Hour, 1000 * hour},
		{"oldest row written under a wider interval", 1000 * hour, 1032*hour + hour/2, 32*time.Hour + 30*time.Minute, 1000 * hour},
		{"oldest row opened after the read window", 1032*hour + hour/2, 1032 * hour, 0, 1032*hour + hour/2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			columns := slotProgressColumns{
				mode: workflow.SlotSelectionOldest, picked: "3", progressRows: workflow.OwnedPackageVirtualSlotCount,
				oldestWindowStart: sql.NullInt64{Int64: tc.oldestStart, Valid: true},
			}
			window := workflow.OwnedPackageDependencyTargetWindow{SlotSize: 1, RowsRead: 1}
			got, err := ownedPackageSlotProgressSelection(columns, []int{3}, window, tc.readStart, 0)
			if err != nil {
				t.Fatalf("ownedPackageSlotProgressSelection() error = %v", err)
			}
			if got.OldestSlotAge != tc.wantAge || got.OldestPlannedWindowStartUnix != tc.wantOldestAt {
				t.Fatalf("age=%v oldest=%d, want age=%v oldest=%d", got.OldestSlotAge, got.OldestPlannedWindowStartUnix, tc.wantAge, tc.wantOldestAt)
			}
		})
	}
}
