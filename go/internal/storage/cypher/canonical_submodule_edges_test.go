// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestBatchCanonicalSubmodulePinEdgeCypherShape(t *testing.T) {
	t.Parallel()

	cypher := batchCanonicalSubmodulePinEdgeCypher
	if !strings.Contains(cypher, "MERGE (parent:Repository {id: row.parent_repo_id})") {
		t.Errorf("template missing parent Repository MERGE: %q", cypher)
	}
	if !strings.Contains(cypher, "MERGE (target:Repository {id: row.resolved_repo_id})") {
		t.Errorf("template missing target Repository MERGE: %q", cypher)
	}
	if !strings.Contains(cypher, "MERGE (parent)-[rel:PINS_SUBMODULE") {
		t.Errorf("template missing PINS_SUBMODULE edge MERGE: %q", cypher)
	}
	if !strings.Contains(cypher, "->(target)") {
		t.Errorf("template does not close the relationship onto target: %q", cypher)
	}
	// The relationship MERGE key must include path — not just the
	// (parent, target) endpoints — or a repo pinning the same target at two
	// paths (or different targets at different paths) would collapse onto a
	// single rebound relationship.
	if !strings.Contains(cypher, "path: row.submodule_path") {
		t.Errorf("relationship MERGE must key on path to keep parallel submodule pins distinct: %q", cypher)
	}
}

func TestBuildSubmodulePinRowMap(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"parent_repo_id":   "repo-parent",
		"resolved_repo_id": "repo-target",
		"submodule_path":   "vendor/lib-a",
		"pinned_sha":       "abc123",
		"generation_id":    "gen-1",
	}
	cypher, rowMap, ok := buildSubmodulePinRowMap(payload, "reducer/submodule")
	if !ok {
		t.Fatal("buildSubmodulePinRowMap ok = false, want true")
	}
	if cypher != batchCanonicalSubmodulePinEdgeCypher {
		t.Errorf("cypher = %q, want the submodule pin edge template", cypher)
	}
	want := map[string]any{
		"parent_repo_id":   "repo-parent",
		"resolved_repo_id": "repo-target",
		"submodule_path":   "vendor/lib-a",
		"pinned_sha":       "abc123",
		"generation_id":    "gen-1",
		"evidence_source":  "reducer/submodule",
	}
	if !reflect.DeepEqual(rowMap, want) {
		t.Errorf("rowMap = %#v, want %#v", rowMap, want)
	}
}

// TestBuildSubmodulePinRowMapOmitsUnknownPinnedSHA proves a fact with no
// gitlink (PinnedSHA unknown) omits the pinned_sha key entirely rather than
// setting it to an empty string, so the Cypher SET clears any stale property
// via a null rather than writing a misleading empty string.
func TestBuildSubmodulePinRowMapOmitsUnknownPinnedSHA(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"parent_repo_id":   "repo-parent",
		"resolved_repo_id": "repo-target",
		"submodule_path":   "vendor/lib-a",
		"generation_id":    "gen-1",
	}
	_, rowMap, ok := buildSubmodulePinRowMap(payload, "reducer/submodule")
	if !ok {
		t.Fatal("buildSubmodulePinRowMap ok = false, want true")
	}
	if _, present := rowMap["pinned_sha"]; present {
		t.Errorf("rowMap[pinned_sha] = %#v, want absent", rowMap["pinned_sha"])
	}
}

func TestBuildSubmodulePinRowMapRequiresMergeKeys(t *testing.T) {
	t.Parallel()

	base := map[string]any{
		"parent_repo_id":   "repo-parent",
		"resolved_repo_id": "repo-target",
		"submodule_path":   "vendor/lib-a",
	}
	for _, missing := range []string{"parent_repo_id", "resolved_repo_id", "submodule_path"} {
		payload := map[string]any{}
		for k, v := range base {
			if k != missing {
				payload[k] = v
			}
		}
		if _, _, ok := buildSubmodulePinRowMap(payload, "src"); ok {
			t.Errorf("missing %q should be rejected, got ok=true", missing)
		}
	}
}

