// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterRetraction(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Repository: &projector.RepositoryRow{
			RepoID: "repo-1",
			Name:   "my-repo",
			Path:   "/repos/my-repo",
		},
		Files: []projector.FileRow{
			{Path: "/repos/my-repo/main.go", RelativePath: "main.go", Name: "main.go", Language: "go", RepoID: "repo-1", DirPath: "/repos/my-repo"},
		},
	}

	err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// First calls should be retraction (OperationCanonicalRetract)
	var retractCalls []Statement
	for _, call := range exec.calls {
		if call.Operation == OperationCanonicalRetract {
			retractCalls = append(retractCalls, call)
		}
	}

	if len(retractCalls) == 0 {
		t.Fatal("expected retraction calls, got 0")
	}

	// Early retraction calls run before upserts; entity_retract and
	// terraform_state (#5443, must follow its own migration) intentionally
	// run later.
	lastEarlyRetractIdx := -1
	firstUpsertIdx := -1
	entityRetractIdx := -1
	entityUpsertIdx := -1
	for i, call := range exec.calls {
		phase, _ := call.Parameters[StatementMetadataPhaseKey].(string)
		if call.Operation == OperationCanonicalRetract &&
			phase != "directory_cleanup" &&
			phase != "entity_retract" &&
			phase != canonicalPhaseTerraformState {
			lastEarlyRetractIdx = i
		}
		if phase == "entity_retract" && entityRetractIdx == -1 {
			entityRetractIdx = i
		}
		if phase == "entities" && entityUpsertIdx == -1 {
			entityUpsertIdx = i
		}
		if call.Operation == OperationCanonicalUpsert && firstUpsertIdx == -1 {
			firstUpsertIdx = i
		}
	}
	if firstUpsertIdx >= 0 && lastEarlyRetractIdx >= firstUpsertIdx {
		t.Fatalf("early retraction call at index %d came after upsert at index %d", lastEarlyRetractIdx, firstUpsertIdx)
	}
	if entityUpsertIdx >= 0 && entityRetractIdx >= 0 && entityRetractIdx <= entityUpsertIdx {
		t.Fatalf("entity_retract call at index %d came before entity upsert at index %d", entityRetractIdx, entityUpsertIdx)
	}

	// Verify retraction deletes stale nodes or refreshes current structural
	// edges and carries the identity parameters needed for its scope.
	for i, call := range retractCalls {
		if !strings.Contains(call.Cypher, "DELETE") {
			t.Fatalf("retract call[%d] missing DELETE: %s", i, call.Cypher)
		}
		params := call.Parameters
		if _, ok := params["repo_id"]; !ok {
			if _, ok := params["file_paths"]; !ok {
				if _, ok := params["entity_ids"]; !ok {
					if _, ok := params["rows"]; !ok {
						// scope_id: terraform_state's retraction (#5443) is
						// scope-scoped, not repo-scoped.
						if _, ok := params["scope_id"]; !ok {
							t.Fatalf("retract call[%d] missing repo_id, file_paths, entity_ids, rows, or scope_id param", i)
						}
					}
				}
			}
		}
	}
}

func TestCanonicalNodeWriterSkipsRetractionForFirstGeneration(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 0, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:         "scope-first",
		GenerationID:    "gen-first",
		RepoID:          "repo-first",
		FirstGeneration: true,
		Files: []projector.FileRow{{
			Path:   "/repo/main.go",
			RepoID: "repo-first",
		}},
		Entities: []projector.EntityRow{{
			EntityID: "content-entity:first",
			Label:    "Function",
			RepoID:   "repo-first",
		}},
	}

	if got := writer.buildRetractStatements(mat); len(got) != 0 {
		t.Fatalf("buildRetractStatements() count = %d, want 0 for first generation", len(got))
	}
}

