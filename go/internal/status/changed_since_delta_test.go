// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"testing"
	"time"
)

func TestChangedSinceFilterNormalizeClampsSampleLimit(t *testing.T) {
	t.Parallel()

	zero := ChangedSinceFilter{ScopeID: "  s  ", SampleLimit: 0}.Normalize()
	if zero.ScopeID != "s" {
		t.Fatalf("ScopeID = %q, want trimmed", zero.ScopeID)
	}
	if zero.SampleLimit != DefaultChangedSinceSampleLimit {
		t.Fatalf("SampleLimit = %d, want default %d", zero.SampleLimit, DefaultChangedSinceSampleLimit)
	}

	over := ChangedSinceFilter{Repository: "acme/app", SampleLimit: MaxChangedSinceSampleLimit + 100}.Normalize()
	if over.SampleLimit != MaxChangedSinceSampleLimit {
		t.Fatalf("SampleLimit = %d, want clamp %d", over.SampleLimit, MaxChangedSinceSampleLimit)
	}
}

func TestChangedSinceFilterSelectorsAndReferences(t *testing.T) {
	t.Parallel()

	if (ChangedSinceFilter{}).HasScopeSelector() {
		t.Fatal("empty filter should not have a scope selector")
	}
	if !(ChangedSinceFilter{Repository: "acme/app"}).HasScopeSelector() {
		t.Fatal("repository filter should have a scope selector")
	}
	if (ChangedSinceFilter{}).HasSinceReference() {
		t.Fatal("empty filter should not have a since reference")
	}
	if !(ChangedSinceFilter{SinceGenerationID: "gen-1"}).HasSinceReference() {
		t.Fatal("generation filter should have a since reference")
	}
	if !(ChangedSinceFilter{SinceObservedAt: time.Now()}).HasSinceReference() {
		t.Fatal("observed-at filter should have a since reference")
	}
}

func TestChangedSinceCountsTotal(t *testing.T) {
	t.Parallel()

	counts := ChangedSinceCounts{Added: 1, Updated: 2, Unchanged: 3, Retired: 4, Superseded: 5}
	if got, want := counts.Total(), 15; got != want {
		t.Fatalf("Total() = %d, want %d", got, want)
	}
}

func TestChangedSinceClassificationsClosedSet(t *testing.T) {
	t.Parallel()

	if got, want := len(ChangedSinceClassifications), 5; got != want {
		t.Fatalf("classification count = %d, want %d", got, want)
	}
	if ChangedSinceClassifications[0] != ChangedSinceAdded ||
		ChangedSinceClassifications[3] != ChangedSinceRetired {
		t.Fatalf("classification order changed: %v", ChangedSinceClassifications)
	}
}

func TestChangedSinceTimestampZeroIsEmpty(t *testing.T) {
	t.Parallel()

	if got := ChangedSinceTimestamp(time.Time{}); got != "" {
		t.Fatalf("zero timestamp = %q, want empty", got)
	}
	at := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)
	if got, want := ChangedSinceTimestamp(at), "2026-06-09T10:00:00Z"; got != want {
		t.Fatalf("timestamp = %q, want %q", got, want)
	}
}
