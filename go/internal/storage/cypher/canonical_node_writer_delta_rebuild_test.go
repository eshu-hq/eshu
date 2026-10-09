// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// Delta-rebuild gap (eshu-hq/eshu#7797). A git scope whose ACTIVE generation is
// a delta carries file facts only for the changed paths. Normal operation is
// cumulative: the full base generation is projected first, then the delta
// retracts and re-writes only its touched paths, so unchanged base Files stay.
// recover-generations re-projects each scope through its active generation
// only (rebuild/reset AffectedGenerationsTemplate). Onto an empty graph that
// writes only the delta's Files; the unchanged base Files return only when the
// full generation forced by the refinalize's reindex request activates.
//
// These tests drive the PRODUCTION projector builder (canonical.BuildMaterialization)
// over production-shaped repository and file facts, and the PRODUCTION
// CanonicalNodeWriter statement builder, through the recording mockExecutor.
// They prove writer logic and writer input only: which File paths the writer
// upserts and which File-deleting statements it emits. They run no Cypher and
// do not prove Neo4j or NornicDB runtime behavior.

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const (
	deltaRebuildRepoID   = "repo-delta-rebuild"
	deltaRebuildRepoPath = "/repos/delta-rebuild"
	deltaRebuildScopeID  = "git:repository:delta-rebuild"
)

// deltaRebuildBaseFiles is the full base generation's file set; src/f2.go is
// the only path the later delta changes.
var deltaRebuildBaseFiles = []string{"src/f1.go", "src/f2.go", "src/f3.go"}

func deltaRebuildScope(previousGenerationExists bool) scope.IngestionScope {
	return scope.IngestionScope{
		ScopeID:                  deltaRebuildScopeID,
		SourceSystem:             "git",
		ScopeKind:                scope.KindRepository,
		CollectorKind:            scope.CollectorGit,
		PartitionKey:             deltaRebuildScopeID,
		PreviousGenerationExists: previousGenerationExists,
		Metadata: map[string]string{
			"repo_id":   deltaRebuildRepoID,
			"repo_path": deltaRebuildRepoPath,
		},
	}
}

func deltaRebuildFileFact(generationID, relativePath string) facts.Envelope {
	return facts.Envelope{
		FactID:       generationID + ":file:" + relativePath,
		ScopeID:      deltaRebuildScopeID,
		GenerationID: generationID,
		FactKind:     "file",
		Payload: map[string]any{
			"repo_id":          deltaRebuildRepoID,
			"relative_path":    relativePath,
			"language":         "go",
			"parsed_file_data": map[string]any{},
		},
	}
}

func deltaRebuildRepositoryFact(generationID string, extra map[string]any) facts.Envelope {
	payload := map[string]any{
		"repo_id": deltaRebuildRepoID,
		"name":    "delta-rebuild",
		"path":    deltaRebuildRepoPath,
	}
	for key, value := range extra {
		payload[key] = value
	}
	return facts.Envelope{
		FactID:       generationID + ":repository",
		ScopeID:      deltaRebuildScopeID,
		GenerationID: generationID,
		FactKind:     "repository",
		Payload:      payload,
	}
}

// deltaRebuildFullGeneration is generation A: is_delta=false, every base file.
func deltaRebuildFullGeneration() (scope.ScopeGeneration, []facts.Envelope) {
	gen := scope.ScopeGeneration{
		GenerationID:    "gen-a-full",
		ScopeID:         deltaRebuildScopeID,
		Status:          scope.GenerationStatusActive,
		TriggerKind:     scope.TriggerKindSnapshot,
		SourceCommitSHA: "a",
	}
	envs := []facts.Envelope{deltaRebuildRepositoryFact(gen.GenerationID, nil)}
	for _, rel := range deltaRebuildBaseFiles {
		envs = append(envs, deltaRebuildFileFact(gen.GenerationID, rel))
	}
	return gen, envs
}

