// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeprovenance

import "testing"

func TestConfidenceMatchesADRTierTable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		method Method
		want   float64
	}{
		{MethodSCIP, 0.99},
		{MethodDeclared, 0.95},
		{MethodSameFile, 0.95},
		{MethodImportBinding, 0.90},
		{MethodTypeInferred, 0.80},
		{MethodScopeUniqueName, 0.70},
		{MethodCrossRepoExportPackage, 0.70},
		{MethodRepoUniqueName, 0.50},
	}
	for _, tc := range cases {
		if got := Confidence(tc.method); got != tc.want {
			t.Errorf("Confidence(%q) = %v, want %v", tc.method, got, tc.want)
		}
	}
}

func TestConfidenceUnspecifiedAndUnknownFallBackToLegacy(t *testing.T) {
	t.Parallel()

	if got := Confidence(MethodUnspecified); got != LegacyConfidence {
		t.Errorf("Confidence(unspecified) = %v, want %v", got, LegacyConfidence)
	}
	if got := Confidence("not_a_method"); got != LegacyConfidence {
		t.Errorf("Confidence(unknown) = %v, want %v", got, LegacyConfidence)
	}
}

func TestValidAndClassified(t *testing.T) {
	t.Parallel()

	classified := []Method{
		MethodSCIP, MethodDeclared, MethodSameFile, MethodImportBinding,
		MethodTypeInferred, MethodScopeUniqueName, MethodCrossRepoExportPackage,
		MethodRepoUniqueName,
	}
	for _, method := range classified {
		if !Valid(method) {
			t.Errorf("Valid(%q) = false, want true", method)
		}
		if !Classified(method) {
			t.Errorf("Classified(%q) = false, want true", method)
		}
	}

	if !Valid(MethodUnspecified) {
		t.Error("Valid(unspecified) = false, want true")
	}
	if Classified(MethodUnspecified) {
		t.Error("Classified(unspecified) = true, want false")
	}
	if Valid("not_a_method") {
		t.Error("Valid(unknown) = true, want false")
	}
	if Classified("not_a_method") {
		t.Error("Classified(unknown) = true, want false")
	}
}

func TestReasonIsNonEmptyForEveryVocabularyValue(t *testing.T) {
	t.Parallel()

	all := []Method{
		MethodSCIP, MethodDeclared, MethodSameFile, MethodImportBinding,
		MethodTypeInferred, MethodScopeUniqueName, MethodCrossRepoExportPackage,
		MethodRepoUniqueName, MethodUnspecified,
	}
	for _, method := range all {
		if Reason(method) == "" {
			t.Errorf("Reason(%q) = \"\", want non-empty", method)
		}
	}
	if Reason("not_a_method") != reasonByMethod[MethodUnspecified] {
		t.Error("Reason(unknown) did not fall back to the unspecified reason")
	}
}

// TestEveryConfidenceTierHasAReason guards the closed-set invariant: the
// confidence table and reason table cover the same classified vocabulary.
func TestEveryConfidenceTierHasAReason(t *testing.T) {
	t.Parallel()

	for method := range confidenceByMethod {
		if _, ok := reasonByMethod[method]; !ok {
			t.Errorf("method %q has a confidence tier but no reason", method)
		}
	}
}
