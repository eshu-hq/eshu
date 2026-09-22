// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/changedsince"
)

func TestChangedSinceFilterNormalizeClampsSampleLimit(t *testing.T) {
	t.Parallel()

	zero := changedsince.Filter{ScopeID: "  s  ", SampleLimit: 0}.Normalize()
	if zero.ScopeID != "s" {
		t.Fatalf("ScopeID = %q, want trimmed", zero.ScopeID)
	}
	if zero.SampleLimit != changedsince.DefaultSampleLimit {
		t.Fatalf("SampleLimit = %d, want default %d", zero.SampleLimit, changedsince.DefaultSampleLimit)
	}

	over := changedsince.Filter{Repository: "acme/app", SampleLimit: changedsince.MaxSampleLimit + 100}.Normalize()
	if over.SampleLimit != changedsince.MaxSampleLimit {
		t.Fatalf("SampleLimit = %d, want clamp %d", over.SampleLimit, changedsince.MaxSampleLimit)
	}
}

func TestChangedSinceFilterSelectorsAndReferences(t *testing.T) {
	t.Parallel()

	if (changedsince.Filter{}).HasScopeSelector() {
		t.Fatal("empty filter should not have a scope selector")
	}
	if !(changedsince.Filter{Repository: "acme/app"}).HasScopeSelector() {
		t.Fatal("repository filter should have a scope selector")
	}
	if (changedsince.Filter{}).HasSinceReference() {
		t.Fatal("empty filter should not have a since reference")
	}
	if !(changedsince.Filter{SinceGenerationID: "gen-1"}).HasSinceReference() {
		t.Fatal("generation filter should have a since reference")
	}
	if !(changedsince.Filter{SinceObservedAt: time.Now()}).HasSinceReference() {
		t.Fatal("observed-at filter should have a since reference")
	}
}

func TestChangedSinceCountsTotal(t *testing.T) {
	t.Parallel()

	counts := changedsince.Counts{Added: 1, Updated: 2, Unchanged: 3, Retired: 4, Superseded: 5}
	if got, want := counts.Total(), 15; got != want {
		t.Fatalf("Total() = %d, want %d", got, want)
	}
}

func TestChangedSinceClassificationsClosedSet(t *testing.T) {
	t.Parallel()

	if got, want := len(changedsince.Classifications), 5; got != want {
		t.Fatalf("classification count = %d, want %d", got, want)
	}
	if changedsince.Classifications[0] != changedsince.Added ||
		changedsince.Classifications[3] != changedsince.Retired {
		t.Fatalf("classification order changed: %v", changedsince.Classifications)
	}
}

func TestChangedSinceTimestampZeroIsEmpty(t *testing.T) {
	t.Parallel()

	if got := changedsince.Timestamp(time.Time{}); got != "" {
		t.Fatalf("zero timestamp = %q, want empty", got)
	}
	at := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)
	if got, want := changedsince.Timestamp(at), "2026-06-09T10:00:00Z"; got != want {
		t.Fatalf("timestamp = %q, want %q", got, want)
	}
}