func TestBuildRetractSubmodulePinEdges(t *testing.T) {
	t.Parallel()

	stmt := BuildRetractSubmodulePinEdges([]string{"repo-parent"}, "reducer/submodule")
	if !strings.Contains(stmt.Cypher, "rel:PINS_SUBMODULE") {
		t.Errorf("retract cypher missing PINS_SUBMODULE: %q", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "parent.id IN $repo_ids") {
		t.Errorf("retract cypher is not repo-scoped: %q", stmt.Cypher)
	}
	repoIDs, ok := stmt.Parameters["repo_ids"].([]string)
	if !ok || !reflect.DeepEqual(repoIDs, []string{"repo-parent"}) {
		t.Errorf("repo_ids = %#v, want [repo-parent]", stmt.Parameters["repo_ids"])
	}
}

// TestBuildRetractSubmodulePinEdgesAnchorsOnParentRepo proves the retract
// anchors on the parent Repository's id (never an unanchored path match),
// mirroring the #5419 P1 fix for codeowners: a repository's PINS_SUBMODULE
// retract must never leak into another repository's edges.
func TestBuildRetractSubmodulePinEdgesAnchorsOnParentRepo(t *testing.T) {
	t.Parallel()

	stmt := BuildRetractSubmodulePinEdges([]string{"repo-A"}, "reducer/submodule")
	if !strings.Contains(stmt.Cypher, "(parent:Repository)-[rel:PINS_SUBMODULE]->(:Repository)") {
		t.Errorf("retract cypher does not anchor the MATCH on parent Repository: %q", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "parent.id IN $repo_ids") {
		t.Errorf("retract cypher is not repo-scoped: %q", stmt.Cypher)
	}

	repoIDs, ok := stmt.Parameters["repo_ids"].([]string)
	if !ok || !reflect.DeepEqual(repoIDs, []string{"repo-A"}) {
		t.Fatalf("repo_ids = %#v, want [repo-A]", stmt.Parameters["repo_ids"])
	}
	for _, id := range repoIDs {
		if id == "repo-B" {
			t.Fatalf("repo_ids leaked repo-B into repo-A's retract scope: %#v", repoIDs)
		}
	}
}

func TestEdgeWriterRetractEdgesSubmodulePinWholeRepoScope(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-parent"},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainSubmodulePinEdges, rows, "reducer/submodule")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	stmt := executor.calls[0]
	if !strings.Contains(stmt.Cypher, "parent.id IN $repo_ids") {
		t.Fatalf("whole-repo retract cypher = %q, want repo_ids filter", stmt.Cypher)
	}
	repoIDs, ok := stmt.Parameters["repo_ids"].([]string)
	if !ok || !reflect.DeepEqual(repoIDs, []string{"repo-parent"}) {
		t.Fatalf("repo_ids = %#v, want [repo-parent]", stmt.Parameters["repo_ids"])
	}
}

// TestEdgeWriterRetractEdgesSubmodulePinRepoIDsAreGeneric proves the repo_ids
// collection for this domain's retract is not hardcoded to a single
// repository: a batch spanning two repositories' rows must bind both repo
// ids into the one emitted statement's repo_ids, deduped and derived from the
// rows actually present.
func TestEdgeWriterRetractEdgesSubmodulePinRepoIDsAreGeneric(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-1"},
		{IntentID: "i2", RepositoryID: "repo-2"},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainSubmodulePinEdges, rows, "reducer/submodule")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}

	stmt := executor.calls[0]
	repoIDs, ok := stmt.Parameters["repo_ids"].([]string)
	if !ok || !reflect.DeepEqual(repoIDs, []string{"repo-1", "repo-2"}) {
		t.Fatalf("repo_ids = %#v, want [repo-1 repo-2]", stmt.Parameters["repo_ids"])
	}
}

func TestEdgeWriterWriteEdgesSubmodulePinRoutesTemplate(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-parent",
			Payload: map[string]any{
				"parent_repo_id":   "repo-parent",
				"resolved_repo_id": "repo-target",
				"submodule_path":   "vendor/lib-a",
				"pinned_sha":       "abc123",
				"generation_id":    "gen-1",
			},
		},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainSubmodulePinEdges, rows, "reducer/submodule")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if executor.calls[0].Cypher != batchCanonicalSubmodulePinEdgeCypher {
		t.Fatalf("cypher = %q, want the submodule pin edge template", executor.calls[0].Cypher)
	}
}
