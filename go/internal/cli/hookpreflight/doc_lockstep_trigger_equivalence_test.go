// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"go/ast"
	"go/token"
	"sort"
	"strconv"
	"testing"
)

// Whole-program half of the trigger-class pin (see doc_lockstep_test.go for
// the conventions the whole guard follows).
//
// Its two siblings read syntax. doc_lockstep_switch_test.go holds
// triggerAllowed to a shape the accepted classes can be read out of, and
// doc_lockstep_trigger_path_test.go names who may write a Trigger field and
// requires Evaluate's switch to consult triggerAllowed. Four generations of
// that reading were evaded, each by a spelling the previous generation did not
// recognize -- a guard clause before the switch, a rewritten switch tag, a
// remap in normalizeInput, a widened twin, a build-excluded decoy, a pointer
// alias, an unkeyed composite literal. Every generation was written to be the
// last one, and the next hostile read found another spelling. Enumerating
// syntax cannot close a set that is not finite.
//
// So this file stops describing how the code may be written and asserts what
// it must do: for a request that is eligible in every other respect, Evaluate
// advises for exactly the triggers triggerAllowed accepts. A rewrite between
// Input.Trigger and the gate breaks that equality however it is spelled,
// because it moves one side and not the other.
//
// It is a property over a bounded sample, not a proof, and the bounds are where
// it fails. Two of them, both measured green before the arms below existed:
//
//	a class outside the swept set. The exhaustive arm stops at four characters
//	and the neighbourhood arm one edit from a named class, so a remap reaching
//	further -- `if len(t) > 4 && triggerAllowed(t[:4]) { t = t[:4] }` -- is only
//	caught because "readx" happens to sit one edit from "read".
//	a request outside the swept axes. A gate keyed on another field
//	(`if input.Permission == "elevated" { input.Trigger = "read" }`) leaves the
//	equality true for every trigger on the baseline request and false only for
//	requests this test never builds. That is what triggerAxisVariants is for.
//
// And it cannot see a widening applied consistently to both sides --
// triggerAllowed itself accepting a new class, which keeps the equality true --
// which is the case the two source scanners were built for. The three files are
// complements, not layers: none subsumes another, and removing any one leaves a
// live hole. Widening the sweep or the axis set is the first thing to try when
// an evasion slips through, not concluding the property is wrong.
//
// The sweep is exhaustive rather than a candidate list. A hand-written list can
// only hold classes someone already thought of, which is the failure mode
// AGENTS.md names as this package's most common change.
//
// The exhaustive arm stops at four characters, and that ceiling is a real gap:
// every documented class is four to six characters, so most plausible new class
// names fall outside it. The literal arm was meant to cover the rest, on the
// reasoning that a remap has to name the class it widens -- which is false for a
// remap that computes the class instead of naming it.
// `if p := &normalized.Trigger; strings.HasPrefix(*p, "read") { *p = "read" }`
// names only "read", and every class it widens (read_file, readonly, readx) is
// five characters or more and appears in no literal. A HasSuffix variant behaves
// the same way. Arm four is for that shape.

// triggerSweepAlphabet and triggerSweepMaxLen bound the exhaustive arm: every
// string over this alphabet up to this length is compared. The alphabet is
// lowercase and whitespace-free on purpose -- normalizeInput folds case and
// trims, so "READ" would advise while triggerAllowed("READ") is false, and the
// mismatch would be normalizeInput doing its documented job rather than a
// rewrite. Digits, "_" and "-" are in because ToLower and TrimSpace leave them
// alone, so they carry the same guarantee.
const (
	triggerSweepAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789_-"
	triggerSweepMaxLen   = 4
)

// eligibleExceptTrigger is a request that advises whenever its trigger is
// accepted: a supported host, the hook enabled, a narrow safe scope, fresh
// index, allowed permission, and time left on the budget. Every skip reason
// other than the trigger is ruled out, so the decision below turns on the
// trigger alone.
func eligibleExceptTrigger() Input {
	return Input{
		Host:     supportedHostClaude,
		Enabled:  true,
		RepoPath: "services/api/handler.go",
		Budget:   DefaultBudget,
	}
}

// evaluateAdvises reports whether Evaluate advises for trigger on an otherwise
// eligible request. This is the production side of the equivalence: it goes
// through the real Evaluate, not a reimplementation of its switch.
func evaluateAdvises(trigger string) bool {
	return evaluateAdvisesWith(eligibleExceptTrigger(), trigger)
}

