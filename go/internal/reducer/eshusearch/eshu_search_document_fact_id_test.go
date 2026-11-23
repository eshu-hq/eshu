// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package eshusearch

import "testing"

// TestEshuSearchDocumentFactIDHashesScopeGenerationAndDocument is the #11214
// census proof for the search document writer. Its upsert has no owner check, so
// the claim that it cannot overwrite another scope's row rests on the id: it is
// a SHA-256 over a structured identity of scope, generation and document, so two
// writes share an id only when all three are equal. The cases pin that: a change
// in any one part changes the id, the id is deterministic, and shifting a
// delimiter between two parts cannot alias two different tuples.
func TestEshuSearchDocumentFactIDHashesScopeGenerationAndDocument(t *testing.T) {
	t.Parallel()

	base := eshuSearchDocumentFactID("scope-a", "gen-1", "doc-1")
	if base == "" || base != eshuSearchDocumentFactID("scope-a", "gen-1", "doc-1") {
		t.Fatalf("fact id %q is empty or not deterministic", base)
	}
	for name, other := range map[string]string{
		"scope":      eshuSearchDocumentFactID("scope-b", "gen-1", "doc-1"),
		"generation": eshuSearchDocumentFactID("scope-a", "gen-2", "doc-1"),
		"document":   eshuSearchDocumentFactID("scope-a", "gen-1", "doc-2"),
	} {
		if other == base {
			t.Errorf("the fact id ignores the %s: another %s would overwrite this row", name, name)
		}
	}
	if eshuSearchDocumentFactID("a|b", "c", "d") == eshuSearchDocumentFactID("a", "b|c", "d") ||
		eshuSearchDocumentFactID("a", "b", "c:d") == eshuSearchDocumentFactID("a", "b:c", "d") {
		t.Error("a delimiter shifted between two parts aliases two different (scope, generation, document) tuples")
	}
}
