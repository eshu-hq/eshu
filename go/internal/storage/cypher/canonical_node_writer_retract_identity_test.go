// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterRetractLeavesRemovedIdentitiesEligibleForDeletion(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		RepoID:       "repo-1",
		Files: []projector.FileRow{
			{Path: "/repos/my-repo/readded.go"},
		},
		Directories: []projector.DirectoryRow{
			{Path: "/repos/my-repo"},
		},
	}

	var fileRetract Statement
	var directoryRetract Statement
	for _, stmt := range writer.buildRetractStatements(mat) {
		switch {
		case strings.Contains(stmt.Cypher, "DETACH DELETE f"):
			fileRetract = stmt
		case strings.Contains(stmt.Cypher, "MATCH (d:Directory)"):
			directoryRetract = stmt
		}
	}
	var codeRetract Statement
	for _, stmt := range writer.buildEntityRetractStatements(mat) {
		if strings.Contains(stmt.Cypher, "MATCH (n:Function)") {
			codeRetract = stmt
			break
		}
	}

	for _, tt := range []struct {
		name      string
		stmt      Statement
		paramName string
		current   string
		removed   string
	}{
		{name: "file", stmt: fileRetract, paramName: "file_paths", current: "/repos/my-repo/readded.go", removed: "/repos/my-repo/deleted.go"},
		{name: "directory", stmt: directoryRetract, paramName: "directory_paths", current: "/repos/my-repo", removed: "/repos/old"},
	} {
		values, ok := tt.stmt.Parameters[tt.paramName].([]string)
		if !ok {
			t.Fatalf("%s %s parameter type = %T, want []string", tt.name, tt.paramName, tt.stmt.Parameters[tt.paramName])
		}
		if !slices.Contains(values, tt.current) {
			t.Fatalf("%s %s = %v, want current identity %q preserved", tt.name, tt.paramName, values, tt.current)
		}
		if slices.Contains(values, tt.removed) {
			t.Fatalf("%s %s = %v, removed identity %q should remain retractable", tt.name, tt.paramName, values, tt.removed)
		}
	}
	if _, ok := codeRetract.Parameters["entity_ids"]; ok {
		t.Fatalf("code entity retract carries entity_ids after current entity upsert")
	}
	if strings.Contains(codeRetract.Cypher, "IN $entity_ids") {
		t.Fatalf("code entity retract Cypher = %q, want generation-only stale cleanup", codeRetract.Cypher)
	}
}

