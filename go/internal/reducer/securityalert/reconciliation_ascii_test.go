// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

import (
	"testing"
)

// TestNormalizedSecurityAlertEcosystemKeepsLookalikesApart pins that the
// reconciliation fallback folds ASCII letters only (#9622): a U+0130 (İ) or
// U+212A (K) spelling is not conflated with an ASCII ecosystem the way
// strings.ToLower would conflate it.
func TestNormalizedSecurityAlertEcosystemKeepsLookalikesApart(t *testing.T) {
	t.Parallel()

	for _, lookalike := range []string{"PİP", "NPM\u0130", "MAVEN\u212a"} {
		if got := normalizedSecurityAlertEcosystem(lookalike); got == "pip" || got == "npm" || got == "maven" {
			t.Errorf("normalizedSecurityAlertEcosystem(%q) = %q, joins the ASCII ecosystem", lookalike, got)
		}
	}
	if got := normalizedSecurityAlertEcosystem("NPM"); got != "npm" {
		t.Errorf("normalizedSecurityAlertEcosystem(%q) = %q, want %q", "NPM", got, "npm")
	}
}
