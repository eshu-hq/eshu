// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func atlantisProjectRowEntity(uid, name, filePath, dir, dependsOn string) projector.EntityRow {
	meta := map[string]any{"dir": dir}
	if dependsOn != "" {
		meta["depends_on"] = dependsOn
	}
	return projector.EntityRow{
		Label:      "AtlantisProject",
		EntityID:   uid,
		EntityName: name,
		FilePath:   filePath,
		Metadata:   meta,
	}
}

// TestAtlantisEdgeStatementsResolvesManagesAndDependsOn proves the builder
// resolves both governance edges from AtlantisProject entities: MANAGES rows map
// each project's uid to the absolute Directory path (repo root + dir), and
// DEPENDS_ON rows map a project's uid to the sibling project's uid named in
// depends_on (resolved within the same atlantis.yaml file). Endpoints are matched
// by canonical key (uid / Directory.path), not bound-variable properties.
func TestAtlantisEdgeStatementsResolvesManagesAndDependsOn(t *testing.T) {
	t.Parallel()

	const file = "/repo/atlantis.yaml"
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		RepoPath:     "/repo",
		Entities: []projector.EntityRow{
			atlantisProjectRowEntity("uid-network", "network", file, "network", ""),
			atlantisProjectRowEntity("uid-staging", "staging", file, "staging", "network"),
		},
	}

	stmts := atlantisEdgeStatements(mat)
	if len(stmts) != 2 {
		t.Fatalf("atlantisEdgeStatements() returned %d statements, want 2 (MANAGES + DEPENDS_ON)", len(stmts))
	}

	manages := stmts[0]
	if !strings.Contains(manages.Cypher, "MANAGES") || !strings.Contains(manages.Cypher, "AtlantisProject {uid:") || !strings.Contains(manages.Cypher, "Directory {path:") {
		t.Fatalf("MANAGES cypher should match by uid + Directory.path: %s", manages.Cypher)
	}
	managesRows := manages.Parameters["rows"].([]map[string]any)
	if len(managesRows) != 2 {
		t.Fatalf("MANAGES rows = %d, want 2; %+v", len(managesRows), managesRows)
	}
	wantTargets := map[string]string{"uid-network": "/repo/network", "uid-staging": "/repo/staging"}
	for _, row := range managesRows {
		if got := row["target_path"]; got != wantTargets[row["source_uid"].(string)] {
			t.Fatalf("MANAGES row %v target_path = %v, want %v", row["source_uid"], got, wantTargets[row["source_uid"].(string)])
		}
	}

	dependsOn := stmts[1]
	if !strings.Contains(dependsOn.Cypher, "ATLANTIS_DEPENDS_ON") {
		t.Fatalf("DEPENDS_ON cypher missing ATLANTIS_DEPENDS_ON edge type: %s", dependsOn.Cypher)
	}
	dependsRows := dependsOn.Parameters["rows"].([]map[string]any)
	if len(dependsRows) != 1 {
		t.Fatalf("DEPENDS_ON rows = %d, want 1; %+v", len(dependsRows), dependsRows)
	}
	if dependsRows[0]["source_uid"] != "uid-staging" || dependsRows[0]["target_uid"] != "uid-network" {
		t.Fatalf("DEPENDS_ON row = %+v, want staging->network", dependsRows[0])
	}

	// generation_id must propagate into every row so the edge carries the
	// projecting generation (the writer SETs r.generation_id = row.generation_id).
	for _, row := range append(append([]map[string]any{}, managesRows...), dependsRows...) {
		if row["generation_id"] != "gen-1" {
			t.Fatalf("row %+v missing generation_id=gen-1", row)
		}
	}
}