// sourceStringLiterals returns every distinct string literal the non-test files
// in dir declare. A rewrite has to name the class it remaps, and a literal is
// where that name lands, so this arm reaches classes longer than
// triggerSweepMaxLen without guessing which words to guess.
func sourceStringLiterals(dir string) ([]string, error) {
	_, parsed, _, err := parseNonTestGoFiles(dir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, file := range parsed {
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, unquoteErr := strconv.Unquote(lit.Value)
			if unquoteErr == nil {
				seen[value] = true
			}
			return true
		})
	}
	literals := make([]string, 0, len(seen))
	for value := range seen {
		literals = append(literals, value)
	}
	sort.Strings(literals)
	return literals, nil
}

// TestDocLockstepEvaluateAdvisesExactlyTheAllowedTriggers is the property the
// four generations of source scanning were each an approximation of: on an
// otherwise eligible request, Evaluate's advise set and triggerAllowed's accept
// set are the same set.
//
// It is what README.md and AGENTS.md mean by "the trigger reaches that switch
// unrewritten". A remap in normalizeInput, a pointer alias inside Evaluate, a
// gate reading a rewritten copy, or a call to a widened twin all make Evaluate
// advise for a trigger triggerAllowed refuses, and each fails here on the
// behaviour rather than on the shape it was written in.
func TestDocLockstepEvaluateAdvisesExactlyTheAllowedTriggers(t *testing.T) {
	t.Parallel()

	// Controls first. Without them a base request that never advises -- or one
	// that always does -- would pass every comparison below while proving
	// nothing about the trigger.
	if !evaluateAdvises("read") {
		t.Fatal(`Evaluate does not advise for trigger "read" on an otherwise eligible request; the sweep below would compare two constants`)
	}
	if evaluateAdvises("edit") {
		t.Fatal(`Evaluate advises for trigger "edit", which the contract doc rules out; the sweep below cannot distinguish a rewrite from a base request that always advises`)
	}

	compared, advised, mismatched := 0, 0, 0
	type mismatch struct {
		trigger string
		advises bool
	}
	var reported []mismatch
	check := func(trigger string) {
		compared++
		got := evaluateAdvises(trigger)
		if got {
			advised++
		}
		if got == triggerAllowed(trigger) {
			return
		}
		mismatched++
		if len(reported) < 8 {
			reported = append(reported, mismatch{trigger: trigger, advises: got})
		}
	}

	// Arm one: every string over the alphabet up to triggerSweepMaxLen, plus
	// the empty string. This is the arm that catches a class nobody listed.
	buf := make([]byte, 0, triggerSweepMaxLen)
	var sweep func(prefix []byte)
	sweep = func(prefix []byte) {
		check(string(prefix))
		if len(prefix) == triggerSweepMaxLen {
			return
		}
		for i := 0; i < len(triggerSweepAlphabet); i++ {
			sweep(append(prefix, triggerSweepAlphabet[i]))
		}
	}
	sweep(buf)
	sweptExhaustively := compared

	// Arm two: every string literal the production files declare, which reaches
	// the classes longer than triggerSweepMaxLen that a remap would have to name
	// somewhere in the source.
	literals, err := sourceStringLiterals(".")
	if err != nil {
		t.Fatalf("read the package's string literals: %v", err)
	}
	if len(literals) == 0 {
		t.Fatal("found no string literals in the production files; that arm of the sweep would be vacuous")
	}
	for _, literal := range literals {
		check(literal)
	}

	// Arm three: the classes the docs and the Claude tool mapping name, so the
	// accepted set itself is compared at its real length -- "search", "symbol"
	// and "prompt" are six characters and the exhaustive arm never reaches them.
	_, accepted, _, err := scanClosedStringSwitch(".", "triggerAllowed")
	if err != nil {
		t.Fatalf("read triggerAllowed's accepted classes: %v", err)
	}
	if len(accepted) == 0 {
		t.Fatal("read no accepted classes out of triggerAllowed; that arm of the sweep would be vacuous")
	}
	named := append(append([]string{}, accepted...), documentedTriggers()...)
	advisingTools, rejectedTools := claudeToolTriggerClasses()
	for _, tools := range []map[string]string{advisingTools, rejectedTools} {
		for tool := range tools {
			named = append(named, triggerFromClaudeTool(tool))
		}
	}
	sort.Strings(named)
	for _, trigger := range named {
		check(trigger)
	}

	// Arm four: every string one character edit away from one of those named
	// classes. This is the arm that sees a remap computed from a class's own
	// text rather than spelled out -- a prefix, suffix, or substring test names
	// only the class it maps onto, so arm two finds no literal for the classes
	// it widens, and they are longer than arm one reaches.
	neighbours := oneEditNeighbourhood(named)
	present := make(map[string]bool, len(neighbours))
	for _, neighbour := range neighbours {
		present[neighbour] = true
	}
	for _, base := range named {
		// The one-character-longer strings on either side are what this arm
		// exists for, so they are asserted by name. A neighbourhood that
		// generated only the bases back, or nothing at all, would otherwise
		// pass every comparison below.
		for _, want := range []string{base + "x", "x" + base} {
			if !present[want] {
				t.Fatalf("the one-edit neighbourhood of %q does not contain %q; a prefix- or suffix-keyed remap of that class would go unnoticed", base, want)
			}
		}
	}
	for _, trigger := range neighbours {
		check(trigger)
	}

	// The axes. Every arm above varies the trigger on one request shape and
	// fixes the other ten fields, so a gate keyed on one of them is invisible to
	// all four. Each variant re-runs the named classes and their neighbourhood
	// -- the exhaustive arm is not repeated, because a gate keyed on another
	// field admits whole classes at once and those are the classes it admits.
	reduced := append(append([]string{}, named...), neighbours...)
	variants := triggerAxisVariants()
	if len(variants) < 20 {
		t.Fatalf("axis variants = %d, want the permission-by-freshness cross product plus the scope, host, tool and budget shapes", len(variants))
	}
	variantCompared, variantMismatched := 0, 0
	for _, variant := range variants {
		// Each variant is an advising request in its own right, or the
		// comparison below holds for a shape that never advises.
		if !evaluateAdvisesWith(variant.Input, "read") {
			t.Fatalf("%s: does not advise for trigger \"read\"; that variant would compare two constants", variant.Name)
		}
		if evaluateAdvisesWith(variant.Input, "edit") {
			t.Errorf("%s: advises for trigger \"edit\", which the contract doc rules out", variant.Name)
		}
		for _, trigger := range reduced {
			variantCompared++
			got := evaluateAdvisesWith(variant.Input, trigger)
			if got == triggerAllowed(trigger) {
				continue
			}
			variantMismatched++
			if variantMismatched <= 8 {
				t.Errorf("%s, trigger %q: Evaluate advises=%v, triggerAllowed=%v; a request field other than the trigger is deciding which classes get an advisory",
					variant.Name, trigger, got, triggerAllowed(trigger))
			}
		}
	}
	if variantMismatched > 8 {
		t.Errorf("%d trigger/variant pairs disagreed in total; the first 8 are named above", variantMismatched)
	}
	if want := len(variants) * len(reduced); variantCompared != want {
		t.Errorf("axis arm compared %d trigger/variant pairs, want %d", variantCompared, want)
	}

	for _, bad := range reported {
		t.Errorf("trigger %q: Evaluate advises=%v, triggerAllowed=%v; something between Input.Trigger and the gate rewrote the class, so which classes get an advisory is no longer what triggerAllowed says",
			bad.trigger, bad.advises, triggerAllowed(bad.trigger))
	}
	if mismatched > len(reported) {
		t.Errorf("%d triggers disagreed in total; the first %d are named above", mismatched, len(reported))
	}

	// The counts are part of the assertion. A sweep that silently stopped early,
	// or one where no trigger advised, passes every comparison above.
	if want := expectedSweepSize(); sweptExhaustively != want {
		t.Errorf("exhaustive arm compared %d triggers, want %d; the sweep did not cover every string over %q up to length %d", sweptExhaustively, want, triggerSweepAlphabet, triggerSweepMaxLen)
	}
	if advised == 0 {
		t.Errorf("no trigger out of %d advised; the comparison held only because Evaluate never advises", compared)
	}
	t.Logf("compared %d triggers (%d exhaustive, %d source literals, %d named classes, %d one-edit neighbours); %d advised. %d further comparisons over %d request shapes",
		compared, sweptExhaustively, len(literals), len(named), len(neighbours), advised, variantCompared, len(variants))
}

// expectedSweepSize is the number of strings over triggerSweepAlphabet of
// length 0 through triggerSweepMaxLen, computed independently of the walk that
// generates them so a bug in the walk cannot define its own expectation.
func expectedSweepSize() int {
	total, power := 0, 1
	for length := 0; length <= triggerSweepMaxLen; length++ {
		total += power
		power *= len(triggerSweepAlphabet)
	}
	return total
}
