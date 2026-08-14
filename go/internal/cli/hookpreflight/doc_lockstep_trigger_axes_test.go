// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package hookpreflight

import (
	"fmt"
	"sort"
	"testing"
)

// The bounds of the trigger equivalence, split out of
// doc_lockstep_trigger_equivalence_test.go so the property and the shape of the
// sample it is checked over can be read separately.
//
// The equivalence in that file compares Evaluate's advise set against
// triggerAllowed's accept set. That comparison is only as good as what it is
// run over, and twice now an evasion has passed by sitting outside the sample
// rather than by satisfying the property: a remap reaching classes longer than
// the exhaustive arm sweeps, and a gate keyed on a request field the sweep held
// at one value. This file holds the two things that decide the sample -- the
// alphabet the one-edit neighbourhood is built from, and the request shapes the
// comparison is repeated on -- and pins the constants the coverage rests on.

// triggerNeighbourhoodAlphabet is the alphabet the one-edit arm uses. It is
// wider than triggerSweepAlphabet because that arm's cost is linear in the
// alphabet while the exhaustive arm's is the fourth power, so the punctuation a
// remap can key on is affordable here and not there. A remap that strips a
// trailing "@" widens "read@", which is one edit from "read" over this alphabet
// and unreachable over the other. Still lowercase and whitespace-free, for the
// reason above: normalizeInput folds those itself.
const triggerNeighbourhoodAlphabet = triggerSweepAlphabet + "@#$%^&*+=!?.,:;/\\|~<>()[]{}'\"`"

// TestDocLockstepTriggerSweepBoundsAreFixed pins the two constants that decide
// how much the exhaustive arm covers. expectedSweepSize is computed from them,
// so it agrees with any value they take: shrinking the alphabet to three
// characters alongside a literal-free remap left the whole comparison green and
// the count assertion satisfied. Every other magic number in this package is
// pinned as a literal, and these are the two the coverage rests on.
func TestDocLockstepTriggerSweepBoundsAreFixed(t *testing.T) {
	t.Parallel()

	if len(triggerSweepAlphabet) != 38 || triggerSweepMaxLen != 4 {
		t.Fatalf("the exhaustive arm sweeps %d characters up to length %d, want 38 and 4; narrowing either shrinks what the equivalence covers without changing a single assertion",
			len(triggerSweepAlphabet), triggerSweepMaxLen)
	}
	if len(triggerNeighbourhoodAlphabet) <= len(triggerSweepAlphabet) {
		t.Fatalf("the neighbourhood alphabet is %d characters and the sweep alphabet is %d; the neighbourhood arm exists to reach the punctuation the exhaustive arm cannot afford",
			len(triggerNeighbourhoodAlphabet), len(triggerSweepAlphabet))
	}
}

// evaluateAdvisesWith is the same question asked of a request shape other than
// the baseline.
func evaluateAdvisesWith(base Input, trigger string) bool {
	base.Trigger = trigger
	return Evaluate(base).Decision == DecisionAdvise
}

// triggerAxisVariant is one otherwise-eligible request shape the equivalence is
// re-checked on.
type triggerAxisVariant struct {
	Name  string
	Input Input
}

// triggerAxisVariants are the request shapes the equivalence is checked on
// besides the baseline. eligibleExceptTrigger varies one field and fixes ten,
// so a gate keyed on one of those ten holds the equality true everywhere the
// baseline sweep looks: `if input.Permission == "elevated" { input.Trigger =
// "read" }` gave a 324-character advisory to bash, write, delete and push under
// --permission elevated, with the equivalence log unchanged at 26 advised.
//
// The values are the package's own constants plus, for each field, one value no
// production comparison names.
//
// Do not read more into that second group than it carries. An earlier version
// of this comment claimed it caught "an added gate keying on a value outside
// that set". It does not, and the measurement is worth keeping: of six gates
// inserted ahead of Evaluate's switch, the only two that failed were the two
// whose literals appear below -- Permission == "elevated" and Freshness ==
// "unnamed". Gates on "sudo", "indexing", Tool == "Terminal" and Workload ==
// "canary" all survived. Adding a literal here catches a gate on that literal
// and nothing else; this is an enumeration, and widening it does not make it a
// property.
//
// What actually closes that axis is structural and lives elsewhere:
// TestDocLockstepEvaluateHasOneAdvisePath (doc_lockstep_gate_inputs_test.go)
// holds Evaluate to a single advise path, so a second one fails whatever field
// or value it keys on. These variants are still worth running -- they check the
// trigger equivalence holds on request shapes the baseline never builds -- but
// they are a sample, not a closure.
func triggerAxisVariants() []triggerAxisVariant {
	var variants []triggerAxisVariant
	for _, permission := range []string{"", PermissionAllowed, "elevated", "unnamed"} {
		for _, freshness := range []string{"", FreshnessFresh, freshnessStale, freshnessBuilding, "unnamed"} {
			input := eligibleExceptTrigger()
			input.Permission = permission
			input.Freshness = freshness
			variants = append(variants, triggerAxisVariant{
				Name:  fmt.Sprintf("permission=%q/freshness=%q", permission, freshness),
				Input: input,
			})
		}
	}
	for _, scope := range []struct {
		name  string
		apply func(*Input)
	}{
		{name: "scope=entity_id", apply: func(in *Input) { in.RepoPath, in.EntityID = "", "checkout" }},
		{name: "scope=service", apply: func(in *Input) { in.RepoPath, in.Service = "", "checkout" }},
		{name: "scope=resource", apply: func(in *Input) { in.RepoPath, in.Resource = "", "checkout" }},
	} {
		input := eligibleExceptTrigger()
		scope.apply(&input)
		variants = append(variants, triggerAxisVariant{Name: scope.name, Input: input})
	}
	for _, host := range []string{" Claude ", "CLAUDE"} {
		input := eligibleExceptTrigger()
		input.Host = host
		variants = append(variants, triggerAxisVariant{Name: "host=" + host, Input: input})
	}
	withTool := eligibleExceptTrigger()
	withTool.Tool = "Read"
	variants = append(variants, triggerAxisVariant{Name: "tool set", Input: withTool})
	inBudget := eligibleExceptTrigger()
	inBudget.Elapsed = DefaultBudget - 1
	variants = append(variants, triggerAxisVariant{Name: "elapsed just inside budget", Input: inBudget})
	return variants
}

// oneEditNeighbourhood returns every string one character edit away from a
// value in bases: an insertion at any position, a substitution at any position,
// or a deletion of one character, over triggerNeighbourhoodAlphabet. It reaches past
// triggerSweepMaxLen without guessing words, because a remap keyed on a prefix,
// a suffix, or a substring of an accepted class widens the set to strings that
// sit exactly here. doc_lockstep_gate_inputs_test.go reuses it for the host
// gate, which is the same shape one input over.
//
// It does not close the axis, and the limit is worth stating. A remap keyed on
// something other than the class text -- its length, say -- lands wherever it
// likes, and only the exhaustive and literal arms reach those. A second edit
// would multiply this arm by the alphabet for one more character of reach, which
// is why it stops at one.
func oneEditNeighbourhood(bases []string) []string {
	seen := map[string]bool{}
	for _, base := range bases {
		for position := 0; position <= len(base); position++ {
			for i := 0; i < len(triggerNeighbourhoodAlphabet); i++ {
				char := string(triggerNeighbourhoodAlphabet[i])
				seen[base[:position]+char+base[position:]] = true
				if position < len(base) {
					seen[base[:position]+char+base[position+1:]] = true
				}
			}
			if position < len(base) {
				seen[base[:position]+base[position+1:]] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
