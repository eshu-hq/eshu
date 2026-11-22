// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit

import (
	"fmt"
	"slices"
	"sort"
)

// CheckWaivers is the shrink-only coverage ratchet as a pure function, so a
// test can plant each failure. required lists everything that needs coverage (a
// capability that needs a probe, a package that needs a census owner), covered
// lists what has it, and waived lists what is excused. what names the subject in
// messages. It returns one problem per failure, in a stable order:
//
//   - a required subject that is neither covered nor waived;
//   - a waiver for a subject that does not exist, is already covered, or repeats;
//   - a waiver list longer than ceiling (it may only shrink), or shorter than
//     ceiling (lower the ceiling so the shrink holds).
//
// The ceiling is a code constant, so raising it is a visible edit that only
// review catches; the ratchet stops silent growth, not a deliberate one.
func CheckWaivers(what string, required, covered, waived []string, ceiling int) []string {
	var problems []string
	for _, subject := range sortedCopy(required) {
		if !slices.Contains(covered, subject) && !slices.Contains(waived, subject) {
			problems = append(problems, fmt.Sprintf("%s %q has no coverage and no waiver", what, subject))
		}
	}
	seen := map[string]struct{}{}
	for _, subject := range sortedCopy(waived) {
		if _, dup := seen[subject]; dup {
			problems = append(problems, fmt.Sprintf("waiver for %s %q is listed twice", what, subject))
		}
		seen[subject] = struct{}{}
		if !slices.Contains(required, subject) {
			problems = append(problems, fmt.Sprintf("waiver names %s %q, which does not exist", what, subject))
		}
		if slices.Contains(covered, subject) {
			problems = append(problems, fmt.Sprintf("waiver names %s %q, which now has coverage: delete the waiver", what, subject))
		}
	}
	switch {
	case len(waived) > ceiling:
		problems = append(problems, fmt.Sprintf(
			"%d %s waivers exceed the ceiling %d: a waiver list may only shrink; cover the new entry instead", len(waived), what, ceiling))
	case len(waived) < ceiling:
		problems = append(problems, fmt.Sprintf(
			"%d %s waivers are below the ceiling %d: lower the ceiling constant to %d so the shrink holds", len(waived), what, ceiling, len(waived)))
	}
	return problems
}

func sortedCopy(values []string) []string {
	out := slices.Clone(values)
	sort.Strings(out)
	return out
}
