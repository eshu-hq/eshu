// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package urlredact

import (
	"strings"
	"testing"
)

// TestDifferentialCasesAreSelfConsistent checks the generated table itself, not
// a walk. A row whose declared fragments are not in its input would let the
// driver in internal/reportbundle pass while comparing nothing.
func TestDifferentialCasesAreSelfConsistent(t *testing.T) {
	t.Parallel()

	cases := DifferentialCases()
	if len(cases) == 0 {
		t.Fatal("the differential is empty, so its driver would pass vacuously")
	}

	seen := make(map[string]struct{}, len(cases))
	openingTheValue := 0
	removableRows := 0
	removableFragments := 0
	outsideFragments := 0
	tailRemovableRows := 0
	tailOutsideRows := 0
	for _, tc := range cases {
		if _, dup := seen[tc.Name]; dup {
			t.Errorf("duplicate row name %q", tc.Name)
		}
		seen[tc.Name] = struct{}{}

		if len(tc.Fragments()) == 0 {
			t.Errorf("row %q declares no fragment, so it compares nothing", tc.Name)
		}
		// Containment is checked with strings.Contains, which cannot count, so
		// a row declaring the same fragment twice passes it. That is not a
		// pedantic case: a duplicate is what lets a fragment be dropped from one
		// row and doubled on another while every aggregate below stays exact.
		// Distinctness is also what makes those aggregates forcing — see the
		// note on the pins.
		declared := make(map[string]struct{}, len(tc.Fragments()))
		for _, fragment := range tc.Fragments() {
			if !strings.Contains(tc.Input, fragment) {
				t.Errorf("row %q declares a fragment that is not in its input", tc.Name)
			}
			if _, dup := declared[fragment]; dup {
				t.Errorf("row %q declares %q more than once, which lets a count stay exact while an assertion moves off a row", tc.Name, fragment)
			}
			declared[fragment] = struct{}{}
		}
		if len(tc.Removable) > 0 {
			removableRows++
		}
		removableFragments += len(tc.Removable)
		outsideFragments += len(tc.Outside)
		// Per ROW, not per occurrence. Counting occurrences is what let the
		// aggregate be redistributed: 57 rows declaring TailSentinel twice and
		// 57 declaring it not at all sums to the same 114.
		for _, fragment := range tc.Removable {
			if fragment == TailSentinel {
				tailRemovableRows++
				break
			}
		}
		for _, fragment := range tc.Outside {
			if fragment == TailSentinel {
				tailOutsideRows++
				break
			}
		}
		// The cell that leaked: the escape is the first byte of the value, so
		// the text pending at it is a bare "name=".
		for _, escape := range differentialEscapes {
			if strings.Contains(tc.Input, "="+escape.text) {
				openingTheValue++
				break
			}
		}
	}
	if openingTheValue == 0 {
		t.Error("no differential row puts the escape at the start of a value, which is the position that shipped a whole credential")
	}

	// All six totals are written down rather than derived from the axis tables.
	// Deriving them re-runs the generator's own arithmetic, so a change that
	// hollowed out the table would move the expectation with it and the numbers
	// the docs cite would quietly stop meaning anything. A deliberate new axis
	// fails here, tells you the new totals, and asks you to update the prose.
	//
	// Three questions, each of which a weakening slipped past the level above.
	//
	// HOW MANY ROWS. 594, and 378 of them carrying a removable fragment.
	//
	// HOW MANY FRAGMENTS, because a row count cannot see an assertion leave.
	// 114 rows carry two removable fragments -- 492 fragments over 378 rows,
	// two at most per row -- so demoting the second to Outside holds both row
	// totals exactly. Measured on that weakening: removable 492 -> 378, outside
	// 300 -> 414, everything else in the package green, and the
	// both-walks-wrong-the-same-way mutation down from 36 red subtests to 18.
	//
	// WHICH FRAGMENT, because a count cannot see one identity substituted for
	// another. Declare Sentinel twice in the "inside the value" position and all
	// four totals above stay exact, every fragment is still contained in its
	// input, the whole suite passes -- and TailSentinel is declared by nothing.
	// It is the fragment that sits AFTER the escape, so losing it loses the
	// partial-leak assertion, and the same mutation drops 36 red subtests to 18
	// again.
	//
	// ON WHICH ROWS, because an identity count is redistributable too. Counting
	// occurrences, 57 rows declaring TailSentinel twice and 57 declaring it not
	// at all sum to the same 114: measured, all six literals exact, package
	// green, and the same 36 -> 18 collapse. The counters are per row and no row
	// may declare a fragment twice, which together make these numbers FORCING
	// rather than merely consistent. Each row can declare at most the distinct
	// sentinels its input holds -- one for an opening or closing value, two for
	// an inside one -- so 792 fragments over 594 rows leaves no slack: every row
	// declares each of its sentinels exactly once, TailSentinel lands in exactly
	// one list on each of the 198 inside rows, and 114/84 fixes the split.
	//
	// That is where the ladder stops. See AGENTS.md for the one assumption the
	// argument rests on.
	const (
		wantRows               = 594
		wantRemovableRows      = 378
		wantRemovableFragments = 492
		wantOutsideFragments   = 300
		wantTailRemovableRows  = 114
		wantTailOutsideRows    = 84
	)
	if len(cases) != wantRows {
		t.Errorf("the differential generates %d rows, want %d -- update the figure wherever it is cited", len(cases), wantRows)
	}
	if removableRows != wantRemovableRows {
		t.Errorf("%d rows carry a fragment both walks must remove, want %d -- a row with none pins agreement only, and counting it as removal coverage is what this number exists to stop", removableRows, wantRemovableRows)
	}
	if removableFragments != wantRemovableFragments {
		t.Errorf("%d removable fragments, want %d -- the row count cannot see a fragment demoted to Outside, and each demotion is one removal assertion that stopped running", removableFragments, wantRemovableFragments)
	}
	if outsideFragments != wantOutsideFragments {
		t.Errorf("%d outside fragments, want %d -- a fragment promoted to Removable claims the walks must remove text the depth model puts past the separator", outsideFragments, wantOutsideFragments)
	}
	if tailRemovableRows != wantTailRemovableRows {
		t.Errorf("TailSentinel is declared removable on %d rows, want %d -- it is the fragment after the escape, so a fragment count that stays exact while this moves has swapped the partial-leak assertion for a duplicate", tailRemovableRows, wantTailRemovableRows)
	}
	if tailOutsideRows != wantTailOutsideRows {
		t.Errorf("TailSentinel is declared outside on %d rows, want %d -- see above; the two halves are pinned separately because a fragment moving between them holds every other total", tailOutsideRows, wantTailOutsideRows)
	}
}