// deltaRebuildDeltaGeneration is generation B: is_delta=true over baseline A.
// It emits a file fact only for the changed path src/f2.go, and its repository
// fact carries the delta payload the git collector writes for a delta.
func deltaRebuildDeltaGeneration() (scope.ScopeGeneration, []facts.Envelope) {
	gen := scope.ScopeGeneration{
		GenerationID:           "gen-b-delta",
		ScopeID:                deltaRebuildScopeID,
		Status:                 scope.GenerationStatusActive,
		TriggerKind:            scope.TriggerKindSnapshot,
		SourceCommitSHA:        "b",
		IsDelta:                true,
		DeltaBaselineCommitSHA: "a",
	}
	envs := []facts.Envelope{
		deltaRebuildRepositoryFact(gen.GenerationID, map[string]any{
			"delta_generation":             true,
			"delta_relative_paths":         []any{"src/f2.go"},
			"delta_deleted_relative_paths": []any{},
		}),
		deltaRebuildFileFact(gen.GenerationID, "src/f2.go"),
	}
	return gen, envs
}

// deltaRebuildWrite builds the materialization with the production builder and
// writes it through the production writer, returning the recorded statements.
func deltaRebuildWrite(
	t *testing.T,
	sc scope.IngestionScope,
	gen scope.ScopeGeneration,
	envs []facts.Envelope,
) (canonical.CanonicalMaterialization, []Statement) {
	t.Helper()
	mat, quarantined := canonical.BuildMaterialization(sc, gen, envs)
	if len(quarantined) != 0 {
		t.Fatalf("BuildMaterialization(%s) quarantined %d facts: %#v", gen.GenerationID, len(quarantined), quarantined)
	}
	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)
	if err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write(%s) error = %v", gen.GenerationID, err)
	}
	return mat, exec.calls
}

// deltaRebuildUpsertedFilePaths returns the File paths the files phase upserts.
func deltaRebuildUpsertedFilePaths(t *testing.T, stmts []Statement) map[string]struct{} {
	t.Helper()
	out := make(map[string]struct{})
	for _, stmt := range canonicalTestStatementsByPhase(stmts, CanonicalPhaseFiles) {
		rows, ok := stmt.Parameters["rows"].([]map[string]any)
		if !ok {
			t.Fatalf("files-phase rows parameter has type %T, want []map[string]any", stmt.Parameters["rows"])
		}
		for _, row := range rows {
			if p, ok := row["path"].(string); ok && p != "" {
				out[p] = struct{}{}
			}
		}
	}
	return out
}

func deltaRebuildQualified(rels ...string) []string {
	out := make([]string, 0, len(rels))
	for _, rel := range rels {
		out = append(out, deltaRebuildRepoPath+"/"+rel)
	}
	return out
}

func deltaRebuildMissing(have map[string]struct{}, want []string) []string {
	var missing []string
	for _, p := range want {
		if _, ok := have[p]; !ok {
			missing = append(missing, p)
		}
	}
	sort.Strings(missing)
	return missing
}