// TestAtlantisEdgeStatementsResolvesUsesWorkflow proves a project's workflow
// reference resolves to the AtlantisWorkflow node's uid in the same file (whether
// defined in-file or a referenced stub), producing a USES_WORKFLOW edge row.
func TestAtlantisEdgeStatementsResolvesUsesWorkflow(t *testing.T) {
	t.Parallel()

	const file = "/repo/atlantis.yaml"
	project := projector.EntityRow{
		Label:      "AtlantisProject",
		EntityID:   "uid-app",
		EntityName: "app",
		FilePath:   file,
		Metadata:   map[string]any{"dir": "app", "workflow": "custom"},
	}
	workflow := projector.EntityRow{
		Label:      "AtlantisWorkflow",
		EntityID:   "uid-wf-custom",
		EntityName: "custom",
		FilePath:   file,
		Metadata:   map[string]any{"source": "defined"},
	}
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		RepoPath:     "/repo",
		Entities:     []projector.EntityRow{project, workflow},
	}

	stmts := atlantisEdgeStatements(mat)
	var usesRows []map[string]any
	for _, stmt := range stmts {
		if strings.Contains(stmt.Cypher, "USES_WORKFLOW") {
			usesRows = stmt.Parameters["rows"].([]map[string]any)
		}
	}
	if len(usesRows) != 1 {
		t.Fatalf("USES_WORKFLOW rows = %d, want 1; stmts=%d", len(usesRows), len(stmts))
	}
	if usesRows[0]["source_uid"] != "uid-app" || usesRows[0]["target_uid"] != "uid-wf-custom" {
		t.Fatalf("USES_WORKFLOW row = %+v, want app->custom", usesRows[0])
	}
	// A workflow reference with no matching AtlantisWorkflow node yields no edge.
	matNoWf := projector.CanonicalMaterialization{
		GenerationID: "gen-1", RepoPath: "/repo",
		Entities: []projector.EntityRow{project},
	}
	for _, stmt := range atlantisEdgeStatements(matNoWf) {
		if strings.Contains(stmt.Cypher, "USES_WORKFLOW") {
			t.Fatalf("USES_WORKFLOW emitted with no AtlantisWorkflow node present")
		}
	}
}

// TestAtlantisEdgeStatementsScopesDependsOnPerFile proves depends_on resolves
// only against sibling projects in the SAME atlantis.yaml: two projects sharing a
// name across different files must not produce a cross-file DEPENDS_ON edge.
func TestAtlantisEdgeStatementsScopesDependsOnPerFile(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		RepoPath:     "/repo",
		Entities: []projector.EntityRow{
			// In fileA, "app" depends on "network"; only fileA has a "network".
			atlantisProjectRowEntity("uid-a-network", "network", "/repo/a/atlantis.yaml", "net", ""),
			atlantisProjectRowEntity("uid-a-app", "app", "/repo/a/atlantis.yaml", "app", "network"),
			// fileB has an "app" depending on "network", but its "network" sibling
			// lives only in fileA — it must NOT resolve across files.
			atlantisProjectRowEntity("uid-b-app", "app", "/repo/b/atlantis.yaml", "app", "network"),
		},
	}

	stmts := atlantisEdgeStatements(mat)
	var dependsRows []map[string]any
	for _, stmt := range stmts {
		if strings.Contains(stmt.Cypher, "ATLANTIS_DEPENDS_ON") {
			dependsRows = stmt.Parameters["rows"].([]map[string]any)
		}
	}
	if len(dependsRows) != 1 {
		t.Fatalf("DEPENDS_ON rows = %d, want 1 (only the in-file fileA app->network); %+v", len(dependsRows), dependsRows)
	}
	if dependsRows[0]["source_uid"] != "uid-a-app" || dependsRows[0]["target_uid"] != "uid-a-network" {
		t.Fatalf("DEPENDS_ON row = %+v, want fileA app->network (no cross-file resolution)", dependsRows[0])
	}
}

// TestAtlantisEdgeStatementsNilWithoutAtlantisProject proves the builder is a
// no-op for materializations that carry no AtlantisProject entity.
func TestAtlantisEdgeStatementsNilWithoutAtlantisProject(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		RepoPath:     "/repo",
		Entities: []projector.EntityRow{
			{Label: "Function", EntityID: "fn-1"},
		},
	}
	if stmts := atlantisEdgeStatements(mat); stmts != nil {
		t.Fatalf("atlantisEdgeStatements() = %d statements, want nil for non-Atlantis repo", len(stmts))
	}
}
