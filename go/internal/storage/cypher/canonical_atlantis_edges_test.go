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
	if len(stmts) != 5 {
		t.Fatalf("atlantisEdgeStatements() returned %d statements, want 5 (3 retract + MANAGES + DEPENDS_ON)", len(stmts))
	}

	manages := mergeStatementContaining(t, stmts, "MERGE (p)-[r:MANAGES]->(d)")
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

	dependsOn := mergeStatementContaining(t, stmts, "MERGE (p)-[r:ATLANTIS_DEPENDS_ON]->(q)")
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
	usesWorkflow := mergeStatementContaining(t, stmts, "MERGE (p)-[r:USES_WORKFLOW]->(w)")
	usesRows := usesWorkflow.Parameters["rows"].([]map[string]any)
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
		if stmt.Operation == OperationCanonicalUpsert && strings.Contains(stmt.Cypher, "USES_WORKFLOW") {
			t.Fatalf("USES_WORKFLOW emitted with no AtlantisWorkflow node present")
		}
	}
}

// TestAtlantisEdgeStatementsRetractsStaleEdgesBeforeMerge proves the builder
// emits generation-scoped retraction for Atlantis structural edges BEFORE the
// MERGE statements. This covers the stale-edge case where the project, workflow,
// and target project nodes all survive into the next generation but a project's
// dir, depends_on, or workflow relationship changes.
func TestAtlantisEdgeStatementsRetractsStaleEdgesBeforeMerge(t *testing.T) {
	t.Parallel()

	const file = "/repo/atlantis.yaml"
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		RepoPath:     "/repo",
		Entities: []projector.EntityRow{
			atlantisProjectRowEntity("uid-network", "network", file, "network", ""),
			atlantisProjectRowEntity("uid-app", "app", file, "app", "network"),
			{
				Label:      "AtlantisWorkflow",
				EntityID:   "uid-wf-custom",
				EntityName: "custom",
				FilePath:   file,
			},
			{
				Label:      "AtlantisProject",
				EntityID:   "uid-api",
				EntityName: "api",
				FilePath:   file,
				Metadata: map[string]any{
					"dir":      "api",
					"workflow": "custom",
				},
			},
		},
	}

	stmts := atlantisEdgeStatements(mat)
	if len(stmts) != 6 {
		t.Fatalf("atlantisEdgeStatements() returned %d statements, want 6 (3 retract + 3 merge)", len(stmts))
	}

	for i, relType := range []string{"MANAGES", "ATLANTIS_DEPENDS_ON", "USES_WORKFLOW"} {
		stmt := stmts[i]
		if stmt.Operation != OperationCanonicalRetract {
			t.Fatalf("statement %d Operation = %q, want %q", i, stmt.Operation, OperationCanonicalRetract)
		}
		if !strings.Contains(stmt.Cypher, "AtlantisProject {uid: uid}") ||
			!strings.Contains(stmt.Cypher, "[r:"+relType+"]") ||
			!strings.Contains(stmt.Cypher, "r.evidence_source = 'projector/canonical'") ||
			!strings.Contains(stmt.Cypher, "r.generation_id <> $generation_id") ||
			!strings.Contains(stmt.Cypher, "DELETE r") {
			t.Fatalf("%s retract cypher wrong shape: %s", relType, stmt.Cypher)
		}
		sourceUIDs, ok := stmt.Parameters["source_uids"].([]string)
		if !ok {
			t.Fatalf("%s retract source_uids type = %T, want []string", relType, stmt.Parameters["source_uids"])
		}
		wantSources := map[string]bool{"uid-network": true, "uid-app": true, "uid-api": true}
		if len(sourceUIDs) != len(wantSources) {
			t.Fatalf("%s retract source_uids = %#v, want all project uids", relType, sourceUIDs)
		}
		for _, uid := range sourceUIDs {
			if !wantSources[uid] {
				t.Fatalf("%s retract unexpected source uid %q", relType, uid)
			}
		}
		if stmt.Parameters["generation_id"] != "gen-2" {
			t.Fatalf("%s retract generation_id = %v, want gen-2", relType, stmt.Parameters["generation_id"])
		}
	}

	if stmts[3].Operation != OperationCanonicalUpsert || !strings.Contains(stmts[3].Cypher, "MERGE (p)-[r:MANAGES]->(d)") {
		t.Fatalf("statement 3 should be the MANAGES merge: op=%q cypher=%s", stmts[3].Operation, stmts[3].Cypher)
	}
	if stmts[4].Operation != OperationCanonicalUpsert || !strings.Contains(stmts[4].Cypher, "MERGE (p)-[r:ATLANTIS_DEPENDS_ON]->(q)") {
		t.Fatalf("statement 4 should be the ATLANTIS_DEPENDS_ON merge: op=%q cypher=%s", stmts[4].Operation, stmts[4].Cypher)
	}
	if stmts[5].Operation != OperationCanonicalUpsert || !strings.Contains(stmts[5].Cypher, "MERGE (p)-[r:USES_WORKFLOW]->(w)") {
		t.Fatalf("statement 5 should be the USES_WORKFLOW merge: op=%q cypher=%s", stmts[5].Operation, stmts[5].Cypher)
	}
}

// TestAtlantisEdgeStatementsFirstGenerationSkipsStaleEdgeRetract proves the
// generation-scoped cleanup preserves first-generation behavior. There cannot
// be older-generation Atlantis edges to retract on the first projection for a
// repo, so the builder emits only current MERGE statements.
func TestAtlantisEdgeStatementsFirstGenerationSkipsStaleEdgeRetract(t *testing.T) {
	t.Parallel()

	const file = "/repo/atlantis.yaml"
	mat := projector.CanonicalMaterialization{
		FirstGeneration: true,
		GenerationID:    "gen-1",
		RepoPath:        "/repo",
		Entities: []projector.EntityRow{
			atlantisProjectRowEntity("uid-network", "network", file, "network", ""),
			atlantisProjectRowEntity("uid-app", "app", file, "app", "network"),
		},
	}

	stmts := atlantisEdgeStatements(mat)
	if len(stmts) != 2 {
		t.Fatalf("atlantisEdgeStatements() returned %d statements, want 2 merge-only statements", len(stmts))
	}
	for i, stmt := range stmts {
		if stmt.Operation == OperationCanonicalRetract {
			t.Fatalf("statement %d should not retract on first generation: %s", i, stmt.Cypher)
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
	dependsOn := mergeStatementContaining(t, stmts, "MERGE (p)-[r:ATLANTIS_DEPENDS_ON]->(q)")
	dependsRows := dependsOn.Parameters["rows"].([]map[string]any)
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
