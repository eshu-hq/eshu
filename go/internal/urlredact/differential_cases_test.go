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
	tailInputRows := 0
	for _, tc := range cases {
		if _, dup := seen[tc.Name]; dup {
			t.Errorf("duplicate row name %q", tc.Name)
		}
		seen[tc.Name] = struct{}{}

		if len(tc.Fragments()) == 0 {
			t.Errorf("row %q declares no fragment, so it compares nothing", tc.Name)
		}
		// Three checks, and the counts below are only forcing because all three
		// hold. Containment is strings.Contains, which neither counts nor cares
		// WHAT it matched, so on its own it admits a fragment declared twice and
		// a fragment that is any substring of the input. Either one turns the
		// per-row cap into "unbounded" and hands a weakening somewhere to hide
		// an assertion it took off another row.
		declared := make(map[string]struct{}, len(tc.Fragments()))
		for _, fragment := range tc.Fragments() {
			if !strings.Contains(tc.Input, fragment) {
				t.Errorf("row %q declares a fragment that is not in its input", tc.Name)
			}
			if fragment != Sentinel && fragment != TailSentinel {
				t.Errorf("row %q declares %q, which is neither sentinel -- a proper substring passes containment and buys a row capacity it should not have", tc.Name, fragment)
			}
			if _, dup := declared[fragment]; dup {
				t.Errorf("row %q declares %q more than once, which lets a count stay exact while an assertion moves off a row", tc.Name, fragment)
			}
			declared[fragment] = struct{}{}
		}
		if strings.Contains(tc.Input, TailSentinel) {
			tailInputRows++
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

	// All seven totals are written down rather than derived from the axis tables.
	// Deriving them re-runs the generator's own arithmetic, so a change that
	// hollowed out the table would move the expectation with it and the numbers
	// the docs cite would quietly stop meaning anything. A deliberate new axis
	// fails here, tells you the new totals, and asks you to update the prose.
	//
	// Five levels, each added because a weakening walked past the one above it,
	// and every one of those weakenings cost the same half of the coverage -- the
	// partial-leak case. (The package docs group the SEVEN literals into four
	// questions; these five are the levels the weakenings climbed. Different
	// things counted, both true.)
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
	// at all sum to the same 114: measured, every literal then in place exact, package
	// green, and the mutation down from 36 red subtests to 27. How far it falls
	// depends on WHICH rows are demoted -- aim the demotion at the
	// literal-equals separator rows and more of the 36 go quiet -- but every
	// choice buys the silence with partial-leak assertions the numbers cannot
	// see leaving. So the counters are per row, and no row may declare a
	// fragment twice.
	//
	// WHAT COUNTS AS A FRAGMENT, because the per-row cap the argument above
	// rests on was asserted and not enforced. Containment is strings.Contains,
	// which admits ANY substring of the input, so a row could declare a proper
	// prefix of Sentinel and buy itself capacity. Measured: drop the
	// head-of-value Sentinel from all 114 credential inside rows and pay the
	// count back with a prefix on the 114 credential opening rows, and every
	// literal then in place holds, no fragment is declared twice, every fragment
	// is contained,
	// package green -- while 114 head-of-value assertions have moved off the
	// rows that carried them. The vocabulary check above is what makes the cap
	// real: a fragment must BE one of the two sentinels.
	//
	// The budget is then arithmetic rather than argument, and equality is FORCED
	// rather than observed. Vocabulary and no-duplicates cap each row's declared
	// set at the distinct sentinels its input holds, so capacity is at most
	// 594 + 198 = 792. Containment puts the 792 the table declares at or below
	// capacity. Both bounds are 792, so capacity is exactly 792 and every row
	// declares every sentinel its input holds -- which also means every input
	// holds Sentinel, a consequence of the arithmetic rather than a premise of
	// it. No row can carry a fragment another row gave up.
	const (
		wantRows               = 594
		wantRemovableRows      = 378
		wantRemovableFragments = 492
		wantOutsideFragments   = 300
		wantTailRemovableRows  = 114
		wantTailOutsideRows    = 84
		wantTailInputRows      = 198
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
	if tailInputRows != wantTailInputRows {
		t.Errorf("%d inputs hold TailSentinel, want %d -- declaring capacity is 594 plus this number, so anything above it is slack a weakening can move an assertion into", tailInputRows, wantTailInputRows)
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

// differentialAxisValue is one cell of one axis: the label that names it in a
// row name, and the text the generator actually splices into the input.
type differentialAxisValue struct{ label, text string }

// differentialAxis is one axis of the cross-product, flattened so a single loop
// can ask the same question of all of them.
type differentialAxis struct {
	name   string
	values []differentialAxisValue
}

// differentialAxes flattens the four TEXT-valued axes. The position axis is not
// here because its cells are functions, not strings, and retargeting one changes
// which fragments the row declares -- something the count pins already see.
func differentialAxes() []differentialAxis {
	keys := make([]differentialAxisValue, 0, len(differentialKeyNames))
	for _, key := range differentialKeyNames {
		keys = append(keys, differentialAxisValue{label: key.name, text: key.name})
	}
	spellings := make([]differentialAxisValue, 0, len(differentialSeparatorSpellings))
	for _, spelling := range differentialSeparatorSpellings {
		spellings = append(spellings, differentialAxisValue{label: spelling.name, text: spelling.text})
	}
	escapes := make([]differentialAxisValue, 0, len(differentialEscapes))
	for _, escape := range differentialEscapes {
		escapes = append(escapes, differentialAxisValue{label: escape.name, text: escape.text})
	}
	suffixes := make([]differentialAxisValue, 0, len(differentialSuffixes))
	for _, suffix := range differentialSuffixes {
		suffixes = append(suffixes, differentialAxisValue{label: suffix.name, text: suffix.text})
	}
	return []differentialAxis{
		{name: "key name", values: keys},
		{name: "separator spelling", values: spellings},
		{name: "escape", values: escapes},
		{name: "suffix", values: suffixes},
	}
}

// TestDifferentialAxisValuesAreDistinct pins that each axis crosses what its
// labels say it crosses.
//
// The seven literals next door are CARDINALITY pins. They notice an axis growing
// or shrinking and are blind to what is in a cell. Point the "an escaped line
// feed" cell at "%20" instead of "%0A", keep its label and the axis at eleven
// entries, and every literal holds, all three declaration rules hold, and the
// whole package is green -- while the line-feed byte, one of the eleven this
// cross-product exists to cross, is exercised nowhere. Two cells spelling the
// same text are two cells doing one cell's work.
//
// Labels need no check of their own: two cells sharing one collide on the row
// name, which TestDifferentialCasesAreSelfConsistent already rejects. Text is
// what nothing was looking at.
//
// The limit, so nobody reads more into this than it does: it catches a cell
// retargeted onto a text some other cell already has. A cell retargeted onto a
// NEW distinct text -- "%0A" to "%0B" -- leaves the axis crossing eleven
// different bytes under one wrong label, and nothing here notices. Pinning that
// would mean writing the axis down twice.
func TestDifferentialAxisValuesAreDistinct(t *testing.T) {
	t.Parallel()

	for _, axis := range differentialAxes() {
		if len(axis.values) == 0 {
			t.Errorf("the %s axis is empty, so the cross-product collapses", axis.name)
			continue
		}
		seen := make(map[string]string, len(axis.values))
		for _, value := range axis.values {
			if first, dup := seen[value.text]; dup {
				t.Errorf("the %s axis spells %q twice, as %q and %q -- two cells doing one cell's work, with every count still exact", axis.name, value.text, first, value.label)
				continue
			}
			seen[value.text] = value.label
		}
	}
}
