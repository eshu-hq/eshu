// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// TestGoReachabilityScopeIDsGate pins the storage half of the #10095
// canonical-id gate: only canonical repository ids name a go-reachability
// scope, so a URL, a local path, a blank, or a git-scope alias never reaches
// a scope id. scannercomponent.GoReachabilityScopeID owns the canonical
// pattern; this mirror must accept exactly its language.
func TestGoReachabilityScopeIDsGate(t *testing.T) {
	t.Parallel()

	got := goReachabilityScopeIDs([]string{
		"repository:r_10095a01",
		"repository:r_10095a01",
		" https://example.com/owner/repo ",
		"repository:r_0001a055",
		"git-repository:repository:r_10095a01",
		"",
		"repository:r_SHORT",
		"repo://owner/repo",
		"repository:r_10095a01 ",
	})
	want := []string{
		"go-reachability://repository:r_0001a055",
		"go-reachability://repository:r_10095a01",
	}
	if len(got) != len(want) {
		t.Fatalf("goReachabilityScopeIDs() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("goReachabilityScopeIDs() = %q, want %q", got, want)
		}
	}
	if len(goReachabilityScopeIDs(nil)) != 0 {
		t.Fatalf("goReachabilityScopeIDs(nil) = %q, want empty", goReachabilityScopeIDs(nil))
	}
}

// TestListActiveGoReachabilityFactsQueryProbesScopeGenerationKind pins the
// #10095 probe shape: one (scope_id, generation_id, fact_kind) equality
// probe, the columns of fact_records_scope_generation_idx in order, over the
// active generations only, with no LIMIT (core evidence is never bounded).
func TestListActiveGoReachabilityFactsQueryProbesScopeGenerationKind(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.scope_id = ANY($1::text[])",
		"fact.generation_id = ANY($2::text[])",
		"fact.fact_kind = ANY($3::text[])",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"JOIN ingestion_scopes AS scope",
		"JOIN scope_generations AS generation",
		"scope.active_generation_id = fact.generation_id",
	} {
		if !strings.Contains(listActiveGoReachabilityFactsQuery, want) {
			t.Fatalf("listActiveGoReachabilityFactsQuery missing %q:\n%s", want, listActiveGoReachabilityFactsQuery)
		}
	}
	if strings.Contains(listActiveGoReachabilityFactsQuery, "LIMIT") {
		t.Fatalf("listActiveGoReachabilityFactsQuery must not bound core evidence:\n%s", listActiveGoReachabilityFactsQuery)
	}
}

// TestGoReachabilityEvidenceKindsAreTheTwoLoaderKinds pins the kind list the
// probe carries: exactly the two govulncheck evidence kinds the cross-scope
// stage loads, in chain-package spelling.
func TestGoReachabilityEvidenceKindsAreTheTwoLoaderKinds(t *testing.T) {
	t.Parallel()

	want := []string{
		chain.VulnerabilityGoModuleEvidenceFactKind,
		chain.VulnerabilityGoCallReachabilityFactKind,
	}
	got := goReachabilityEvidenceKinds()
	if len(got) != len(want) {
		t.Fatalf("goReachabilityEvidenceKinds() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("goReachabilityEvidenceKinds() = %q, want %q", got, want)
		}
	}
}
