// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func gitlabPipelineEntityRow(uid, filePath string) projector.EntityRow {
	return projector.EntityRow{
		Label:    "GitlabPipeline",
		EntityID: uid,
		FilePath: filePath,
	}
}

func gitlabJobEntityRow(uid, name, filePath, needs string) projector.EntityRow {
	meta := map[string]any{}
	if needs != "" {
		meta["needs"] = needs
	}
	return projector.EntityRow{
		Label:      "GitlabJob",
		EntityID:   uid,
		EntityName: name,
		FilePath:   filePath,
		Metadata:   meta,
	}
}

// TestGitlabEdgeStatementsResolvesDefinesJobAndNeeds proves the builder resolves
// both edges from GitlabPipeline / GitlabJob entities: DEFINES_JOB rows map the
// pipeline uid to each job uid in the same file, and NEEDS rows map a job's uid to
// the sibling job's uid named in needs (resolved within the same .gitlab-ci.yml).
// Endpoints are matched by canonical key (uid), not bound-variable properties.
func TestGitlabEdgeStatementsResolvesDefinesJobAndNeeds(t *testing.T) {
	t.Parallel()

	const file = "/repo/.gitlab-ci.yml"
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		RepoPath:     "/repo",
		Entities: []projector.EntityRow{
			gitlabPipelineEntityRow("uid-pipeline", file),
			gitlabJobEntityRow("uid-build", "build", file, ""),
			gitlabJobEntityRow("uid-test", "test", file, "build"),
		},
	}

	stmts := gitlabEdgeStatements(mat)
	if len(stmts) != 2 {
		t.Fatalf("gitlabEdgeStatements() returned %d statements, want 2 (DEFINES_JOB + NEEDS)", len(stmts))
	}

	definesJob := stmts[0]
	if !strings.Contains(definesJob.Cypher, "DEFINES_JOB") || !strings.Contains(definesJob.Cypher, "GitlabPipeline {uid:") || !strings.Contains(definesJob.Cypher, "GitlabJob {uid:") {
		t.Fatalf("DEFINES_JOB cypher should match by uid: %s", definesJob.Cypher)
	}
	definesRows := definesJob.Parameters["rows"].([]map[string]any)
	if len(definesRows) != 2 {
		t.Fatalf("DEFINES_JOB rows = %d, want 2; %+v", len(definesRows), definesRows)
	}
	wantTargets := map[string]bool{"uid-build": true, "uid-test": true}
	for _, row := range definesRows {
		if row["source_uid"] != "uid-pipeline" {
			t.Fatalf("DEFINES_JOB source_uid = %v, want uid-pipeline", row["source_uid"])
		}
		if !wantTargets[row["target_uid"].(string)] {
			t.Fatalf("DEFINES_JOB unexpected target_uid %v", row["target_uid"])
		}
	}

	needs := stmts[1]
	if !strings.Contains(needs.Cypher, "[r:NEEDS]") {
		t.Fatalf("NEEDS cypher missing NEEDS edge type: %s", needs.Cypher)
	}
	needsRows := needs.Parameters["rows"].([]map[string]any)
	if len(needsRows) != 1 {
		t.Fatalf("NEEDS rows = %d, want 1; %+v", len(needsRows), needsRows)
	}
	if needsRows[0]["source_uid"] != "uid-test" || needsRows[0]["target_uid"] != "uid-build" {
		t.Fatalf("NEEDS row = %+v, want test->build", needsRows[0])
	}

	// generation_id must propagate into every row so the edge carries the
	// projecting generation (the writer SETs r.generation_id = row.generation_id).
	for _, row := range append(append([]map[string]any{}, definesRows...), needsRows...) {
		if row["generation_id"] != "gen-1" {
			t.Fatalf("row %+v missing generation_id=gen-1", row)
		}
	}
}

// TestGitlabEdgeStatementsScopesNeedsPerFile proves needs resolves only against
// sibling jobs in the SAME .gitlab-ci.yml: two jobs sharing a name across
// different files must not produce a cross-file NEEDS edge.
func TestGitlabEdgeStatementsScopesNeedsPerFile(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		RepoPath:     "/repo",
		Entities: []projector.EntityRow{
			gitlabPipelineEntityRow("uid-pa", "/repo/a/.gitlab-ci.yml"),
			gitlabJobEntityRow("uid-a-build", "build", "/repo/a/.gitlab-ci.yml", ""),
			gitlabJobEntityRow("uid-a-test", "test", "/repo/a/.gitlab-ci.yml", "build"),
			// fileB's "test" needs "build", but its "build" sibling lives only in
			// fileA — it must NOT resolve across files.
			gitlabPipelineEntityRow("uid-pb", "/repo/b/.gitlab-ci.yml"),
			gitlabJobEntityRow("uid-b-test", "test", "/repo/b/.gitlab-ci.yml", "build"),
		},
	}

	stmts := gitlabEdgeStatements(mat)
	var needsRows []map[string]any
	for _, stmt := range stmts {
		if strings.Contains(stmt.Cypher, "[r:NEEDS]") {
			needsRows = stmt.Parameters["rows"].([]map[string]any)
		}
	}
	if len(needsRows) != 1 {
		t.Fatalf("NEEDS rows = %d, want 1 (only in-file fileA test->build); %+v", len(needsRows), needsRows)
	}
	if needsRows[0]["source_uid"] != "uid-a-test" || needsRows[0]["target_uid"] != "uid-a-build" {
		t.Fatalf("NEEDS row = %+v, want fileA test->build (no cross-file resolution)", needsRows[0])
	}
}

// TestGitlabEdgeStatementsNilWithoutGitlabEntities proves the builder is a no-op
// for materializations that carry no GitlabPipeline / GitlabJob entity.
func TestGitlabEdgeStatementsNilWithoutGitlabEntities(t *testing.T) {
	t.Parallel()

	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-1",
		RepoPath:     "/repo",
		Entities: []projector.EntityRow{
			{Label: "Function", EntityID: "fn-1"},
		},
	}
	if stmts := gitlabEdgeStatements(mat); stmts != nil {
		t.Fatalf("gitlabEdgeStatements() = %d statements, want nil for non-GitLab repo", len(stmts))
	}
}
