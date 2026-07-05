// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// mergeStatementContaining returns the single OperationCanonicalUpsert statement
// whose cypher contains marker, failing if none or more than one matches. It lets
// tests locate a MERGE upsert by its edge clause regardless of the retract
// statements now interleaved ahead of it.
func mergeStatementContaining(t *testing.T, stmts []Statement, marker string) Statement {
	t.Helper()
	var found []Statement
	for _, stmt := range stmts {
		if stmt.Operation == OperationCanonicalUpsert && strings.Contains(stmt.Cypher, marker) {
			found = append(found, stmt)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly 1 upsert statement containing %q, got %d", marker, len(found))
	}
	return found[0]
}

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
	// 2 generation-scoped retracts (DEFINES_JOB, NEEDS) precede the 2 MERGE upserts.
	if len(stmts) != 4 {
		t.Fatalf("gitlabEdgeStatements() returned %d statements, want 4 (2 retract + DEFINES_JOB + NEEDS)", len(stmts))
	}

	definesJob := mergeStatementContaining(t, stmts, "MERGE (p)-[r:DEFINES_JOB]->(j)")
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

	needs := mergeStatementContaining(t, stmts, "MERGE (a)-[r:NEEDS]->(b)")
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

// TestGitlabEdgeStatementsRetractsStaleEdgesBeforeMerge proves the builder emits
// generation-scoped retraction for NEEDS and DEFINES_JOB BEFORE the MERGE
// statements, so a re-projection where a job's needs change (but both endpoints
// survive into the current generation) removes the stale job-to-job NEEDS and
// pipeline-to-job DEFINES_JOB edges that repository_cleanup / entity_retract do
// not touch. The retracts are scoped to THIS materialization's source uids and
// delete only projector/canonical edges whose generation_id differs from the
// current one; the subsequent MERGE rewrites the current edges.
func TestGitlabEdgeStatementsRetractsStaleEdgesBeforeMerge(t *testing.T) {
	t.Parallel()

	const file = "/repo/.gitlab-ci.yml"
	mat := projector.CanonicalMaterialization{
		GenerationID: "gen-2",
		RepoPath:     "/repo",
		Entities: []projector.EntityRow{
			gitlabPipelineEntityRow("uid-pipeline", file),
			gitlabJobEntityRow("uid-build", "build", file, ""),
			gitlabJobEntityRow("uid-test", "test", file, "build"),
		},
	}

	stmts := gitlabEdgeStatements(mat)
	if len(stmts) != 4 {
		t.Fatalf("gitlabEdgeStatements() returned %d statements, want 4 (2 retract + 2 merge)", len(stmts))
	}

	// First two statements must be the retracts, in DEFINES_JOB then NEEDS order,
	// each an OperationCanonicalRetract.
	definesRetract := stmts[0]
	needsRetract := stmts[1]
	for i, rt := range []Statement{definesRetract, needsRetract} {
		if rt.Operation != OperationCanonicalRetract {
			t.Fatalf("statement %d Operation = %q, want %q (retract must precede merge)", i, rt.Operation, OperationCanonicalRetract)
		}
		if !rt.Drain {
			t.Fatalf("statement %d Drain = false, want true so mixed structural_edges relationship retracts run autocommit", i)
		}
	}

	// DEFINES_JOB retract: scoped to pipeline source uids, generation-guarded.
	if !strings.Contains(definesRetract.Cypher, "GitlabPipeline {uid: uid}") ||
		!strings.Contains(definesRetract.Cypher, "[r:DEFINES_JOB]") ||
		!strings.Contains(definesRetract.Cypher, "r.generation_id <> $generation_id") ||
		!strings.Contains(definesRetract.Cypher, "r.evidence_source = 'projector/canonical'") ||
		!strings.Contains(definesRetract.Cypher, "DELETE r") {
		t.Fatalf("DEFINES_JOB retract cypher wrong shape: %s", definesRetract.Cypher)
	}
	if strings.Contains(definesRetract.Cypher, "NEEDS") {
		t.Fatalf("DEFINES_JOB retract must be per-label, not multi-type: %s", definesRetract.Cypher)
	}
	definesSources, ok := definesRetract.Parameters["source_uids"].([]string)
	if !ok || len(definesSources) != 1 || definesSources[0] != "uid-pipeline" {
		t.Fatalf("DEFINES_JOB retract source_uids = %#v, want [uid-pipeline]", definesRetract.Parameters["source_uids"])
	}
	if definesRetract.Parameters["generation_id"] != "gen-2" {
		t.Fatalf("DEFINES_JOB retract generation_id = %v, want gen-2", definesRetract.Parameters["generation_id"])
	}

	// NEEDS retract: scoped to job source uids, generation-guarded.
	if !strings.Contains(needsRetract.Cypher, "GitlabJob {uid: uid}") ||
		!strings.Contains(needsRetract.Cypher, "[r:NEEDS]") ||
		!strings.Contains(needsRetract.Cypher, "r.generation_id <> $generation_id") ||
		!strings.Contains(needsRetract.Cypher, "r.evidence_source = 'projector/canonical'") ||
		!strings.Contains(needsRetract.Cypher, "DELETE r") {
		t.Fatalf("NEEDS retract cypher wrong shape: %s", needsRetract.Cypher)
	}
	if strings.Contains(needsRetract.Cypher, "DEFINES_JOB") {
		t.Fatalf("NEEDS retract must be per-label, not multi-type: %s", needsRetract.Cypher)
	}
	needsSources, ok := needsRetract.Parameters["source_uids"].([]string)
	if !ok {
		t.Fatalf("NEEDS retract source_uids missing/wrong type: %#v", needsRetract.Parameters["source_uids"])
	}
	// Every GitlabJob uid in the materialization is a potential NEEDS source.
	wantNeedsSources := map[string]bool{"uid-build": true, "uid-test": true}
	if len(needsSources) != len(wantNeedsSources) {
		t.Fatalf("NEEDS retract source_uids = %#v, want both job uids", needsSources)
	}
	for _, uid := range needsSources {
		if !wantNeedsSources[uid] {
			t.Fatalf("NEEDS retract unexpected source uid %q", uid)
		}
	}
	if needsRetract.Parameters["generation_id"] != "gen-2" {
		t.Fatalf("NEEDS retract generation_id = %v, want gen-2", needsRetract.Parameters["generation_id"])
	}

	// The trailing two statements remain the MERGE upserts.
	if stmts[2].Operation != OperationCanonicalUpsert || !strings.Contains(stmts[2].Cypher, "MERGE (p)-[r:DEFINES_JOB]->(j)") {
		t.Fatalf("statement 2 should be the DEFINES_JOB merge: op=%q cypher=%s", stmts[2].Operation, stmts[2].Cypher)
	}
	if stmts[3].Operation != OperationCanonicalUpsert || !strings.Contains(stmts[3].Cypher, "MERGE (a)-[r:NEEDS]->(b)") {
		t.Fatalf("statement 3 should be the NEEDS merge: op=%q cypher=%s", stmts[3].Operation, stmts[3].Cypher)
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
	needsMerge := mergeStatementContaining(t, stmts, "MERGE (a)-[r:NEEDS]->(b)")
	needsRows := needsMerge.Parameters["rows"].([]map[string]any)
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
