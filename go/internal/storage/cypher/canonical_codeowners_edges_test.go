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

func TestBatchCanonicalCodeownersOwnershipEdgeCypherShape(t *testing.T) {
	t.Parallel()

	cypher := batchCanonicalCodeownersOwnershipEdgeCypher
	if !strings.Contains(cypher, "MERGE (team:CodeownerTeam {ref:") {
		t.Errorf("template missing CodeownerTeam MERGE: %q", cypher)
	}
	if !strings.Contains(cypher, "MERGE (repo)-[rel:DECLARES_CODEOWNER") {
		t.Errorf("template missing DECLARES_CODEOWNER edge MERGE: %q", cypher)
	}
	if !strings.Contains(cypher, "->(team)") {
		t.Errorf("template does not close the relationship onto team: %q", cypher)
	}
	if !strings.Contains(cypher, "MERGE (repo:Repository {id: row.repo_id})") {
		t.Errorf("template missing Repository MERGE: %q", cypher)
	}
	// The relationship MERGE key must include pattern and source_path — not
	// just the (repo, team) endpoints — or two different rule patterns naming
	// the same owner would collapse onto a single rebound relationship (see
	// the package doc comment on batchCanonicalCodeownersOwnershipEdgeCypher).
	if !strings.Contains(cypher, "pattern: row.pattern") || !strings.Contains(cypher, "source_path: row.source_path") {
		t.Errorf("relationship MERGE must key on pattern+source_path to keep parallel rule edges distinct: %q", cypher)
	}
}

func TestBuildCodeownersOwnershipRowMap(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"repo_id":       "repo-1",
		"owner_ref":     "@org/backend",
		"pattern":       "*.go",
		"source_path":   ".github/CODEOWNERS",
		"order_index":   3,
		"generation_id": "gen-1",
	}
	cypher, rowMap, ok := buildCodeownersOwnershipRowMap(payload, "reducer/codeowners")
	if !ok {
		t.Fatal("buildCodeownersOwnershipRowMap ok = false, want true")
	}
	if cypher != batchCanonicalCodeownersOwnershipEdgeCypher {
		t.Errorf("cypher = %q, want the codeowners ownership edge template", cypher)
	}
	want := map[string]any{
		"repo_id":         "repo-1",
		"owner_ref":       "@org/backend",
		"pattern":         "*.go",
		"source_path":     ".github/CODEOWNERS",
		"order_index":     3,
		"generation_id":   "gen-1",
		"evidence_source": "reducer/codeowners",
	}
	if !reflect.DeepEqual(rowMap, want) {
		t.Errorf("rowMap = %#v, want %#v", rowMap, want)
	}
}

func TestBuildCodeownersOwnershipRowMapRequiresMergeKeys(t *testing.T) {
	t.Parallel()

	base := map[string]any{
		"repo_id":     "repo-1",
		"owner_ref":   "@org/backend",
		"pattern":     "*.go",
		"source_path": ".github/CODEOWNERS",
	}
	for _, missing := range []string{"repo_id", "owner_ref", "pattern", "source_path"} {
		payload := map[string]any{}
		for k, v := range base {
			if k != missing {
				payload[k] = v
			}
		}
		if _, _, ok := buildCodeownersOwnershipRowMap(payload, "src"); ok {
			t.Errorf("missing %q should be rejected, got ok=true", missing)
		}
	}
}

func TestBuildRetractCodeownersOwnershipEdges(t *testing.T) {
	t.Parallel()

	stmt := BuildRetractCodeownersOwnershipEdges([]string{"repo-1"}, "reducer/codeowners")
	if !strings.Contains(stmt.Cypher, "rel:DECLARES_CODEOWNER") {
		t.Errorf("retract cypher missing DECLARES_CODEOWNER: %q", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "repo.id IN $repo_ids") {
		t.Errorf("retract cypher is not repo-scoped: %q", stmt.Cypher)
	}
	repoIDs, ok := stmt.Parameters["repo_ids"].([]string)
	if !ok || !reflect.DeepEqual(repoIDs, []string{"repo-1"}) {
		t.Errorf("repo_ids = %#v, want [repo-1]", stmt.Parameters["repo_ids"])
	}
}

func TestBuildRetractCodeownersOwnershipEdgesByFilePath(t *testing.T) {
	t.Parallel()

	stmt := BuildRetractCodeownersOwnershipEdgesByFilePath([]string{".github/CODEOWNERS"}, "reducer/codeowners")
	if !strings.Contains(stmt.Cypher, "rel:DECLARES_CODEOWNER") {
		t.Errorf("retract cypher missing DECLARES_CODEOWNER: %q", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "rel.source_path IN $file_paths") {
		t.Errorf("retract cypher is not source_path-scoped: %q", stmt.Cypher)
	}
	filePaths, ok := stmt.Parameters["file_paths"].([]string)
	if !ok || !reflect.DeepEqual(filePaths, []string{".github/CODEOWNERS"}) {
		t.Errorf("file_paths = %#v, want [.github/CODEOWNERS]", stmt.Parameters["file_paths"])
	}
}

func TestEdgeWriterRetractEdgesCodeownersOwnershipDeltaUsesFilePathScope(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-1",
			Payload: map[string]any{
				"repo_id":          "repo-1",
				"delta_projection": true,
				"delta_file_paths": []string{".github/CODEOWNERS"},
			},
		},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainCodeownersOwnershipEdges, rows, "reducer/codeowners")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	stmt := executor.calls[0]
	if !strings.Contains(stmt.Cypher, "rel.source_path IN $file_paths") {
		t.Fatalf("delta retract cypher = %q, want source_path filter", stmt.Cypher)
	}
	filePaths, ok := stmt.Parameters["file_paths"].([]string)
	if !ok {
		t.Fatalf("file_paths parameter type = %T, want []string", stmt.Parameters["file_paths"])
	}
	if got, want := strings.Join(filePaths, ","), ".github/CODEOWNERS"; got != want {
		t.Fatalf("file_paths = %q, want %q", got, want)
	}
}

func TestEdgeWriterRetractEdgesCodeownersOwnershipWholeRepoScope(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-1"},
	}

	err := writer.RetractEdges(context.Background(), reducer.DomainCodeownersOwnershipEdges, rows, "reducer/codeowners")
	if err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	stmt := executor.calls[0]
	if !strings.Contains(stmt.Cypher, "repo.id IN $repo_ids") {
		t.Fatalf("whole-repo retract cypher = %q, want repo_ids filter", stmt.Cypher)
	}
	repoIDs, ok := stmt.Parameters["repo_ids"].([]string)
	if !ok || !reflect.DeepEqual(repoIDs, []string{"repo-1"}) {
		t.Fatalf("repo_ids = %#v, want [repo-1]", stmt.Parameters["repo_ids"])
	}
}

func TestEdgeWriterWriteEdgesCodeownersOwnershipRoutesTemplate(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	rows := []reducer.SharedProjectionIntentRow{
		{
			IntentID:     "i1",
			RepositoryID: "repo-1",
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"owner_ref":     "@org/backend",
				"pattern":       "*.go",
				"source_path":   ".github/CODEOWNERS",
				"order_index":   0,
				"generation_id": "gen-1",
			},
		},
	}

	err := writer.WriteEdges(context.Background(), reducer.DomainCodeownersOwnershipEdges, rows, "reducer/codeowners")
	if err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
	if executor.calls[0].Cypher != batchCanonicalCodeownersOwnershipEdgeCypher {
		t.Fatalf("cypher = %q, want the codeowners ownership edge template", executor.calls[0].Cypher)
	}
}
