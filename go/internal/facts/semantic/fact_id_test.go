// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semantic

import "testing"

// TestScopedFactIDSeparatesScopeAndGeneration pins the #11203 contract: the
// fact_records primary key is fact_id alone, so an id that omits the scope or
// the generation lets one scope's write land on another scope's row.
func TestScopedFactIDSeparatesScopeAndGeneration(t *testing.T) {
	t.Parallel()

	base := ScopedFactID(SemanticCodeHintFactKind, "sha256:key", "scope-a", "gen-1")
	if base == "" || base == "sha256:key" {
		t.Fatalf("ScopedFactID() = %q, want a non-empty id distinct from the stable key", base)
	}
	if again := ScopedFactID(SemanticCodeHintFactKind, "sha256:key", "scope-a", "gen-1"); again != base {
		t.Fatalf("ScopedFactID() = %q then %q, want deterministic", base, again)
	}
	for name, other := range map[string]string{
		"scope":      ScopedFactID(SemanticCodeHintFactKind, "sha256:key", "scope-b", "gen-1"),
		"generation": ScopedFactID(SemanticCodeHintFactKind, "sha256:key", "scope-a", "gen-2"),
		"stable key": ScopedFactID(SemanticCodeHintFactKind, "sha256:key2", "scope-a", "gen-1"),
		"fact kind":  ScopedFactID(SemanticDocumentationObservationFactKind, "sha256:key", "scope-a", "gen-1"),
	} {
		if other == base {
			t.Errorf("ScopedFactID() ignores %s: %q", name, other)
		}
	}
	// A delimiter-shift must not alias two different (scope, generation) pairs.
	if ScopedFactID(SemanticCodeHintFactKind, "k", "a|b", "c") == ScopedFactID(SemanticCodeHintFactKind, "k", "a", "b|c") {
		t.Error("ScopedFactID() aliases (a|b, c) with (a, b|c)")
	}
}
