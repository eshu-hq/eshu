// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterRefreshesStructuralEdgesBeforeEntityRetract(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		RepoID:       "repo-1",
		Files: []projector.FileRow{
			{Path: "/repos/my-repo/main.go"},
		},
		Entities: []projector.EntityRow{
			{EntityID: "content-entity:function", Label: "Function"},
		},
	}

	importRefreshPhaseIdx := -1
	entityRetractPhaseIdx := -1
	for i, phase := range writer.buildPhases(mat) {
		switch phase.name {
		case "retract":
			for _, stmt := range phase.statements {
				if strings.Contains(stmt.Cypher, "-[r:IMPORTS]->") {
					importRefreshPhaseIdx = i
				}
			}
		case "entity_retract":
			entityRetractPhaseIdx = i
		}
	}

	if importRefreshPhaseIdx < 0 {
		t.Fatal("missing import refresh statement")
	}
	if entityRetractPhaseIdx < 0 {
		t.Fatal("missing entity_retract phase")
	}
	if importRefreshPhaseIdx > entityRetractPhaseIdx {
		t.Fatalf("import refresh phase index = %d, entity_retract phase index = %d; refresh must run first",
			importRefreshPhaseIdx, entityRetractPhaseIdx)
	}
}

func TestCanonicalNodeWriterDoesNotRefreshFileEntityEdgesPerFile(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		RepoID:       "repo-1",
		Files: []projector.FileRow{
			{Path: "/repos/my-repo/current.go"},
			{Path: "/repos/my-repo/empty.go"},
		},
		Entities: []projector.EntityRow{
			{EntityID: "function-current", Label: "Function", FilePath: "/repos/my-repo/current.go"},
			{EntityID: "struct-current", Label: "Struct", FilePath: "/repos/my-repo/current.go"},
		},
	}

	for _, stmt := range writer.buildRetractStatements(mat) {
		if strings.Contains(stmt.Cypher, "[r:CONTAINS]->(n)") {
			t.Fatalf("retract still emits per-file entity refresh after entity retraction owns stale edge cleanup: %s", stmt.Cypher)
		}
	}
}

func TestCanonicalNodeWriterRefreshesOnlyStaleEntityContainmentEdges(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		RepoID:       "repo-1",
		Entities: []projector.EntityRow{
			{EntityID: "class-current", Label: "Class", EntityName: "Handler", FilePath: "/repos/my-repo/current.go"},
			{EntityID: "method-current", Label: "Function", EntityName: "ServeHTTP", FilePath: "/repos/my-repo/current.go", StartLine: 10},
			{EntityID: "function-empty", Label: "Function", EntityName: "topLevel", FilePath: "/repos/my-repo/current.go", StartLine: 30},
		},
		ClassMembers: []projector.ClassMemberRow{
			{ClassName: "Handler", FunctionName: "ServeHTTP", FilePath: "/repos/my-repo/current.go", FunctionLine: 10},
		},
	}

	var containmentRefreshes []Statement
	for _, stmt := range writer.buildRetractStatements(mat) {
		if strings.Contains(stmt.Cypher, "{uid: row.parent_entity_id})-[r:CONTAINS]->(m)") {
			containmentRefreshes = append(containmentRefreshes, stmt)
		}
	}
	if got, want := len(containmentRefreshes), 2; got != want {
		t.Fatalf("entity containment refresh statement count = %d, want %d", got, want)
	}
	var rows []map[string]any
	for _, stmt := range containmentRefreshes {
		if !strings.Contains(stmt.Cypher, "MATCH (n:Class {uid: row.parent_entity_id})") &&
			!strings.Contains(stmt.Cypher, "MATCH (n:Function {uid: row.parent_entity_id})") {
			t.Fatalf("entity containment refresh Cypher = %q, want label-specific uid anchor", stmt.Cypher)
		}
		stmtRows, ok := stmt.Parameters["rows"].([]map[string]any)
		if !ok {
			t.Fatalf("rows type = %T, want []map[string]any", stmt.Parameters["rows"])
		}
		rows = append(rows, stmtRows...)
	}
	if got, want := len(rows), 3; got != want {
		t.Fatalf("rows count = %d, want %d", got, want)
	}
	for _, row := range rows {
		parentID, ok := row["parent_entity_id"].(string)
		if !ok {
			t.Fatalf("parent_entity_id type = %T, want string", row["parent_entity_id"])
		}
		childIDs, ok := row["child_entity_ids"].([]string)
		if !ok {
			t.Fatalf("child_entity_ids type = %T, want []string", row["child_entity_ids"])
		}
		switch parentID {
		case "class-current":
			if got, want := strings.Join(childIDs, ","), "method-current"; got != want {
				t.Fatalf("refresh[%s] child_entity_ids = %q, want %q", parentID, got, want)
			}
		case "method-current":
			if len(childIDs) != 0 {
				t.Fatalf("refresh[%s] child_entity_ids = %#v, want empty", parentID, childIDs)
			}
		case "function-empty":
			if len(childIDs) != 0 {
				t.Fatalf("refresh[%s] child_entity_ids = %#v, want empty", parentID, childIDs)
			}
		default:
			t.Fatalf("unexpected parent_entity_id %q", parentID)
		}
	}
}

