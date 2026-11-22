// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package snapshot_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/collector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/workflow/snapshot"
)

func sourcePacingRows() []collector.SourcePacingSnapshot {
	return []collector.SourcePacingSnapshot{
		{
			SourceSystem: "git", Slowed: true, Reason: "source_behind", Stuck: true,
			OldestClaimableAge: 2 * time.Hour, ClaimableDepth: 400, EngagedSince: base.Add(-45 * time.Minute),
		},
		{SourceSystem: "npm", OldestClaimableAge: 90 * time.Second, ClaimableDepth: 7},
	}
}

// TestSourcePacingRowsRoundTripThroughTheStoredSummary proves the stored-summary
// path keeps every source_pacing row (#10882 AC6): the slowed source with its
// stuck verdict and engagement time, and the calm source with neither.
func TestSourcePacingRowsRoundTripThroughTheStoredSummary(t *testing.T) {
	t.Parallel()

	snap := fullSnapshot()
	snap.SourcePacing = sourcePacingRows()
	entries, err := snapshot.Encode(snap)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	var stored int
	for _, entry := range entries {
		if entry.Section == "source_pacing" {
			stored++
		}
	}
	if stored != 2 {
		t.Fatalf("stored %d source_pacing entries, want 2", stored)
	}
	got, err := snapshot.Decode(entries)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !reflect.DeepEqual(got.SourcePacing, snap.SourcePacing) {
		t.Fatalf("SourcePacing round trip = %#v, want %#v", got.SourcePacing, snap.SourcePacing)
	}
}

// TestDecodeRejectsMalformedSourcePacingEntries proves a corrupt source_pacing
// row refuses the whole stored snapshot instead of serving a partial one: an
// unknown field, trailing data and an unparseable engagement time each fail.
func TestDecodeRejectsMalformedSourcePacingEntries(t *testing.T) {
	t.Parallel()

	snap := fullSnapshot()
	snap.SourcePacing = sourcePacingRows()
	good, err := snapshot.Encode(snap)
	if err != nil {
		t.Fatal(err)
	}
	mutateFirstPacing := func(edit func(string) string) []summary.Entry {
		entries := append([]summary.Entry(nil), good...)
		for i := range entries {
			if entries[i].Section == "source_pacing" {
				entries[i].JSON = edit(entries[i].JSON)
				break
			}
		}
		return entries
	}
	cases := map[string][]summary.Entry{
		"unknown field": mutateFirstPacing(func(s string) string { return strings.Replace(s, "{", `{"surprise":1,`, 1) }),
		"trailing data": mutateFirstPacing(func(s string) string { return s + ` {}` }),
		"bad engaged_since": mutateFirstPacing(func(s string) string {
			return strings.Replace(s, base.Add(-45*time.Minute).Format(time.RFC3339Nano), "not-a-time", 1)
		}),
	}
	for name, entries := range cases {
		if _, err := snapshot.Decode(entries); err == nil {
			t.Errorf("%s: Decode() error = nil, want an error", name)
		}
	}
}

// TestDecodeRejectsAnUnknownFieldInEveryStoredSection proves no stored section
// is decoded leniently: a field the reader does not know (a newer writer, or
// corruption) refuses the snapshot instead of silently dropping it, for every
// section the encoder writes, source_pacing included.
func TestDecodeRejectsAnUnknownFieldInEveryStoredSection(t *testing.T) {
	t.Parallel()

	snap := fullSnapshot()
	snap.SourcePacing = sourcePacingRows()
	good, err := snapshot.Encode(snap)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for index, entry := range good {
		if seen[entry.Section] {
			continue
		}
		seen[entry.Section] = true
		mutated := append([]summary.Entry(nil), good...)
		mutated[index].JSON = strings.Replace(entry.JSON, "{", `{"surprise":1,`, 1)
		if _, err := snapshot.Decode(mutated); err == nil {
			t.Errorf("section %q: Decode() error = nil with an unknown field, want an error", entry.Section)
		}
	}
	if len(seen) < 6 {
		t.Fatalf("exercised %d sections, want every stored section (%v)", len(seen), seen)
	}
}
