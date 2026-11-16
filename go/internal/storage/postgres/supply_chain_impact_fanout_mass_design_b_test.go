// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strconv"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestHolderMassProbeQueryIsFillToCapBound pins the #10681 design-B probe
// shape (theory lane S2, K=100): the group-members arm counts over
// LIMIT K+1 and reports the cap above K, so the per-id heap fetch stays
// bounded at cold cost (~100-200ms) instead of the uncapped ~26k-row arm.
// It fails until the arm-3 rewrite lands.
func TestHolderMassProbeQueryIsFillToCapBound(t *testing.T) {
	t.Parallel()

	q := HolderMassProbeQuery
	k := int64(supplychaincore.HolderMassProbeFillToCapRows)
	capDigits := strconv.FormatInt(supplychaincore.HolderMassProbeCap, 10)

	fillVerb := "CASE WHEN COUNT(*) > " + strconv.FormatInt(k, 10) +
		" THEN " + capDigits + " ELSE COUNT(*) END"
	if !strings.Contains(q, fillVerb) {
		t.Fatalf("probe lost the fill-to-cap arm-3 verb %q:\n%s", fillVerb, q)
	}
	limitVerb := "LIMIT " + strconv.FormatInt(k+1, 10)
	if !strings.Contains(q, limitVerb) {
		t.Fatalf("probe lost the bounded member-arm fetch %q:\n%s", limitVerb, q)
	}
	if strings.Contains(q, "LIMIT "+capDigits) {
		t.Fatalf("probe still fetches the member arm up to the full cap instead of K+1:\n%s", q)
	}
	if got := strings.Count(q, "LIMIT"); got != 1 {
		t.Fatalf("probe carries %d LIMITs, want 1 (arm-3 only: cheap arms stay exact, never capped without an arbiter call)", got)
	}
	if strings.Contains(q, "EXISTS") {
		t.Fatalf("probe regressed to the rejected S1 EXISTS-to-cap shape, whose planner flips to an unbounded filter scan:\n%s", q)
	}
	if got := strings.Count(q, "LEFT JOIN"); got != 2 {
		t.Fatalf("probe joins %d times, want 2 (cheap-arm aggregate plus the conditional member arm) with zero-fill", got)
	}
}

// TestHolderMassProbeFillToCapDecidesLikeUncapped is the #10681 design-B
// decision-equivalence regression: on hot, cold, and mixed chunks the
// fill-to-cap report must decide exactly like the uncapped S0 report, and
// off those bands it must only overestimate (split-safe bias preserved).
// The maps compared here are the planner's whole input (one mass per id),
// so equal maps decide identically.
func TestHolderMassProbeFillToCapDecidesLikeUncapped(t *testing.T) {
	t.Parallel()

	cap := int64(supplychaincore.HolderMassProbeCap)
	k := int64(supplychaincore.HolderMassProbeFillToCapRows)
	s0Total := func(cheap, member int64) int64 {
		if total := cheap + member; total > cap {
			return cap
		} else {
			return total
		}
	}
	fillMember := func(member int64) int64 {
		if member > k {
			return cap
		}
		return member
	}
	s2Total := func(cheap, member int64) int64 {
		return s0Total(cheap, fillMember(member))
	}

	equivalent := []struct {
		name          string
		cheap, member int64
	}{
		{"zero", 0, 0},
		{"cold cheap and member", 3, 7},
		{"member at K stays exact", 0, k},
		{"cheap at K stays exact", k, 0},
		{"member just hot", 0, cap},
		{"hot member saturates both", 0, 26000},
		{"already-hot cheap short-circuits mid-band member", cap, 5000},
		{"cold total at the hot threshold", 9990, 10},
		{"huge", 0, 1 << 40},
	}
	for _, tc := range equivalent {
		t.Run("equivalent/"+tc.name, func(t *testing.T) {
			t.Parallel()

			if got, want := s2Total(tc.cheap, tc.member), s0Total(tc.cheap, tc.member); got != want {
				t.Fatalf("s2Total(%d, %d) = %d, want s0 %d: the fill moved a decision", tc.cheap, tc.member, got, want)
			}
		})
	}

	mixed := map[string][2]int64{
		"id-hot":   {0, 26000},
		"id-cold":  {3, 7},
		"id-zero":  {0, 0},
		"id-huge":  {0, 1 << 40},
		"id-bound": {9990, 10},
		"id-cheap": {5000, 50},
	}
	s0Map := make(map[string]int64, len(mixed))
	s2Map := make(map[string]int64, len(mixed))
	for id, arms := range mixed {
		s0Map[id] = s0Total(arms[0], arms[1])
		s2Map[id] = s2Total(arms[0], arms[1])
	}
	for id, want := range s0Map {
		if got := s2Map[id]; got != want {
			t.Fatalf("mixed chunk id %q: s2 reports %d, s0 reports %d", id, got, want)
		}
	}

	// The one accepted deviation: a member count in (K, threshold] saturates
	// to the cap, so an S0-cold id reads hot. That only splits more (safe
	// direction) and matches the theory lane's measured chunks, which hold
	// no id in that band.
	t.Run("mid-band overestimates split-safe", func(t *testing.T) {
		t.Parallel()

		if got := s2Total(0, k+1); got != cap {
			t.Fatalf("s2Total(0, K+1) = %d, want the cap %d: the first saturated row must read hot", got, cap)
		}
		if got := s0Total(0, k+1); got != k+1 {
			t.Fatalf("s0Total(0, K+1) = %d, want %d exact: the test baseline must stay uncapped", got, k+1)
		}
	})

	// Off the equivalent bands the fill must only overestimate: a hotter
	// report splits more, never less. Sweep the mid band plus the edges.
	for member := int64(0); member <= cap+1; member += 17 {
		for _, cheap := range []int64{0, 9999, cap} {
			if got, want := s2Total(cheap, member), s0Total(cheap, member); got < want {
				t.Fatalf("s2Total(%d, %d) = %d under s0 %d: the fill underestimates, flipping the safe bias", cheap, member, got, want)
			}
			if s0Total(cheap, member) >= cap && s2Total(cheap, member) < cap {
				t.Fatalf("s2Total(%d, %d) lost hot: an s0-hot id must stay hot", cheap, member)
			}
		}
	}
}
