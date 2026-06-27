// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sourcetool

import (
	"testing"
)

func TestIsValidAcceptsCanonicalTokens(t *testing.T) {
	t.Parallel()

	for _, token := range []string{"terraform", "helm", "kubernetes", "unknown"} {
		if !IsValid(token) {
			t.Errorf("IsValid(%q) = false, want true", token)
		}
	}
}

func TestIsValidRejectsUnknownTokens(t *testing.T) {
	t.Parallel()

	for _, token := range []string{"", "Terraform", "TERRAFORM", "notatool", "saltstack"} {
		if IsValid(token) {
			t.Errorf("IsValid(%q) = true, want false", token)
		}
	}
}

func TestCanonicalHasNoDuplicates(t *testing.T) {
	t.Parallel()

	seen := make(map[string]int, len(Canonical))
	for i, tok := range Canonical {
		if prev, dup := seen[tok]; dup {
			t.Errorf("Canonical[%d]=%q duplicates Canonical[%d]", i, tok, prev)
		}
		seen[tok] = i
	}
}

func TestCanonicalIncludesUnknown(t *testing.T) {
	t.Parallel()

	if !IsValid("unknown") {
		t.Error(`Canonical must include "unknown" as the explicit fallback token`)
	}
}
