// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reset

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/recovery"
)

// TestReadAffectedGenerationsCarriesIsDelta pins that the one read carries each
// covered generation's is_delta into the bound set, index-aligned (#7797).
func TestReadAffectedGenerationsCarriesIsDelta(t *testing.T) {
	t.Parallel()

	q := &readFakeQueryer{
		rows: [][3]string{
			{"scope-full", "gen-full", ""},
			{"scope-skipped", "", recovery.SkipReasonNoActiveGeneration},
			{"scope-delta", "gen-delta", ""},
		},
		deltas: []bool{false, false, true},
	}
	generations, _, err := ReadAffectedGenerations(context.Background(), q, recovery.RefinalizeFilter{AllScopes: true})
	if err != nil {
		t.Fatalf("ReadAffectedGenerations() error = %v, want nil", err)
	}
	if got, want := generations.IsDelta, []bool{false, true}; !slices.Equal(got, want) {
		t.Fatalf("IsDelta = %v, want %v (aligned with ScopeIDs %v)", got, want, generations.ScopeIDs)
	}
	if len(generations.IsDelta) != len(generations.ScopeIDs) {
		t.Fatalf("IsDelta has %d entries for %d scopes", len(generations.IsDelta), len(generations.ScopeIDs))
	}
}

// TestGenerationsDeltaActiveClassifiesReindexableScopes pins which delta scopes
// a reindex watermark can force: only git default-branch scopes. A ref scope,
// another collector's scope, and a bare prefix are reported as unsupported,
// and a full generation is not reported at all.
func TestGenerationsDeltaActiveClassifiesReindexableScopes(t *testing.T) {
	t.Parallel()

	var g Generations
	g.AppendSelected("git-repository-scope:repo-a", "gen-a", true)
	g.Append("git-repository-scope:repo-full", "gen-full")
	g.AppendSelected("git-repository-scope:repo-b@feature", "gen-b", true)
	g.AppendSelected("other-collector-scope:repo-c", "gen-c", true)
	g.AppendSelected("git-repository-scope:", "gen-empty", true)

	delta, reindexable := g.DeltaActive()
	if got, want := reindexable, []string{"git-repository-scope:repo-a"}; !slices.Equal(got, want) {
		t.Fatalf("reindexable = %v, want %v", got, want)
	}
	got := make([]string, 0, len(delta))
	for _, d := range delta {
		got = append(got, d.ScopeID+"|"+d.GenerationID+"|"+d.Outcome)
	}
	want := []string{
		"git-repository-scope:repo-a|gen-a|" + recovery.DeltaActiveOutcomeReindexRequested,
		"git-repository-scope:repo-b@feature|gen-b|" + recovery.DeltaActiveOutcomeReindexUnsupported,
		"other-collector-scope:repo-c|gen-c|" + recovery.DeltaActiveOutcomeReindexUnsupported,
		"git-repository-scope:|gen-empty|" + recovery.DeltaActiveOutcomeReindexUnsupported,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("delta-active pairs = %v, want %v", got, want)
	}
}

// TestAffectedGenerationsQueryReturnsIsDelta keeps the read's column list in
// step with the four-column scan.
func TestAffectedGenerationsQueryReturnsIsDelta(t *testing.T) {
	t.Parallel()

	query, _ := AffectedGenerationsQuery(recovery.RefinalizeFilter{AllScopes: true})
	for _, fragment := range []string{"AS is_delta", "active.is_delta", "newest.is_delta", "g.is_delta"} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("AffectedGenerationsQuery is missing %q:\n%s", fragment, query)
		}
	}
}
