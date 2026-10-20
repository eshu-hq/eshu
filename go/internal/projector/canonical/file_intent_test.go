// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonical

// Regression tests for eshu-hq/eshu#8657: the writer-side conditional
// retract needs the build's file intent (live vs tombstoned file-fact
// counts) carried on the materialization. The writer sees only the
// materialization, never the input facts, so a file-less build (zero live,
// zero tombstoned) over prior File nodes must be distinguishable there from
// a tombstone-driven mass delete (zero live, some tombstoned), which the
// unbounded retract is meant to carry out.

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSummarizeFileIntentCountsLiveAndTombstoned(t *testing.T) {
	t.Parallel()

	input := []facts.Envelope{
		filelessGuardRepositoryFact(),
		filelessGuardFileFact("live-1", false),
		filelessGuardFileFact("live-2", false),
		filelessGuardFileFact("tomb-1", true),
	}

	got := summarizeFileIntent(input)
	want := FileIntentSummary{LiveFileFacts: 2, TombstonedFileFacts: 1}
	if got != want {
		t.Fatalf("summarizeFileIntent() = %+v, want %+v", got, want)
	}
}

func TestSummarizeFileIntentZeroShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input []facts.Envelope
	}{
		{"nil", nil},
		{"empty", []facts.Envelope{}},
		{"repository only", []facts.Envelope{filelessGuardRepositoryFact()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := summarizeFileIntent(tc.input); got != (FileIntentSummary{}) {
				t.Fatalf("summarizeFileIntent() = %+v, want zero summary", got)
			}
		})
	}
}

// The builder carries the summary onto the materialization for both shapes
// the writer must tell apart: a file-less build and a tombstone-driven
// mass delete (mirrors TestBuildMaterializationAllTombstoneFilesStillSucceeds).
func TestBuildMaterializationCarriesFileIntent(t *testing.T) {
	t.Parallel()

	fileless, _, err := BuildMaterialization(
		filelessGuardScope(), testGeneration(),
		[]facts.Envelope{filelessGuardRepositoryFact()},
	)
	if err != nil {
		t.Fatalf("BuildMaterialization() error = %v, want nil; clean zero-facts builds succeed by design", err)
	}
	if fileless.FileIntent != (FileIntentSummary{}) {
		t.Fatalf("file-less build FileIntent = %+v, want zero summary", fileless.FileIntent)
	}

	massDelete, _, err := BuildMaterialization(
		filelessGuardScope(), testGeneration(),
		[]facts.Envelope{
			filelessGuardRepositoryFact(),
			filelessGuardFileFact("file-deleted-1", true),
			filelessGuardFileFact("file-deleted-2", true),
		},
	)
	if err != nil {
		t.Fatalf("BuildMaterialization() error = %v, want nil; all-tombstone is a legitimate delete", err)
	}
	want := FileIntentSummary{TombstonedFileFacts: 2}
	if massDelete.FileIntent != want {
		t.Fatalf("mass-delete build FileIntent = %+v, want %+v", massDelete.FileIntent, want)
	}
}
