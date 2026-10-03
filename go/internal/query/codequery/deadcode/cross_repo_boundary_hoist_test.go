// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode_test

import (
	"testing"
)

func hoistedCitations(t *testing.T, data map[string]any) []string {
	t.Helper()

	raw, ok := data["boundary_consumer_evidence"].([]any)
	if !ok {
		t.Fatalf("boundary_consumer_evidence type = %T, want []any", data["boundary_consumer_evidence"])
	}
	citations := make([]string, 0, len(raw))
	for _, item := range raw {
		citations = append(citations, item.(map[string]any)["citation"].(string))
	}
	return citations
}

// TestCrossRepoDeadCodeBoundaryEvidenceIsHoistedOnce pins the #7129 shape: the
// repository-boundary evidence every fallback row used to repeat is returned
// once in data.boundary_consumer_evidence, and the fallback rows keep an empty
// consumer_evidence array that says where their evidence went.
func TestCrossRepoDeadCodeBoundaryEvidenceIsHoistedOnce(t *testing.T) {
	t.Parallel()

	relationships := []map[string]any{boundaryRelationship("consumer-1"), boundaryRelationship("consumer-2")}
	data := postBoundaryHoistRequest(t, boundaryHoistStore(relationships), `{"repo_id":"repo-producer","limit":10}`, nil)

	got := hoistedCitations(t, data)
	want := []string{
		"repository_relationships:relationship-generation-1/resolved-consumer-1",
		"repository_relationships:relationship-generation-1/resolved-consumer-2",
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("boundary_consumer_evidence citations = %v, want %v", got, want)
	}
	if data["boundary_consumer_evidence_count"] != float64(2) {
		t.Fatalf("boundary_consumer_evidence_count = %#v, want 2", data["boundary_consumer_evidence_count"])
	}

	buckets := data["candidate_buckets"].(map[string]any)
	for _, id := range []string{"p-boundary-a", "p-boundary-b"} {
		row := assertCrossRepoDeadCodeBucketEntity(t, buckets, "unknown", id)
		evidence, ok := row["consumer_evidence"].([]any)
		if !ok || len(evidence) != 0 {
			t.Fatalf("%s consumer_evidence = %#v, want an empty array (the evidence is hoisted)", id, row["consumer_evidence"])
		}
		if row["consumer_evidence_source"] != "repository_boundary" {
			t.Fatalf("%s consumer_evidence_source = %#v, want repository_boundary", id, row["consumer_evidence_source"])
		}
		if row["consumer_evidence_count"] != float64(0) {
			t.Fatalf("%s consumer_evidence_count = %#v, want 0 (the row's own list)", id, row["consumer_evidence_count"])
		}
	}

	entity := assertCrossRepoDeadCodeBucketEntity(t, buckets, "live_by_consumer", "p-entity")
	if entity["consumer_evidence_source"] != "entity" {
		t.Fatalf("p-entity consumer_evidence_source = %#v, want entity", entity["consumer_evidence_source"])
	}
	if entity["consumer_evidence_count"] != float64(1) {
		t.Fatalf("p-entity consumer_evidence_count = %#v, want 1", entity["consumer_evidence_count"])
	}
	assertCrossRepoDeadCodeEvidenceCitation(t, entity, "code_reachability_rows:scope-a/gen-a/consumer-1/consumer-root/p-entity")
}

// TestCrossRepoDeadCodeBoundaryHoistFollowsGrantAndSelector pins that the
// hoisted list is the same filtered list each row used to get: a consumer
// selector and a scoped grant narrow it, and the hidden count stays per row.
func TestCrossRepoDeadCodeBoundaryHoistFollowsGrantAndSelector(t *testing.T) {
	t.Parallel()

	relationships := []map[string]any{boundaryRelationship("consumer-1"), boundaryRelationship("consumer-2")}
	oneCitation := "repository_relationships:relationship-generation-1/resolved-consumer-1"

	selected := postBoundaryHoistRequest(t, boundaryHoistStore(relationships),
		`{"repo_id":"repo-producer","consumer_repo_ids":["consumer-1"],"limit":10}`, nil)
	if got := hoistedCitations(t, selected); len(got) != 1 || got[0] != oneCitation {
		t.Fatalf("selector-narrowed boundary = %v, want [%s]", got, oneCitation)
	}

	scoped := postBoundaryHoistRequest(t, boundaryHoistStore(relationships),
		`{"repo_id":"repo-producer","limit":10}`, []string{"repo-producer", "consumer-1"})
	if got := hoistedCitations(t, scoped); len(got) != 1 || got[0] != oneCitation {
		t.Fatalf("grant-narrowed boundary = %v, want [%s]", got, oneCitation)
	}
	if scoped["boundary_consumer_evidence_count"] != float64(1) {
		t.Fatalf("scoped boundary_consumer_evidence_count = %#v, want 1", scoped["boundary_consumer_evidence_count"])
	}
	row := assertCrossRepoDeadCodeBucketEntity(t, scoped["candidate_buckets"].(map[string]any), "unknown", "p-boundary-a")
	if row["hidden_consumer_evidence_count"] != float64(1) {
		t.Fatalf("hidden_consumer_evidence_count = %#v, want 1 on the row", row["hidden_consumer_evidence_count"])
	}
}

// TestCrossRepoDeadCodeWithoutBoundaryReportsEmptyHoist pins the always-present
// keys: a repository with no boundary relationships still answers an empty
// list and a zero count, and a row nothing backs is not labelled boundary.
func TestCrossRepoDeadCodeWithoutBoundaryReportsEmptyHoist(t *testing.T) {
	t.Parallel()

	data := postBoundaryHoistRequest(t, boundaryHoistStore(nil), `{"repo_id":"repo-producer","limit":10}`, nil)
	if got := hoistedCitations(t, data); len(got) != 0 {
		t.Fatalf("boundary_consumer_evidence = %v, want empty", got)
	}
	if data["boundary_consumer_evidence_count"] != float64(0) {
		t.Fatalf("boundary_consumer_evidence_count = %#v, want 0", data["boundary_consumer_evidence_count"])
	}
	row := assertCrossRepoDeadCodeBucketEntity(t, data["candidate_buckets"].(map[string]any), "dead", "p-boundary-a")
	if row["consumer_evidence_source"] != "entity" {
		t.Fatalf("dead row consumer_evidence_source = %#v, want entity (no boundary evidence backed it)", row["consumer_evidence_source"])
	}
}
