// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterScopesDeltaProjectionToTouchedFiles(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	err := writer.Write(context.Background(), projector.CanonicalMaterialization{
		ScopeID:               "scope-1",
		GenerationID:          "gen-2",
		RepoID:                "repo-1",
		RepoPath:              "/repos/repo",
		DeltaProjection:       true,
		DeltaFilePaths:        []string{"/repos/repo/changed.go", "/repos/repo/old/deleted.go"},
		DeltaDeletedFilePaths: []string{"/repos/repo/old/deleted.go"},
		Repository: &projector.RepositoryRow{
			RepoID:    "repo-1",
			Name:      "repo",
			Path:      "/repos/repo",
			LocalPath: "/repos/repo",
		},
		Files: []projector.FileRow{
			{
				Path:         "/repos/repo/changed.go",
				RelativePath: "changed.go",
				Name:         "changed.go",
				Language:     "go",
				RepoID:       "repo-1",
			},
		},
		Entities: []projector.EntityRow{
			{
				EntityID:     "repo-1:function:changed.go:Run:1",
				Label:        "Function",
				EntityName:   "Run",
				FilePath:     "/repos/repo/changed.go",
				RelativePath: "changed.go",
				StartLine:    1,
				EndLine:      3,
				Language:     "go",
				RepoID:       "repo-1",
			},
		},
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var sawFileScopedRetract bool
	var sawDirectoryScopedRetract bool
	var sawEntityScopedRetract bool
	for _, call := range exec.calls {
		phase, _ := call.Parameters[StatementMetadataPhaseKey].(string)
		if phase == "repository_cleanup" {
			t.Fatalf("delta projection emitted repository cleanup: %s", call.Cypher)
		}
		if strings.Contains(call.Cypher, "NOT f.path IN $file_paths") ||
			strings.Contains(call.Cypher, "NOT d.path IN $directory_paths") ||
			(strings.Contains(call.Cypher, "n.generation_id <> $generation_id") &&
				!strings.Contains(call.Cypher, "n.path IN $file_paths")) {
			t.Fatalf("delta projection emitted full-generation cleanup: %s", call.Cypher)
		}
		if strings.Contains(call.Cypher, "UNWIND $file_paths AS file_path") &&
			strings.Contains(call.Cypher, "MATCH (f:File {path: file_path})") {
			sawFileScopedRetract = true
			if got := call.Parameters["file_paths"]; len(got.([]string)) != 1 {
				t.Fatalf("file scoped retract paths = %#v, want only deleted paths", got)
			}
		}
		if strings.Contains(call.Cypher, "UNWIND $directory_paths AS directory_path") {
			sawDirectoryScopedRetract = true
			for _, path := range call.Parameters["directory_paths"].([]string) {
				if path == "/repos/repo" {
					t.Fatalf("directory scoped retract included repo root: %#v", call.Parameters["directory_paths"])
				}
			}
		}
		if phase == "entity_retract" &&
			strings.Contains(call.Cypher, "n.path IN $file_paths") {
			sawEntityScopedRetract = true
		}
	}

	if !sawFileScopedRetract {
		t.Fatal("missing file-path scoped delta file retract")
	}
	if !sawDirectoryScopedRetract {
		t.Fatal("missing directory-path scoped delta empty directory retract")
	}
	if !sawEntityScopedRetract {
		t.Fatal("missing file-path scoped delta entity retract")
	}
}

func TestDeltaEmptyDirectoryRetractStatementsRunLeafFirst(t *testing.T) {
	t.Parallel()

	statements := buildDeltaEmptyDirectoryRetractStatements(
		"repo-1",
		"/repos/repo",
		[]string{"/repos/repo/service/api/deleted.go"},
	)

	if got, want := len(statements), 2; got != want {
		t.Fatalf("len(statements) = %d, want %d", got, want)
	}
	if got, want := statements[0].Parameters["directory_paths"], []string{"/repos/repo/service/api"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first directory_paths = %#v, want %#v", got, want)
	}
	if got, want := statements[1].Parameters["directory_paths"], []string{"/repos/repo/service"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second directory_paths = %#v, want %#v", got, want)
	}
}
