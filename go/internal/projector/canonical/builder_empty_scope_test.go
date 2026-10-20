// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq
package canonical

// Empty-scope marker contract for eshu-hq/eshu#8668 (escape hatch for the
// #8657 writer-side conditional retract).
//
// A scope that is genuinely fileless declares it on its repository fact
// with the `empty_scope` payload key (same in-band declaration pattern as
// `delta_generation` / `reconciliation_generation` in delta.go). The builder
// carries the declaration on CanonicalMaterialization.DeclaredEmptyScope so
// the writer-side retract check can tell "collector observed zero files"
// from "file facts went missing downstream". The declaration excuses only
// builds that carry zero file facts: live file facts that drop still fail
// loud through rejectSilentFilelessWipe, marker or not.

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func emptyScopeRepositoryFact() facts.Envelope {
	fact := filelessGuardRepositoryFact()
	fact.Payload["empty_scope"] = true
	return fact
}

// A marked-empty scope (declaration, zero file facts) on a non-first,
// non-delta generation builds successfully and carries the declaration,
// so the writer-side retract check can honor it.
func TestBuildMaterializationMarkedEmptyScopeSucceedsWithDeclaration(t *testing.T) {
	t.Parallel()

	input := []facts.Envelope{emptyScopeRepositoryFact()}

	mat, _, err := BuildMaterialization(filelessGuardScope(), testGeneration(), input)
	if err != nil {
		t.Fatalf("BuildMaterialization() error = %v, want nil; a declared-empty scope is genuine filelessness, not a silent drop", err)
	}
	if !mat.DeclaredEmptyScope {
		t.Fatal("BuildMaterialization() DeclaredEmptyScope = false, want true; the writer-side retract check needs the declaration carried")
	}
	if len(mat.Files) != 0 {
		t.Fatalf("len(Files) = %d, want 0", len(mat.Files))
	}
}

// An unmarked zero-file-facts scope still builds on current main (clean
// zero-facts builds succeed by design), but carries no declaration: the
// writer-side retract check must treat it as ambiguous, not as declared
// empty. This pins the exact gap the #8657 writer check closes.
func TestBuildMaterializationUnmarkedZeroFactsScopeCarriesNoDeclaration(t *testing.T) {
	t.Parallel()

	input := []facts.Envelope{filelessGuardRepositoryFact()}

	mat, _, err := BuildMaterialization(filelessGuardScope(), testGeneration(), input)
	if err != nil {
		t.Fatalf("BuildMaterialization() error = %v, want nil; clean zero-facts builds succeed by design", err)
	}
	if mat.DeclaredEmptyScope {
		t.Fatal("BuildMaterialization() DeclaredEmptyScope = true, want false; no declaration was made")
	}
}

// The declaration excuses only zero-file-fact builds. Live file facts that
// drop contradict the declaration (the collector claimed emptiness yet
// emitted file facts), so the build still fails loud naming the facts.
func TestBuildMaterializationMarkerDoesNotExcuseDroppedLiveFacts(t *testing.T) {
	t.Parallel()

	input := []facts.Envelope{
		emptyScopeRepositoryFact(),
		filelessGuardFileFact("file-dropped-1", false),
	}

	_, _, err := BuildMaterialization(filelessGuardScope(), testGeneration(), input)
	if err == nil {
		t.Fatal("BuildMaterialization() error = nil, want loud failure; dropped live facts contradict the empty-scope declaration")
	}
}

func TestExtractDeclaredEmptyScope(t *testing.T) {
	t.Parallel()

	marked := emptyScopeRepositoryFact()
	unmarked := filelessGuardRepositoryFact()
	explicitFalse := filelessGuardRepositoryFact()
	explicitFalse.Payload["empty_scope"] = false
	nonBool := filelessGuardRepositoryFact()
	nonBool.Payload["empty_scope"] = "yes"
	stringTrue := filelessGuardRepositoryFact()
	stringTrue.Payload["empty_scope"] = "true"

	cases := []struct {
		name  string
		input []facts.Envelope
		want  bool
	}{
		{"no facts", nil, false},
		{"no repository fact", []facts.Envelope{filelessGuardFileFact("f-1", true)}, false},
		{"absent key", []facts.Envelope{unmarked}, false},
		{"explicit false", []facts.Envelope{explicitFalse}, false},
		{"unparseable value", []facts.Envelope{nonBool}, false},
		{"declared", []facts.Envelope{marked}, true},
		// PayloadBoolPtr seam parity with the delta keys: a
		// parseable-string "true" declares.
		{"string true declares", []facts.Envelope{stringTrue}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := extractDeclaredEmptyScope(tc.input); got != tc.want {
				t.Fatalf("extractDeclaredEmptyScope() = %v, want %v", got, tc.want)
			}
		})
	}
}
