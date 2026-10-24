// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"math/rand"
	"strings"
	"testing"
)

// uvDamage is one structural damage the seeded generator applies to a clean
// file, with the outcome the generator (not the parser) predicts.
type uvDamage int

const (
	uvDamageCommentHeaders    uvDamage = iota // every [[package]] gains a trailing comment: same rows, complete
	uvDamageOpenRootArray                     // a root array never closes: partial, no member or root omitted
	uvDamageOpenMembers                       // the members array never closes: partial, no member or root omitted
	uvDamageBadMembersList                    // the members list breaks the grammar: complete, no member omitted
	uvDamageOpenSubTableAtEOF                 // a trailing sub-table array never closes: partial, same rows
	uvDamageKinds
)

// uvApplyDamage rewrites src and returns the expected rows, root, and members
// counts plus whether the state must be complete. ok=false when the damage
// does not apply to this file (no members list to break).
func uvApplyDamage(rng *rand.Rand, src string, want uvFuzzExpect, kind uvDamage) (string, uvFuzzExpect, bool, bool) {
	hasMembers := strings.Contains(src, "[manifest]")
	all := uvFuzzExpect{rows: want.rows + want.members + want.root}
	if rng.Intn(2) == 0 && kind != uvDamageCommentHeaders {
		// Resync must also work when the header after the swallow has a comment.
		src = strings.ReplaceAll(src, "[[package]]\n", "[[package]] # lock entry\n")
	}
	switch kind {
	case uvDamageCommentHeaders:
		comments := []string{" # c", "\t# pinned", "   #", "#x"}
		return strings.ReplaceAll(src, "[[package]]\n", "[[package]]"+comments[rng.Intn(len(comments))]+"\n"), want, true, true
	case uvDamageOpenRootArray:
		open := []string{
			"resolution-markers = [\n    \"python_full_version >= '3.12'\",\n",
			"resolution-markers = [\n",
			"conflicts = [[\n    { package = \"a\", extra = \"x\" },\n",
		}
		return strings.Replace(src, "revision = 2\n", "revision = 2\n"+open[rng.Intn(len(open))], 1), all, false, true
	case uvDamageOpenMembers:
		if !hasMembers {
			return src, want, false, false
		}
		return strings.Replace(src, "    \"pkg-1\",\n]\n", "    \"pkg-1\",\n", 1), all, false, true
	case uvDamageBadMembersList:
		if !hasMembers {
			return src, want, false, false
		}
		breaks := [][2]string{
			{"\"Pkg_0\",\n", "\"Pkg_0\"\n"},        // missing comma across lines
			{"\"Pkg_0\",\n", "\"Pkg_0\",,\n"},      // doubled comma
			{"members = [\n", "members = [\n,\n"},  // leading comma
			{"\"Pkg_0\",\n", "\"Pkg_0\", 1,\n"},    // a non-string member
			{"\"Pkg_0\",\n", "\"Pkg_0\" \"x\",\n"}, // two names, no comma
		}
		b := breaks[rng.Intn(len(breaks))]
		return strings.Replace(src, b[0], b[1], 1), uvFuzzExpect{rows: want.rows + want.members, root: want.root}, true, true
	default: // uvDamageOpenSubTableAtEOF
		return src + "\n[package.optional-dependencies]\nextra = [\n    { name = \"more\" },\n", want, false, true
	}
}

// TestParseUVLockSeededDamageFailsClosed applies structural damage that random
// byte mutation rarely produces (outer arrays left open, commented headers,
// broken member lists) and checks the exact outcome the generator predicts.
func TestParseUVLockSeededDamageFailsClosed(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewSource(88211))
	cases := uvFuzzCases(t)
	var applied [uvDamageKinds]int
	for c := 0; c < cases; c++ {
		src, want := uvFuzzSeedFile(rng, 1+rng.Intn(4))
		kind := uvDamage(rng.Intn(int(uvDamageKinds)))
		damaged, expect, complete, ok := uvApplyDamage(rng, src, want, kind)
		if !ok {
			continue
		}
		applied[kind]++
		result := parseUVLockBytes([]byte(damaged))
		s := result.State
		if len(result.Rows) != expect.rows || s.RootPackages != expect.root || s.WorkspaceMembers != expect.members || (s.State() == "complete") != complete {
			t.Fatalf("case %d damage %d: rows=%d state=%+v, want %+v complete=%v\n%s", c, kind, len(result.Rows), s, expect, complete, damaged)
		}
		if sum := s.Resolved + s.Unresolved + s.Overflow + s.RootPackages + s.WorkspaceMembers; sum != s.Entries {
			t.Fatalf("case %d: accounted=%d != entries=%d in %+v\n%s", c, sum, s.Entries, s, damaged)
		}
		if result.Scanned > 3*len(damaged)+1024 {
			t.Fatalf("case %d: scanned %d > 3x input %d", c, result.Scanned, len(damaged))
		}
		uvAssertResolvedRowsAreWrittenVerbatim(t, damaged, result)
	}
	t.Logf("%d cases: damage applied per kind %v (comment headers, open root array, open members, bad members list, open sub-table at EOF)", cases, applied)
}