// TestCheckAgreementFailsBothDirections breaks the check on purpose. A
// comparison that cannot go red is the defect this package keeps finding in its
// own guards, so the assertion is exercised rather than assumed: one direction
// proves an unrecorded disagreement fails, the other proves a recorded
// exemption that stopped being true fails too.
func TestCheckAgreementFailsBothDirections(t *testing.T) {
	t.Parallel()

	unexempted := DifferentialCase{Outside: []string{Sentinel}}
	if err := unexempted.CheckAgreement("clean", Sentinel); err == nil {
		t.Error("a walk keeping a fragment the other removed passed with no exemption recorded")
	}
	if err := unexempted.CheckAgreement("clean", "clean"); err != nil {
		t.Errorf("two agreeing walks failed: %v", err)
	}

	exempted := DifferentialCase{Outside: []string{Sentinel}, WalksDisagree: "recorded"}
	if err := exempted.CheckAgreement("clean", "clean"); err == nil {
		t.Error("a stale exemption passed, so the differential could quietly permit more than it says")
	}
	if err := exempted.CheckAgreement("clean", Sentinel); err != nil {
		t.Errorf("a live exemption failed: %v", err)
	}
}

// TestCheckRemovalFiresWhenBothWalksKeepTheCredential is the assertion
// CheckAgreement structurally cannot make. Two walks that both keep a fragment
// agree, so the agreement check is silent; that silence is what let 18
// credential-named rows count as coverage while carrying no removal assertion
// at all, and what makes a shared-predicate regression invisible on all 594.
func TestCheckRemovalFiresWhenBothWalksKeepTheCredential(t *testing.T) {
	t.Parallel()

	inside := DifferentialCase{Removable: []string{Sentinel}}
	if err := inside.CheckAgreement(Sentinel, Sentinel); err != nil {
		t.Errorf("the agreement check fired on two walks that agree: %v", err)
	}
	if err := inside.CheckRemoval(Sentinel, Sentinel); err == nil {
		t.Error("both walks kept a credential fragment and the removal check passed")
	}
	if err := inside.CheckRemoval(Sentinel, "clean"); err == nil {
		t.Error("the endpoint walk kept a credential fragment and the removal check passed")
	}
	if err := inside.CheckRemoval("clean", Sentinel); err == nil {
		t.Error("the free-text walk kept a credential fragment and the removal check passed")
	}
	if err := inside.CheckRemoval("clean", "clean"); err != nil {
		t.Errorf("two walks that both removed the fragment failed: %v", err)
	}

	// A fragment the depth model puts outside the value carries no removal
	// requirement, which is the whole reason the two lists are separate.
	outside := DifferentialCase{Outside: []string{Sentinel}}
	if err := outside.CheckRemoval(Sentinel, Sentinel); err != nil {
		t.Errorf("an Outside fragment was held to a removal requirement: %v", err)
	}
}
