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
	for _, tc := range cases {
		if _, dup := seen[tc.Name]; dup {
			t.Errorf("duplicate row name %q", tc.Name)
		}
		seen[tc.Name] = struct{}{}

		if len(tc.Fragments()) == 0 {
			t.Errorf("row %q declares no fragment, so it compares nothing", tc.Name)
		}
		for _, fragment := range tc.Fragments() {
			if !strings.Contains(tc.Input, fragment) {
				t.Errorf("row %q declares a fragment that is not in its input", tc.Name)
			}
		}
		if len(tc.Removable) > 0 {
			removableRows++
		}
		removableFragments += len(tc.Removable)
		outsideFragments += len(tc.Outside)
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

	// All four totals are written down rather than derived from the axis
	// tables. Deriving them re-runs the generator's own arithmetic, so a change
	// that hollowed out the table would move the expectation with it and the
	// numbers the docs cite would quietly stop meaning anything. A deliberate
	// new axis fails here, tells you the new totals, and asks you to update the
	// prose.
	//
	// The FRAGMENT counts are here because the row counts alone were not enough.
	// 114 of the 594 rows carry two Removable fragments -- 492 fragments across
	// 378 rows, and a row holds at most two -- so demoting the second one to
	// Outside leaves BOTH row totals untouched. Measured on that weakening:
	// removable fragments 492 -> 378, outside 300 -> 414, every other test in
	// the package still green, and the both-walks-wrong-the-same-way mutation
	// down from 36 red subtests to 18. The half that disappears is exactly the
	// partial-leak case TailSentinel exists to catch.
	const (
		wantRows               = 594
		wantRemovableRows      = 378
		wantRemovableFragments = 492
		wantOutsideFragments   = 300
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
