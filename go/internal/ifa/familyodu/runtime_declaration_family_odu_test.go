// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import "testing"

// TestRuntimeDeclarationFamilyOduStableKeysAreUnique pins that every fact in
// the runtime_declaration Odù carries a distinct stable_fact_key, including
// the deliberate same-identity replay, so no fact silently overwrites another
// on ingest.
func TestRuntimeDeclarationFamilyOduStableKeysAreUnique(t *testing.T) {
	t.Parallel()

	odu := RuntimeDeclarationFamilyOdu().Odu
	if got, want := len(odu.Facts), 8; got != want {
		t.Fatalf("runtime_declaration Odù carries %d facts, want %d (1 repository + 7 declarations)", got, want)
	}
	seen := make(map[string]struct{}, len(odu.Facts))
	for _, env := range odu.Facts {
		if _, dup := seen[env.StableFactKey]; dup {
			t.Fatalf("duplicate stable_fact_key %q", env.StableFactKey)
		}
		seen[env.StableFactKey] = struct{}{}
		if env.ScopeID != runtimeDeclarationFamilyScopeID || env.GenerationID != runtimeDeclarationFamilyGenerationID {
			t.Fatalf("fact %q is outside the single scope generation", env.StableFactKey)
		}
	}
}
