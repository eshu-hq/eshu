// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strconv"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestHolderMassProbeQueryIsFillToCapBound pins the #10681 probe shape
// (theory lane S2 for arm-3, arbiter Shape-B ruling for arms 1-2, K=100):
// every arm counts over LIMIT K+1 and reports the cap above K, so the
// per-id heap fetch stays bounded on all three arms instead of the
// uncapped exact-COUNT cheap arms that timed out live. It fails until
// the cheap-arm rewrite lands.
func TestHolderMassProbeQueryIsFillToCapBound(t *testing.T) {
	t.Parallel()

	q := HolderMassProbeQuery
	k := int64(supplychaincore.HolderMassProbeFillToCapRows)
	capDigits := strconv.FormatInt(supplychaincore.HolderMassProbeCap, 10)

	fillVerb := "CASE WHEN COUNT(*) > " + strconv.FormatInt(k, 10) +
		" THEN " + capDigits + " ELSE COUNT(*) END"
	if got := strings.Count(q, fillVerb); got != 3 {
		t.Fatalf("probe carries %d fill-to-cap verbs, want 3 (one per arm, K=100 covers all arms):\n%s", got, q)
	}
	limitVerb := "LIMIT " + strconv.FormatInt(k+1, 10)
	if got := strings.Count(q, limitVerb); got != 3 {
		t.Fatalf("probe carries %d bounded fetches %q, want 3 (one per arm):\n%s", got, limitVerb, q)
	}
	if strings.Contains(q, "LIMIT "+capDigits) {
		t.Fatalf("probe still fetches an arm up to the full cap instead of K+1:\n%s", q)
	}
	if got := strings.Count(q, "LIMIT"); got != 3 {
		t.Fatalf("probe carries %d LIMITs, want 3 (one per arm: all arms bounded, none exact)", got)
	}
	if strings.Contains(q, "EXISTS") {
		t.Fatalf("probe regressed to the rejected S1 EXISTS-to-cap shape, whose planner flips to an unbounded filter scan:\n%s", q)
	}
	if strings.Contains(q, "UNION ALL") {
		t.Fatalf("probe still aggregates the cheap arms with UNION ALL exact COUNT instead of per-id fill-to-cap LATERALs:\n%s", q)
	}
	if got := strings.Count(q, "LEFT JOIN"); got != 3 {
		t.Fatalf("probe joins %d times, want 3 (one per-id LATERAL arm) with zero-fill", got)
	}
}

// TestHolderMassProbeFillToCapDecidesLikeUncapped is the #10681
// decision-equivalence regression: at or under K on every arm the
// fill-to-cap report must decide exactly like the uncapped S0 report, and
// off those bands it must only overestimate (split-safe bias preserved).
// The maps compared here are the planner's whole input (one mass per id),
// so equal maps decide identically. Under the Shape-B ruling all three
// arms fill to the cap, so the model covers cve, advisory, and member.
func TestHolderMassProbeFillToCapDecidesLikeUncapped(t *testing.T) {
	t.Parallel()

	cap := int64(supplychaincore.HolderMassProbeCap)
	k := int64(supplychaincore.HolderMassProbeFillToCapRows)
	s0Total := func(cve, adv, member int64) int64 {
		if total := cve + adv + member; total > cap {
			return cap
		} else {
			return total
		}
	}
	fillArm := func(mass int64) int64 {
		if mass > k {
			return cap
		}
		return mass
	}
	// bTotal mirrors the shipped template: each arm fills to the cap, the
	// member arm runs only while the filled cheap arms stay under the cap,
	// and LEAST caps the total.
	bTotal := func(cve, adv, member int64) int64 {
		fc, fa := fillArm(cve), fillArm(adv)
		var fm int64
		if fc+fa < cap {
			fm = fillArm(member)
		}
		return s0Total(fc, fa, fm)
	}

	equivalent := []struct {
		name             string
		cve, adv, member int64
	}{
		{"zero", 0, 0, 0},
		{"cold on all arms", 3, 5, 7},
		{"every arm at K stays exact", k, k, k},
		{"member just hot", 0, 0, cap},
		{"hot member saturates both", 0, 0, 26000},
		{"hot cve saturates both", 26000, 0, 0},
		{"hot advisory saturates both", 0, 26000, 0},
		{"already-hot cheap short-circuits mid-band member", cap, 0, 5000},
		{"huge on every arm", 1 << 40, 1 << 40, 1 << 40},
	}
	for _, tc := range equivalent {
		t.Run("equivalent/"+tc.name, func(t *testing.T) {
			t.Parallel()

			if got, want := bTotal(tc.cve, tc.adv, tc.member), s0Total(tc.cve, tc.adv, tc.member); got != want {
				t.Fatalf("bTotal(%d, %d, %d) = %d, want s0 %d: the fill moved a decision", tc.cve, tc.adv, tc.member, got, want)
			}
		})
	}

	mixed := map[string][3]int64{
		"id-hot":  {0, 0, 26000},
		"id-cold": {3, 5, 7},
		"id-zero": {0, 0, 0},
		"id-huge": {0, 0, 1 << 40},
	}
	s0Map := make(map[string]int64, len(mixed))
	bMap := make(map[string]int64, len(mixed))
	for id, arms := range mixed {
		s0Map[id] = s0Total(arms[0], arms[1], arms[2])
		bMap[id] = bTotal(arms[0], arms[1], arms[2])
	}
	for id, want := range s0Map {
		if got := bMap[id]; got != want {
			t.Fatalf("mixed chunk id %q: shape-B reports %d, s0 reports %d", id, got, want)
		}
	}

	// The accepted deviations: a count in (K, threshold] on any arm
	// saturates to the cap, so an S0-cold id reads hot. That only splits
	// more (safe direction).
	t.Run("mid-band overestimates split-safe", func(t *testing.T) {
		t.Parallel()

		for _, arms := range [][3]int64{{0, 0, k + 1}, {k + 1, 0, 0}, {0, k + 1, 0}} {
			if got := bTotal(arms[0], arms[1], arms[2]); got != cap {
				t.Fatalf("bTotal(%d, %d, %d) = %d, want the cap %d: the first saturated row must read hot", arms[0], arms[1], arms[2], got, cap)
			}
			if got := s0Total(arms[0], arms[1], arms[2]); got != k+1 {
				t.Fatalf("s0Total(%d, %d, %d) = %d, want %d exact: the test baseline must stay uncapped", arms[0], arms[1], arms[2], got, k+1)
			}
		}
	})

	// Off the equivalent bands the fill must only overestimate: a hotter
	// report splits more, never less. Sweep every arm across the mid band
	// plus the edges.
	for _, cve := range []int64{0, k, k + 1, 9999, cap} {
		for _, adv := range []int64{0, k, k + 1, 9999, cap} {
			for member := int64(0); member <= cap+1; member += 257 {
				if got, want := bTotal(cve, adv, member), s0Total(cve, adv, member); got < want {
					t.Fatalf("bTotal(%d, %d, %d) = %d under s0 %d: the fill underestimates, flipping the safe bias", cve, adv, member, got, want)
				}
				if s0Total(cve, adv, member) >= cap && bTotal(cve, adv, member) < cap {
					t.Fatalf("bTotal(%d, %d, %d) lost hot: an s0-hot id must stay hot", cve, adv, member)
				}
			}
		}
	}
}
