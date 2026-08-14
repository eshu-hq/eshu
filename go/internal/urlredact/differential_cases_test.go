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
	for _, tc := range cases {
		if _, dup := seen[tc.Name]; dup {
			t.Errorf("duplicate row name %q", tc.Name)
		}
		seen[tc.Name] = struct{}{}

		if len(tc.Secrets) == 0 {
			t.Errorf("row %q declares no fragment, so it compares nothing", tc.Name)
		}
		for _, secret := range tc.Secrets {
			if !strings.Contains(tc.Input, secret) {
				t.Errorf("row %q declares a fragment that is not in its input", tc.Name)
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
}

// TestCheckAgreementFailsBothDirections breaks the check on purpose. A
// comparison that cannot go red is the defect this package keeps finding in its
// own guards, so the assertion is exercised rather than assumed: one direction
// proves an unrecorded disagreement fails, the other proves a recorded
// exemption that stopped being true fails too.
func TestCheckAgreementFailsBothDirections(t *testing.T) {
	t.Parallel()

	unexempted := DifferentialCase{Secrets: []string{Sentinel}}
	if err := unexempted.CheckAgreement("clean", Sentinel); err == nil {
		t.Error("a walk keeping a fragment the other removed passed with no exemption recorded")
	}
	if err := unexempted.CheckAgreement("clean", "clean"); err != nil {
		t.Errorf("two agreeing walks failed: %v", err)
	}

	exempted := DifferentialCase{Secrets: []string{Sentinel}, WalksDisagree: "recorded"}
	if err := exempted.CheckAgreement("clean", "clean"); err == nil {
		t.Error("a stale exemption passed, so the differential could quietly permit more than it says")
	}
	if err := exempted.CheckAgreement("clean", Sentinel); err != nil {
		t.Errorf("a live exemption failed: %v", err)
	}
}