// TestDeltaGenerationNormalProjectionKeepsUnchangedBaseFiles is the control:
// full generation A then delta generation B in normal operation. B must take
// the delta path, upsert only the changed file, and emit no statement that can
// delete an unchanged base File, so the graph stays cumulative: 3 Files.
func TestDeltaGenerationNormalProjectionKeepsUnchangedBaseFiles(t *testing.T) {
	t.Parallel()

	genA, envsA := deltaRebuildFullGeneration()
	matA, stmtsA := deltaRebuildWrite(t, deltaRebuildScope(false), genA, envsA)
	if !matA.FirstGeneration || matA.DeltaProjection {
		t.Fatalf("gen A FirstGeneration=%v DeltaProjection=%v, want true/false", matA.FirstGeneration, matA.DeltaProjection)
	}

	genB, envsB := deltaRebuildDeltaGeneration()
	matB, stmtsB := deltaRebuildWrite(t, deltaRebuildScope(true), genB, envsB)
	if matB.FirstGeneration || !matB.DeltaProjection {
		t.Fatalf("gen B FirstGeneration=%v DeltaProjection=%v, want false/true", matB.FirstGeneration, matB.DeltaProjection)
	}

	for _, stmt := range stmtsB {
		if stmt.Cypher == canonicalNodeRetractFilesCypher || stmt.Cypher == canonicalNodeRetractRemovedFilesCypher {
			t.Fatalf("delta gen B emitted a full File retract:\n%s", stmt.Cypher)
		}
		if strings.Contains(stmt.Cypher, ":File") && strings.Contains(stmt.Cypher, "DELETE f") {
			if stmt.Cypher != canonicalNodeRetractDeltaDeletedFilesCypher {
				t.Fatalf("delta gen B emitted an unexpected File-deleting statement:\n%s", stmt.Cypher)
			}
			paths, _ := stmt.Parameters["file_paths"].([]string)
			if len(paths) != 0 {
				t.Fatalf("delta gen B deleted-file retract paths = %v, want none", paths)
			}
		}
	}

	upsertedB := deltaRebuildUpsertedFilePaths(t, stmtsB)
	if got := len(upsertedB); got != 1 {
		t.Fatalf("delta gen B upserted %d Files, want 1 (only the changed file)", got)
	}
	cumulative := deltaRebuildUpsertedFilePaths(t, stmtsA)
	for p := range upsertedB {
		cumulative[p] = struct{}{}
	}
	if missing := deltaRebuildMissing(cumulative, deltaRebuildQualified(deltaRebuildBaseFiles...)); len(missing) != 0 {
		t.Fatalf("normal projection A then B is missing base Files %v", missing)
	}
	if got := len(cumulative); got != 3 {
		t.Fatalf("normal projection A then B File count = %d, want 3 (base + changed)", got)
	}
}

// TestRecoverStyleRebuildFromActiveDeltaWritesOnlyTheDelta pins the #7797
// limitation as a contract. After the graph is wiped, recover-generations
// re-projects the scope through its ACTIVE generation only, which here is
// delta B. The active file set (newest full A overlaid by delta B) is f1, f2,
// f3, but re-projecting B onto an empty graph writes exactly one File, the
// changed f2. The unchanged base Files come back only when a full generation
// activates; the refinalize records the per-repository reindex request that
// forces one (TestRefinalizeDeltaActiveRequestsFullReindex in
// storage/postgres).
//
// The test asserts the delta-only result on purpose. A change that makes the
// rebuild replay history (the newest full generation, then the deltas) makes it
// fail, so that change must update this contract, the refinalize reindex
// request, and the graph-rebuild-from-facts docs together.
//
// Both claim shapes are covered: PreviousGenerationExists=true (the superseded
// base row still exists) and false (no prior row is visible to the claim).
func TestRecoverStyleRebuildFromActiveDeltaWritesOnlyTheDelta(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		previous bool
	}{
		{name: "prior_generation_visible", previous: true},
		{name: "no_prior_generation", previous: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			genB, envsB := deltaRebuildDeltaGeneration()
			_, stmtsB := deltaRebuildWrite(t, deltaRebuildScope(tc.previous), genB, envsB)

			rebuilt := deltaRebuildUpsertedFilePaths(t, stmtsB)
			got := make([]string, 0, len(rebuilt))
			for p := range rebuilt {
				got = append(got, p)
			}
			sort.Strings(got)
			want := deltaRebuildQualified("src/f2.go")
			if len(got) != 1 || got[0] != want[0] {
				t.Fatalf("recover-style rebuild from active delta %s wrote %d File(s) %v, want exactly the delta's %v; "+
					"if the rebuild now restores the base, update this contract with the #7797 reindex request and docs",
					genB.GenerationID, len(got), got, want)
			}
			if missing := deltaRebuildMissing(rebuilt, deltaRebuildQualified(deltaRebuildBaseFiles...)); len(missing) != 2 {
				t.Fatalf("unchanged base Files missing after a delta-only rebuild = %v, want f1 and f3 (#7797)", missing)
			}
		})
	}
}
