// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import (
	"slices"
	"testing"
)

func TestKnownWarningCodesMatchesPredicateAndReturnsCopy(t *testing.T) {
	t.Parallel()

	want := []string{
		WarningCodeUnsupportedReferrersAPI,
		WarningCodeComputedManifestDigest,
		WarningCodeConfigBlobUnavailable,
		WarningCodeConfigBlobOversized,
		WarningCodeTagListTruncated,
		WarningCodeMissingManifestDigest,
	}
	got := KnownWarningCodes()
	if !slices.Equal(got, want) {
		t.Fatalf("KnownWarningCodes() = %v, want %v", got, want)
	}
	for _, warningCode := range got {
		if !IsKnownWarningCode(warningCode) {
			t.Fatalf("IsKnownWarningCode(%q) = false, want true", warningCode)
		}
	}
	got[0] = "mutated"
	if slices.Equal(KnownWarningCodes(), got) {
		t.Fatal("KnownWarningCodes() returned shared mutable storage")
	}
	if IsKnownWarningCode("future_completeness_warning") {
		t.Fatal("IsKnownWarningCode(unknown) = true, want false")
	}
}
