// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factwrite_test

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

func TestNowUsesTheInjectedClockAndNormalizesToUTC(t *testing.T) {
	t.Parallel()

	// A deliberately non-UTC zone. Fact rows carry observed_at, and readers
	// order rows from different collectors against each other, so a writer that
	// stored a local timestamp would sort wrongly without failing a type check.
	zone := time.FixedZone("UTC+7", 7*60*60)
	fixed := time.Date(2026, time.March, 14, 9, 30, 0, 0, zone)

	got := factwrite.Now(func() time.Time { return fixed })

	if !got.Equal(fixed) {
		t.Fatalf("Now(clock) = %s, want the instant %s", got, fixed)
	}
	if got.Location() != time.UTC {
		t.Fatalf("Now(clock) location = %s, want UTC", got.Location())
	}
}

func TestNowFallsBackToTheWallClockWhenNoClockIsInjected(t *testing.T) {
	t.Parallel()

	before := time.Now().UTC()
	got := factwrite.Now(nil)
	after := time.Now().UTC()

	if got.Before(before) || got.After(after) {
		t.Fatalf("Now(nil) = %s, want an instant within [%s, %s]", got, before, after)
	}
	if got.Location() != time.UTC {
		t.Fatalf("Now(nil) location = %s, want UTC", got.Location())
	}
}

func TestCollectorKindTrimsAndDefaultsBlankToUnknown(t *testing.T) {
	t.Parallel()

	// "unknown" is a stored column value operators group by, so a blank source
	// must collapse to exactly that string rather than to "".
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{name: "plain value passes through", source: "github", want: "github"},
		{name: "surrounding whitespace is trimmed", source: "  github\t", want: "github"},
		{name: "empty becomes unknown", source: "", want: "unknown"},
		{name: "whitespace only becomes unknown", source: "   \t\n ", want: "unknown"},
		{name: "inner whitespace is preserved", source: " aws config ", want: "aws config"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := factwrite.CollectorKind(tc.source); got != tc.want {
				t.Fatalf("CollectorKind(%q) = %q, want %q", tc.source, got, tc.want)
			}
		})
	}
}
