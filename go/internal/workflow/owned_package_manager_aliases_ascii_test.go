// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"testing"
)

// TestCanonicalOwnedPackageEcosystemKeepsLookalikesApart pins that the raw
// package_manager mapping folds ASCII letters only (#9622): a U+0130 (İ) or
// U+212A (K) spelling keeps its bytes apart from the ASCII ecosystem instead
// of joining it the way strings.ToLower would.
func TestCanonicalOwnedPackageEcosystemKeepsLookalikesApart(t *testing.T) {
	t.Parallel()

	for _, lookalike := range []string{"PİP", "PİK", "NPM\u0130", "Gradle\u212a"} {
		if got := CanonicalOwnedPackageEcosystem(lookalike); got == "pip" || got == "npm" || got == "gradle" || got == "maven" {
			t.Errorf("CanonicalOwnedPackageEcosystem(%q) = %q, joins the ASCII ecosystem", lookalike, got)
		}
	}
	for raw, want := range map[string]string{
		"NPM":    "npm",
		"Gradle": "maven",
		"GOMOD":  "go",
	} {
		if got := CanonicalOwnedPackageEcosystem(raw); got != want {
			t.Errorf("CanonicalOwnedPackageEcosystem(%q) = %q, want %q", raw, got, want)
		}
	}
}
