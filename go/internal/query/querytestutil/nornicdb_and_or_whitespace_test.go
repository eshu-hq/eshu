// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"fmt"
	"testing"
)

// recordingT is a cypherAssertionT double that records whether Fatalf ran,
// instead of actually failing the test binary. testing.TB cannot be
// implemented outside package testing (it carries an unexported method), so
// AssertCypherHasNoBrokenAndOr's parameter is the narrower cypherAssertionT
// interface specifically so a seeded-violation test can observe a RED case
// fail without the RED case failing THIS test run.
type recordingT struct {
	failed  bool
	message string
}

func (r *recordingT) Helper() {}

func (r *recordingT) Fatalf(format string, args ...any) {
	r.failed = true
	r.message = fmt.Sprintf(format, args...)
}

// TestAssertCypherHasNoBrokenAndOrSeededViolations is the repository's
// required seeded-violation RED/GREEN pair for a new guard (root
// CLAUDE.md's "MUST have a seeded-violation RED/GREEN pair" rule, #6786
// review follow-up F4): every RED case below must make
// AssertCypherHasNoBrokenAndOr fail, and every GREEN case must leave it
// silent.
func TestAssertCypherHasNoBrokenAndOrSeededViolations(t *testing.T) {
	t.Parallel()

	redCases := []string{
		// The original motivating shape: a tab directly before AND.
		"WHERE a = $x\n\tAND b = $y",
		// A bare newline directly before OR, no tab at all.
		"WHERE a = $x\nOR b = $y",
		// A space THEN a tab before AND: the character immediately before
		// AND is still the tab, not the space, so this is still broken.
		"WHERE a = $x\n \tAND b = $y",
	}
	for _, cypher := range redCases {
		t.Run(cypher, func(t *testing.T) {
			t.Parallel()
			rec := &recordingT{}
			AssertCypherHasNoBrokenAndOr(rec, cypher)
			if !rec.failed {
				t.Fatalf("AssertCypherHasNoBrokenAndOr did not fail for a broken AND/OR: %q", cypher)
			}
		})
	}

	greenCases := []string{
		// A tab THEN a space before AND: the character immediately before
		// AND is the space, so this is the safe shape.
		"WHERE a = $x\n\t AND b = $y",
		// Two spaces before AND.
		"WHERE a = $x\n  AND b = $y",
		// ORDER BY starts with "OR" but is not the OR keyword: the pattern's
		// trailing \b must not match inside a longer identifier.
		"MATCH (w:Workload) WHERE x\n\tORDER BY w.id",
		// OPTIONAL MATCH does not contain AND/OR at all.
		"MATCH (w:Workload) WHERE x\n\tOPTIONAL MATCH (y)",
	}
	for _, cypher := range greenCases {
		t.Run(cypher, func(t *testing.T) {
			t.Parallel()
			rec := &recordingT{}
			AssertCypherHasNoBrokenAndOr(rec, cypher)
			if rec.failed {
				t.Fatalf("AssertCypherHasNoBrokenAndOr() failed for a safe string: %q (message: %s)", cypher, rec.message)
			}
		})
	}
}
