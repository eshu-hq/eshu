// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package urlredact

import (
	"strings"
	"testing"
)

// freeTextSentinel is the synthetic credential these cases plant.
const freeTextSentinel = "SENTINELCREDENTIAL"

// TestFreeTextExtraPredicateOnlyWidensTheNameSet is the guard on the one place a
// caller can influence what this walk removes.
//
// The walk takes an extra predicate because internal/reportbundle has a name
// that is not a credential — an inline-content key — and still must not ship. It
// takes it as an ADDITION, OR'd with collector.IsSensitiveKeyName, rather than
// as the whole predicate. A caller handing in a complete predicate could quietly
// stop matching "authorization" and every walk in the repo would disagree about
// what a sensitive key is, which is the drift this package exists to end.
//
// So both directions are asserted: an extra predicate that says no to everything
// cannot narrow the shared set, and one that says yes to a new name does widen
// it.
func TestFreeTextExtraPredicateOnlyWidensTheNameSet(t *testing.T) {
	t.Parallel()

	sharedName := "api_key=" + freeTextSentinel
	extraName := "excerpt=" + freeTextSentinel

	tests := []struct {
		name          string
		alsoSensitive func(string) bool
		text          string
		wantRemoved   bool
	}{
		{
			name:          "nil predicate still removes a shared credential name",
			alsoSensitive: nil,
			text:          sharedName,
			wantRemoved:   true,
		},
		{
			name:          "a predicate that says no to everything cannot narrow the shared set",
			alsoSensitive: func(string) bool { return false },
			text:          sharedName,
			wantRemoved:   true,
		},
		{
			name:          "a nil predicate does not know a caller's own name",
			alsoSensitive: nil,
			text:          extraName,
			wantRemoved:   false,
		},
		{
			name:          "a predicate that says yes widens the set",
			alsoSensitive: func(name string) bool { return strings.EqualFold(name, "excerpt") },
			text:          extraName,
			wantRemoved:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, changed := FreeText(tt.text, tt.alsoSensitive)
			if changed != tt.wantRemoved {
				t.Fatalf("FreeText(%q) removed=%v, want %v (got %q)", tt.text, changed, tt.wantRemoved, got)
			}
			if tt.wantRemoved && strings.Contains(got, freeTextSentinel) {
				t.Fatalf("the credential survived: %q", got)
			}
			if !tt.wantRemoved && got != tt.text {
				t.Fatalf("FreeText rewrote text it reported unchanged: %q", got)
			}
		})
	}
}

// TestFreeTextMarkerTripsNoRuleOfItsOwn is why both callers can re-scan their own
// output. reportbundle.Capture runs Validate over the bundle it just wrote, and a
// first-run evidence artifact is re-rendered from a saved envelope; a marker
// holding an "=" or a ":" would make each of them find something new on a pass
// that should be a no-op.
func TestFreeTextMarkerTripsNoRuleOfItsOwn(t *testing.T) {
	t.Parallel()

	if strings.ContainsAny(FreeTextMarker, "=:%") {
		t.Fatalf("FreeTextMarker %q contains a separator the walk scans for", FreeTextMarker)
	}
	if _, changed := FreeText(FreeTextMarker, nil); changed {
		t.Fatalf("FreeText removed part of its own marker %q", FreeTextMarker)
	}
	once, _ := FreeText("api_key="+freeTextSentinel, nil)
	if again, changed := FreeText(once, nil); changed || again != once {
		t.Fatalf("FreeText is not a fixed point: %q then %q", once, again)
	}
}
