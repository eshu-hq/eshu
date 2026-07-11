// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSourceOrderKeyOrdersByObservedAtThenFactID(t *testing.T) {
	t.Parallel()
	older := sourceOrderKey(facts.Envelope{
		ObservedAt: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		FactID:     "fact-z",
	})
	newer := sourceOrderKey(facts.Envelope{
		ObservedAt: time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC),
		FactID:     "fact-a",
	})
	if newer <= older {
		t.Fatalf("sourceOrderKey() newer=%q should sort greater than older=%q regardless of fact id", newer, older)
	}
}

func TestSourceOrderKeyTiesBreakOnFactID(t *testing.T) {
	t.Parallel()
	sameInstant := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	low := sourceOrderKey(facts.Envelope{ObservedAt: sameInstant, FactID: "fact-aaa"})
	high := sourceOrderKey(facts.Envelope{ObservedAt: sameInstant, FactID: "fact-bbb"})
	if high <= low {
		t.Fatalf("sourceOrderKey() equal timestamps should tie-break on FactID: high=%q, low=%q", high, low)
	}
}

func TestSourceOrderKeyFixedWidthTimestampNeverMisordersAcrossFractionalDigitCounts(t *testing.T) {
	t.Parallel()
	whole := sourceOrderKey(facts.Envelope{
		ObservedAt: time.Date(2026, time.January, 1, 0, 0, 5, 0, time.UTC),
		FactID:     "f",
	})
	fractional := sourceOrderKey(facts.Envelope{
		ObservedAt: time.Date(2026, time.January, 1, 0, 0, 5, 500000000, time.UTC),
		FactID:     "f",
	})
	if fractional <= whole {
		t.Fatalf("sourceOrderKey() fractional=%q should sort greater than whole=%q", fractional, whole)
	}
	if len(whole) != len(fractional) {
		t.Fatalf("sourceOrderKey() timestamps must be fixed-width: whole=%q (%d) fractional=%q (%d)", whole, len(whole), fractional, len(fractional))
	}
}

func TestSourceOrderKeyNormalizesNonUTCToUTC(t *testing.T) {
	t.Parallel()
	loc := time.FixedZone("test-offset", -5*60*60)
	local := time.Date(2026, time.January, 1, 12, 0, 0, 0, loc)
	gotLocal := sourceOrderKey(facts.Envelope{ObservedAt: local, FactID: "fact"})
	gotUTC := sourceOrderKey(facts.Envelope{ObservedAt: local.UTC(), FactID: "fact"})
	if gotLocal != gotUTC {
		t.Fatalf("sourceOrderKey() must normalize to UTC: local=%q utc=%q", gotLocal, gotUTC)
	}
}

func TestPreferMaxSourceOrderKeyPrefersNilExisting(t *testing.T) {
	t.Parallel()
	if !preferMaxSourceOrderKey(nil, map[string]any{sourceOrderKeyField: "any"}) {
		t.Fatal("preferMaxSourceOrderKey() = false, want true when existing is nil")
	}
}

func TestPreferMaxSourceOrderKeyPrefersGreaterKey(t *testing.T) {
	t.Parallel()
	existing := map[string]any{sourceOrderKeyField: "1000-low"}
	if !preferMaxSourceOrderKey(existing, map[string]any{sourceOrderKeyField: "2000-high"}) {
		t.Fatal("preferMaxSourceOrderKey() = false, want true when candidate key is greater")
	}
	if preferMaxSourceOrderKey(existing, map[string]any{sourceOrderKeyField: "0500-lower"}) {
		t.Fatal("preferMaxSourceOrderKey() = true, want false when candidate key is lower")
	}
}

func TestPreferMaxSourceOrderKeyRejectsEqualKey(t *testing.T) {
	t.Parallel()
	existing := map[string]any{sourceOrderKeyField: "1000-same"}
	if preferMaxSourceOrderKey(existing, map[string]any{sourceOrderKeyField: "1000-same"}) {
		t.Fatal("preferMaxSourceOrderKey() = true, want false for an equal key (existing stays stable)")
	}
}

func TestPreferMaxSourceOrderKeyFailsSafeOnMissingField(t *testing.T) {
	t.Parallel()
	if !preferMaxSourceOrderKey(map[string]any{sourceOrderKeyField: "1000"}, map[string]any{"other": "value"}) {
		t.Fatal("preferMaxSourceOrderKey() = false, want true (fail-safe replace) when candidate is missing the order key")
	}
	if !preferMaxSourceOrderKey(map[string]any{"other": "value"}, map[string]any{sourceOrderKeyField: "1000"}) {
		t.Fatal("preferMaxSourceOrderKey() = false, want true (fail-safe replace) when existing is missing the order key")
	}
}