func TestCanonicalNodeWriterFileRetractPreservesCurrentFilePaths(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		Files: []projector.FileRow{
			{Path: "/repos/my-repo/main.go"},
			{Path: "/repos/my-repo/internal/graph.go"},
		},
	}

	var fileRetract Statement
	for _, stmt := range writer.buildRetractStatements(mat) {
		if stmt.Operation == OperationCanonicalRetract && strings.Contains(stmt.Cypher, "DETACH DELETE f") {
			fileRetract = stmt
			break
		}
	}
	if fileRetract.Cypher == "" {
		t.Fatal("missing File retract statement")
	}
	if !strings.Contains(fileRetract.Cypher, "NOT (f.path IN $file_paths)") {
		t.Fatalf("File retract cypher = %q, want current path exclusion", fileRetract.Cypher)
	}

	gotPaths, ok := fileRetract.Parameters["file_paths"].([]string)
	if !ok {
		t.Fatalf("file_paths parameter type = %T, want []string", fileRetract.Parameters["file_paths"])
	}
	wantPaths := []string{"/repos/my-repo/main.go", "/repos/my-repo/internal/graph.go"}
	if strings.Join(gotPaths, "\n") != strings.Join(wantPaths, "\n") {
		t.Fatalf("file_paths = %v, want %v", gotPaths, wantPaths)
	}
}

func TestCanonicalNodeWriterRetractPreservesCurrentEntityAndDirectoryIdentities(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		Directories: []projector.DirectoryRow{
			{Path: "/repos/my-repo/internal"},
			{Path: "/repos/my-repo/cmd"},
		},
		Entities: []projector.EntityRow{
			{EntityID: "entity-function-1", Label: "Function"},
			{EntityID: "entity-struct-1", Label: "Struct"},
			{EntityID: "entity-k8s-1", Label: "K8sResource"},
		},
	}

	var functionRetract Statement
	var structRetract Statement
	var infraRetract Statement
	var directoryRetract Statement
	for _, stmt := range writer.buildEntityRetractStatements(mat) {
		switch {
		case strings.Contains(stmt.Cypher, "MATCH (n:Function)"):
			functionRetract = stmt
		case strings.Contains(stmt.Cypher, "MATCH (n:Struct)"):
			structRetract = stmt
		case strings.Contains(stmt.Cypher, "MATCH (n:K8sResource)"):
			infraRetract = stmt
		case strings.Contains(stmt.Cypher, "MATCH (d:Directory)"):
			directoryRetract = stmt
		}
	}
	if functionRetract.Cypher == "" {
		t.Fatal("missing Function entity retract statement")
	}
	if strings.Contains(functionRetract.Cypher, "IN $entity_ids") {
		t.Fatalf("Function entity retract cypher = %q, want generation-only stale cleanup", functionRetract.Cypher)
	}
	if _, ok := functionRetract.Parameters["entity_ids"]; ok {
		t.Fatalf("Function entity retract should not carry entity_ids after current entity upsert")
	}
	if structRetract.Cypher == "" {
		t.Fatal("missing Struct entity retract statement")
	}
	if infraRetract.Cypher == "" {
		t.Fatal("missing K8sResource entity retract statement")
	}

	for _, stmt := range writer.buildRetractStatements(mat) {
		if strings.Contains(stmt.Cypher, "MATCH (d:Directory)") {
			directoryRetract = stmt
			break
		}
	}
	if directoryRetract.Cypher == "" {
		t.Fatal("missing Directory retract statement")
	}
	if !strings.Contains(directoryRetract.Cypher, "NOT (d.path IN $directory_paths)") {
		t.Fatalf("Directory retract cypher = %q, want current path exclusion", directoryRetract.Cypher)
	}
	gotDirectoryPaths, ok := directoryRetract.Parameters["directory_paths"].([]string)
	if !ok {
		t.Fatalf("directory_paths parameter type = %T, want []string", directoryRetract.Parameters["directory_paths"])
	}
	wantDirectoryPaths := []string{"/repos/my-repo/internal", "/repos/my-repo/cmd"}
	if strings.Join(gotDirectoryPaths, "\n") != strings.Join(wantDirectoryPaths, "\n") {
		t.Fatalf("directory_paths = %v, want %v", gotDirectoryPaths, wantDirectoryPaths)
	}
}
