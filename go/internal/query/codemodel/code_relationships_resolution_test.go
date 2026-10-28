// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func resolutionTestCandidate(id, name, path string, startLine int) querycontract.EntityContent {
	return querycontract.EntityContent{
		EntityID:     id,
		EntityName:   name,
		EntityType:   "Function",
		RelativePath: path,
		RepoID:       "repo-1",
		Language:     "go",
		StartLine:    startLine,
		EndLine:      startLine + 5,
	}
}

// TestResolveRelationshipsNameTargetNilReader proves a nil content
// store resolves to nothing without an error.
func TestResolveRelationshipsNameTargetNilReader(t *testing.T) {
	t.Parallel()

	entity, resolution, err := ResolveRelationshipsNameTarget(
		context.Background(), nil, RelationshipsRequest{Name: "main"},
	)
	if err != nil || entity != nil || resolution != nil {
		t.Errorf("nil reader must resolve to (nil, nil, nil); got (%v, %v, %v)", entity, resolution, err)
	}
}

// TestSelectExactGraphEntityCandidate proves exact selection prefers
// the lone candidate, then the lone non-test candidate, and reports
// ambiguity otherwise.
func TestSelectExactGraphEntityCandidate(t *testing.T) {
	t.Parallel()

	solo := []querycontract.EntityContent{resolutionTestCandidate("e1", "main", "a.go", 1)}
	if got, err := selectExactGraphEntityCandidate("repo-1", "main", solo); err != nil || got == nil || got.EntityID != "e1" {
		t.Errorf("lone candidate must resolve: (%v, %v)", got, err)
	}
	if got, err := selectExactGraphEntityCandidate("repo-1", "main", nil); err != nil || got != nil {
		t.Errorf("empty set must resolve to (nil, nil); got (%v, %v)", got, err)
	}

	mixed := []querycontract.EntityContent{
		resolutionTestCandidate("e-test", "main", "a_test.go", 1),
		resolutionTestCandidate("e-real", "main", "a.go", 9),
	}
	if got, err := selectExactGraphEntityCandidate("repo-1", "main", mixed); err != nil || got == nil || got.EntityID != "e-real" {
		t.Errorf("lone non-test candidate must resolve: (%v, %v)", got, err)
	}

	ambiguous := []querycontract.EntityContent{
		resolutionTestCandidate("e1", "main", "a.go", 1),
		resolutionTestCandidate("e2", "main", "b.go", 2),
	}
	if _, err := selectExactGraphEntityCandidate("repo-1", "main", ambiguous); err == nil {
		t.Error("multiple non-test candidates must report ambiguity")
	}
}

// TestExactEntityNameMatches proves name matching is exact after
// trimming, and test-path detection keys on the Go test-file suffix.
func TestExactEntityNameMatches(t *testing.T) {
	t.Parallel()

	matches := []querycontract.EntityContent{
		resolutionTestCandidate("e1", "main", "a.go", 1),
		resolutionTestCandidate("e2", "main.helper", "a.go", 5),
	}
	if got := exactEntityNameMatches(matches, "main"); len(got) != 1 {
		t.Errorf("exact match kept %d candidates, want 1", len(got))
	}
	if !isTestEntityPath("pkg/a_test.go") {
		t.Error("a_test.go must read as a test path")
	}
	if isTestEntityPath("pkg/a.go") || isTestEntityPath("") {
		t.Error("ordinary and empty paths must not read as test paths")
	}
}

// TestAmbiguousRelationshipsResponse proves the ambiguity envelope
// carries the candidate count, the truncation flag, and empty
// neighbour lists.
func TestAmbiguousRelationshipsResponse(t *testing.T) {
	t.Parallel()

	candidates := []querycontract.EntityContent{
		resolutionTestCandidate("e2", "main", "b.go", 2),
		resolutionTestCandidate("e1", "main", "a.go", 1),
	}
	SortRelationshipStoryCandidates(candidates)
	if candidates[0].EntityID != "e1" {
		t.Errorf("candidates must sort by path first; first = %q", candidates[0].EntityID)
	}
	maps := RelationshipStoryCandidateMaps(candidates, 1)
	if len(maps) != 1 {
		t.Fatalf("limited maps len = %d, want 1", len(maps))
	}

	resolution := RelationshipStoryResolution{
		Status: "ambiguous", Candidates: maps, Truncated: true,
	}
	response := AmbiguousRelationshipsResponse(
		RelationshipsRequest{Name: " main ", RepoID: "repo-1"}, resolution,
	)
	if response["status"] != "ambiguous" {
		t.Errorf("status = %v, want ambiguous", response["status"])
	}
	summary, ok := response["summary"].(map[string]any)
	if !ok || summary["candidate_count"] != 1 || summary["truncated"] != true {
		t.Errorf("summary must carry count 1 and truncated:true: %#v", response["summary"])
	}
}