func TestCanonicalNodeWriterRefreshesCurrentFileStructuralEdges(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		RepoID:       "repo-1",
		Files: []projector.FileRow{
			{Path: "/repos/my-repo/main.go"},
		},
		Entities: []projector.EntityRow{
			{EntityID: "content-entity:function", Label: "Function", EntityName: "ServeHTTP", FilePath: "/repos/my-repo/main.go", StartLine: 10},
			{EntityID: "content-entity:class", Label: "Class", EntityName: "Handler", FilePath: "/repos/my-repo/main.go"},
		},
		ClassMembers: []projector.ClassMemberRow{
			{ClassName: "Handler", FunctionName: "ServeHTTP", FilePath: "/repos/my-repo/main.go", FunctionLine: 10},
		},
	}

	var importRefresh Statement
	var directoryFileRefresh Statement
	var entityContainmentRefreshes []Statement
	for _, stmt := range writer.buildRetractStatements(mat) {
		switch {
		case strings.Contains(stmt.Cypher, "-[r:IMPORTS]->"):
			importRefresh = stmt
		case strings.Contains(stmt.Cypher, "CONTAINS]->(f)"):
			directoryFileRefresh = stmt
		case strings.Contains(stmt.Cypher, "[r:CONTAINS]->(n)"):
			t.Fatalf("file/entity edge refresh should be handled by entity retraction, got: %s", stmt.Cypher)
		case strings.Contains(stmt.Cypher, "{uid: row.parent_entity_id})-[r:CONTAINS]->(m)"):
			entityContainmentRefreshes = append(entityContainmentRefreshes, stmt)
		}
	}

	for _, tt := range []struct {
		name      string
		stmt      Statement
		paramName string
		want      string
	}{
		{name: "imports", stmt: importRefresh, paramName: "file_paths", want: "/repos/my-repo/main.go"},
		{name: "directory file contains", stmt: directoryFileRefresh, paramName: "file_paths", want: "/repos/my-repo/main.go"},
	} {
		if tt.stmt.Cypher == "" {
			t.Fatalf("missing %s refresh statement", tt.name)
		}
		values, ok := tt.stmt.Parameters[tt.paramName].([]string)
		if !ok {
			t.Fatalf("%s %s parameter type = %T, want []string", tt.name, tt.paramName, tt.stmt.Parameters[tt.paramName])
		}
		if !slices.Contains(values, tt.want) {
			t.Fatalf("%s %s = %v, want %q", tt.name, tt.paramName, values, tt.want)
		}
	}
	var foundClassRefresh bool
	for _, stmt := range entityContainmentRefreshes {
		rows, ok := stmt.Parameters["rows"].([]map[string]any)
		if !ok {
			t.Fatalf("entity contains rows type = %T, want []map[string]any", stmt.Parameters["rows"])
		}
		for _, row := range rows {
			if row["parent_entity_id"] != "content-entity:class" {
				continue
			}
			foundClassRefresh = true
			childIDs, ok := row["child_entity_ids"].([]string)
			if !ok {
				t.Fatalf("entity contains child_entity_ids type = %T, want []string", row["child_entity_ids"])
			}
			if !slices.Contains(childIDs, "content-entity:function") {
				t.Fatalf("entity contains child_entity_ids = %v, want current child entity", childIDs)
			}
		}
	}
	if !foundClassRefresh {
		t.Fatal("missing class containment refresh statement")
	}
}

func TestCanonicalNodeWriterDeduplicatesRetractFilePaths(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		RepoID:       "repo-1",
		Files: []projector.FileRow{
			{Path: "/repos/my-repo/main.go"},
			{Path: "/repos/my-repo/main.go"},
			{Path: "/repos/my-repo/routes.go"},
		},
	}

	var fileRetract Statement
	var importRefresh Statement
	for _, stmt := range writer.buildRetractStatements(mat) {
		switch {
		case strings.Contains(stmt.Cypher, "DETACH DELETE f"):
			fileRetract = stmt
		case strings.Contains(stmt.Cypher, "-[r:IMPORTS]->"):
			importRefresh = stmt
		}
	}

	want := []string{"/repos/my-repo/main.go", "/repos/my-repo/routes.go"}
	for _, tt := range []struct {
		name string
		stmt Statement
	}{
		{name: "file retract", stmt: fileRetract},
		{name: "import refresh", stmt: importRefresh},
	} {
		got, ok := tt.stmt.Parameters["file_paths"].([]string)
		if !ok {
			t.Fatalf("%s file_paths type = %T, want []string", tt.name, tt.stmt.Parameters["file_paths"])
		}
		if !slices.Equal(got, want) {
			t.Fatalf("%s file_paths = %v, want %v", tt.name, got, want)
		}
	}
}

func TestCanonicalNodeWriterKeepsEmptyDirectoryPathList(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		RepoID:       "repo-1",
	}

	var directoryRetract Statement
	for _, stmt := range writer.buildRetractStatements(mat) {
		if strings.Contains(stmt.Cypher, "MATCH (d:Directory)") {
			directoryRetract = stmt
			break
		}
	}
	if directoryRetract.Cypher == "" {
		t.Fatal("missing directory retract statement")
	}
	got, ok := directoryRetract.Parameters["directory_paths"].([]string)
	if !ok {
		t.Fatalf("directory_paths type = %T, want []string", directoryRetract.Parameters["directory_paths"])
	}
	if got == nil {
		t.Fatal("directory_paths is nil, want empty []string so Cypher IN receives a list")
	}
	if len(got) != 0 {
		t.Fatalf("directory_paths = %v, want empty list", got)
	}
}