func TestCanonicalNodeWriterRetractCoversStructuralFamiliesFromIssue3987(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		RepoID:       "repo-1",
		Files: []projector.FileRow{
			{Path: "/repos/my-repo/current.go"},
		},
		Entities: []projector.EntityRow{
			{EntityID: "class-current", Label: "Class", EntityName: "Handler", FilePath: "/repos/my-repo/current.go"},
			{EntityID: "method-current", Label: "Function", EntityName: "ServeHTTP", FilePath: "/repos/my-repo/current.go", StartLine: 10},
		},
		ClassMembers: []projector.ClassMemberRow{
			{ClassName: "Handler", FunctionName: "ServeHTTP", FilePath: "/repos/my-repo/current.go", FunctionLine: 10},
		},
	}

	var sawImportRefresh, sawParameterRetract, sawClassMemberRefresh bool
	for _, stmt := range writer.buildRetractStatements(mat) {
		switch {
		case strings.Contains(stmt.Cypher, "MATCH (f)-[r:IMPORTS]->(:Module)"):
			sawImportRefresh = true
		case strings.Contains(stmt.Cypher, "MATCH (p:Parameter)"):
			sawParameterRetract = true
			if !strings.Contains(stmt.Cypher, "p.generation_id <> $generation_id") {
				t.Fatalf("parameter retract missing generation guard: %s", stmt.Cypher)
			}
			if stmt.Parameters["generation_id"] != "gen-2" {
				t.Fatalf("parameter retract generation_id = %v, want gen-2", stmt.Parameters["generation_id"])
			}
		case strings.Contains(stmt.Cypher, "MATCH (n:Class {uid: row.parent_entity_id})-[r:CONTAINS]->(m)"):
			sawClassMemberRefresh = true
			rows, ok := stmt.Parameters["rows"].([]map[string]any)
			if !ok || len(rows) != 1 {
				t.Fatalf("class-member refresh rows = %#v, want one row", stmt.Parameters["rows"])
			}
			childIDs, ok := rows[0]["child_entity_ids"].([]string)
			if !ok || strings.Join(childIDs, ",") != "method-current" {
				t.Fatalf("class-member refresh child ids = %#v, want method-current", rows[0]["child_entity_ids"])
			}
		}
	}
	if !sawImportRefresh {
		t.Fatal("missing import edge refresh for current file")
	}
	if !sawParameterRetract {
		t.Fatal("missing generation-scoped parameter retract")
	}
	if !sawClassMemberRefresh {
		t.Fatal("missing class-member containment refresh")
	}
}

func TestCanonicalNodeWriterRetractCoversProjectableEntityLabels(t *testing.T) {
	t.Parallel()

	covered := make(map[string]string)
	for _, family := range []struct {
		name   string
		labels map[string]struct{}
	}{
		{name: "code", labels: canonicalNodeRetractCodeEntityLabels},
		{name: "infra", labels: canonicalNodeRetractInfraEntityLabels},
		{name: "terraform", labels: canonicalNodeRetractTerraformEntityLabels},
		{name: "cloudformation", labels: canonicalNodeRetractCloudFormationEntityLabels},
		{name: "sql", labels: canonicalNodeRetractSQLEntityLabels},
		{name: "data", labels: canonicalNodeRetractDataEntityLabels},
		{name: "oci", labels: canonicalNodeRetractOCIEntityLabels},
		{name: "package_registry", labels: canonicalNodeRetractPackageRegistryEntityLabels},
	} {
		for label := range family.labels {
			if previous, exists := covered[label]; exists {
				t.Fatalf("label %s covered by both %s and %s retract families", label, previous, family.name)
			}
			covered[label] = family.name
		}
	}
	retractLabels := make(map[string]struct{})
	for _, label := range canonicalNodeRetractEntityLabels() {
		retractLabels[label] = struct{}{}
	}

	var missing []string
	for _, label := range projector.EntityTypeLabelMap() {
		if label == "Module" || label == "Parameter" {
			continue
		}
		if _, ok := covered[label]; !ok {
			missing = append(missing, label)
			continue
		}
		if _, ok := retractLabels[label]; !ok {
			missing = append(missing, label)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("retract families missing projectable labels: %s", strings.Join(missing, ", "))
	}
}

// TestCanonicalNodeWriterRetractRetainsCrossplaneClaimForLegacySweep guards
// issue #5478: a Crossplane Claim has been edge-only since #5347 (it stays a
// K8sResource node; the SATISFIED_BY edge to its CrossplaneXRD is the
// classification), so no writer emits the CrossplaneClaim label anymore. The
// retract registry must still scan for it, though: a graph provisioned before
// #5347 can hold nodes carrying the literal CrossplaneClaim label, and only
// the retract phase's DETACH DELETE sweeps them once the Claim re-projects as
// a K8sResource node in a later reconciliation generation. Dropping this
// entry would orphan those legacy nodes forever for a deployment that
// upgrades straight from a pre-#5347 binary to a post-#5478 one.
func TestCanonicalNodeWriterRetractRetainsCrossplaneClaimForLegacySweep(t *testing.T) {
	t.Parallel()

	if _, ok := canonicalNodeRetractInfraEntityLabels["CrossplaneClaim"]; !ok {
		t.Fatal("canonicalNodeRetractInfraEntityLabels must retain CrossplaneClaim to sweep legacy pre-#5347 nodes (issue #5478)")
	}
	found := false
	for _, label := range canonicalNodeRetractEntityLabels() {
		if label == "CrossplaneClaim" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("canonicalNodeRetractEntityLabels() must retain CrossplaneClaim to sweep legacy pre-#5347 nodes (issue #5478)")
	}
	found = false
	for _, label := range RetractableNodeEntityLabels() {
		if label == "CrossplaneClaim" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("RetractableNodeEntityLabels() must retain CrossplaneClaim to sweep legacy pre-#5347 nodes (issue #5478)")
	}
}

func TestCanonicalNodeWriterEmptyMaterialization(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
	}

	err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if len(exec.calls) != 0 {
		t.Fatalf("expected 0 executor calls for empty materialization, got %d", len(exec.calls))
	}
}

func TestCanonicalNodeWriterRepositoryOnly(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Repository: &projector.RepositoryRow{
			RepoID:    "repo-1",
			Name:      "my-repo",
			Path:      "/repos/my-repo",
			LocalPath: "/repos/my-repo",
			RemoteURL: "https://github.com/org/my-repo",
			RepoSlug:  "org/my-repo",
			HasRemote: true,
		},
	}

	err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Should have retraction calls + repository upsert even with no files/entities
	var repoUpsertFound bool
	for _, call := range exec.calls {
		if call.Operation == OperationCanonicalUpsert && strings.Contains(call.Cypher, "MERGE (r:Repository") {
			repoUpsertFound = true
			params := call.Parameters
			if params["repo_id"] != "repo-1" {
				t.Fatalf("repo_id = %v, want repo-1", params["repo_id"])
			}
			if params["name"] != "my-repo" {
				t.Fatalf("name = %v, want my-repo", params["name"])
			}
			if params["has_remote"] != true {
				t.Fatalf("has_remote = %v, want true", params["has_remote"])
			}
		}
	}
	if !repoUpsertFound {
		t.Fatal("expected repository upsert call")
	}
}

func TestCanonicalNodeWriterFilesCreateRepoContainsEdges(t *testing.T) {
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
		Directories: []projector.DirectoryRow{
			{Path: "/repos/my-repo/src", Name: "src", ParentPath: "/repos/my-repo", RepoID: "repo-1", Depth: 0},
		},
		Files: []projector.FileRow{
			{Path: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", Name: "main.go", Language: "go", RepoID: "repo-1", DirPath: "/repos/my-repo/src"},
		},
	}

	err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Find the file upsert call and verify it includes REPO_CONTAINS and CONTAINS edges
	var fileCypher string
	for _, call := range exec.calls {
		if call.Operation == OperationCanonicalUpsert &&
			call.Parameters[StatementMetadataPhaseKey] == CanonicalPhaseFiles &&
			strings.Contains(call.Cypher, "MATCH (f:File {path: row.path})") {
			fileCypher = call.Cypher
			break
		}
	}
	if fileCypher == "" {
		t.Fatal("expected file upsert call")
	}
	if !strings.Contains(fileCypher, "REPO_CONTAINS") {
		t.Fatalf("file cypher missing REPO_CONTAINS: %s", fileCypher)
	}
	if !strings.Contains(fileCypher, "MATCH (d:Directory") {
		t.Fatalf("file cypher missing Directory CONTAINS edge: %s", fileCypher)
	}
}
