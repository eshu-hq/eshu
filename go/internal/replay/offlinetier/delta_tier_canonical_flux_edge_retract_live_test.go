// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Flux Kustomization RECONCILES_FROM edge retract coverage (issue #5360 PR B,
// C-14 #4367 retract axis sibling).
//
// RECONCILES_FROM is Drain-marked from its first commit
// (canonical_flux_edges.go: retractFluxReconcilesFromEdgesCypher), mirroring
// the Atlantis governance retract fix (#4476) rather than repeating the bug --
// its UNWIND relationship DELETE runs as a standalone autocommit statement
// before the grouped structural_edges MERGE upserts, so this is a live-claim
// proof of a correctly-shaped statement, not a production fix. The unit
// regression is TestFluxReconcilesFromRetractsStaleEdgesBeforeMerge in
// go/internal/storage/cypher; this is the live NornicDB proof.
//
// The test drives the REAL production canonical node writer
// (cypher.CanonicalNodeWriter.Write) through livePhaseGroupExecutor, mirroring
// TestReducerCanonicalGovernanceEdgeRetractGraphTruth: it writes an "in"-scope
// repository across two generations, changing the FluxKustomization's
// resolved source between generations while both old and new source CRs
// survive, and writes a separate "out"-scope repository once (first
// generation only) as a survivor control never touched by the "in"-scope's
// second write.
//
// Skills active: golang-engineering, eshu-golden-corpus-rigor,
// cypher-query-rigor, eshu-correlation-truth, concurrency-deadlock-rigor.

package offlinetier_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	fluxInRepoID    = "replay-flux-edge:in"
	fluxOutRepoID   = "replay-flux-edge:out"
	fluxInRepoPath  = "/repo/flux-edge-in"
	fluxOutRepoPath = "/repo/flux-edge-out"
)

// fluxScopeMaterialization builds the CanonicalMaterialization for one scope
// (in or out) carrying a FluxKustomization whose sourceRef always names
// "flux-system", plus TWO namespace-exact FluxGitRepository candidates
// declared in different namespaces (source-a-ns / source-b-ns). targetSuffix
// picks which namespace the Kustomization's sourceRef.namespace declares, so
// the same builder produces both gen1 ("a") and gen2 ("b") for the "in" scope
// with the resolved source retargeted, and the single write for the "out"
// scope.
func fluxScopeMaterialization(repoID, repoPath, generationID string, firstGeneration bool, targetSuffix string) projector.CanonicalMaterialization {
	kustomizationFile := repoPath + "/apps.yaml"
	sourcesFile := repoPath + "/sources.yaml"

	return projector.CanonicalMaterialization{
		RepoID:          repoID,
		RepoPath:        repoPath,
		GenerationID:    generationID,
		FirstGeneration: firstGeneration,
		Repository: &projector.RepositoryRow{
			RepoID: repoID,
			Name:   repoID,
			Path:   repoPath,
		},
		Files: []projector.FileRow{
			{Path: kustomizationFile, RelativePath: "apps.yaml", Name: "apps.yaml", RepoID: repoID},
			{Path: sourcesFile, RelativePath: "sources.yaml", Name: "sources.yaml", RepoID: repoID},
		},
		Entities: []projector.EntityRow{
			{
				EntityID: repoID + ":kustomization-apps", Label: "FluxKustomization",
				EntityName: "apps", FilePath: kustomizationFile, RepoID: repoID,
				Metadata: map[string]any{
					"source_ref_kind":      "GitRepository",
					"source_ref_name":      "flux-system",
					"source_ref_namespace": "namespace-" + targetSuffix,
				},
			},
			{
				EntityID: repoID + ":source-a", Label: "FluxGitRepository",
				EntityName: "flux-system", FilePath: sourcesFile, RepoID: repoID,
				Metadata: map[string]any{"namespace": "namespace-a"},
			},
			{
				EntityID: repoID + ":source-b", Label: "FluxGitRepository",
				EntityName: "flux-system", FilePath: sourcesFile, RepoID: repoID,
				Metadata: map[string]any{"namespace": "namespace-b"},
			},
		},
	}
}

func TestFluxScopeMaterializationEntitySourcesHaveFiles(t *testing.T) {
	t.Parallel()
	assertEntitySourceFiles(t, fluxScopeMaterialization(fluxInRepoID, fluxInRepoPath, "gen-1", true, "a"))
}

// TestReducerCanonicalFluxReconcilesFromEdgeRetractGraphTruth proves the
// RECONCILES_FROM retract deletes only the stale generation's edge while both
// old and new source-CR endpoints survive, on a real NornicDB, through the
// production CanonicalNodeWriter.
func TestReducerCanonicalFluxReconcilesFromEdgeRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the canonical Flux edge retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, writer := openDeltaLiveBackend(ctx, t)
	cleanupFluxEdgeScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupFluxEdgeScope(cleanCtx, t, exec)
	})

	// "out" scope: written once (first generation) and never revisited -- its
	// RECONCILES_FROM edge must survive the "in" scope's second-generation
	// retract.
	outMat := fluxScopeMaterialization(fluxOutRepoID, fluxOutRepoPath, "gen-1", true, "a")
	if err := writer.Write(ctx, outMat); err != nil {
		t.Fatalf("write out-of-scope generation: %v", err)
	}

	// "in" scope gen1: resolves against the namespace-a source.
	inGen1 := fluxScopeMaterialization(fluxInRepoID, fluxInRepoPath, "gen-1", true, "a")
	if err := writer.Write(ctx, inGen1); err != nil {
		t.Fatalf("write in-scope gen1: %v", err)
	}
	assertFluxEndpointsSurvive(ctx, t, exec)
	assertFluxEdgeGraphTruth(ctx, t, exec, "a", 1, "gen1: namespace-a-targeted edge present")
	assertFluxEdgeGraphTruth(ctx, t, exec, "b", 0, "gen1: namespace-b-targeted edge absent")

	// "in" scope gen2: retargets the sourceRef namespace to "b" while both
	// source-a and source-b FluxGitRepository nodes survive.
	inGen2 := fluxScopeMaterialization(fluxInRepoID, fluxInRepoPath, "gen-2", false, "b")
	if err := writer.Write(ctx, inGen2); err != nil {
		t.Fatalf("write in-scope gen2: %v", err)
	}

	assertFluxEdgeGraphTruth(ctx, t, exec, "a", 0, "gen2: stale namespace-a-targeted edge retracted")
	assertFluxEdgeGraphTruth(ctx, t, exec, "b", 1, "gen2: fresh namespace-b-targeted edge present")

	// Out-of-scope survivor: untouched by the in-scope gen2 write.
	assertFluxOutOfScopeSurvives(ctx, t, exec)

	// Endpoint node survival: both source-a and source-b persist even though
	// the relationship moved.
	assertFluxEndpointsSurvive(ctx, t, exec)
}

// assertFluxEdgeGraphTruth asserts the "in" scope's RECONCILES_FROM edge
// pointed at the given namespace suffix has the wanted count.
func assertFluxEdgeGraphTruth(ctx context.Context, t *testing.T, exec liveExecutor, targetSuffix string, want int64, msg string) {
	t.Helper()
	repoID := fluxInRepoID

	assertEdgeCount(ctx, t, exec,
		"MATCH (k:FluxKustomization {uid: $k})-[r:RECONCILES_FROM]->(:FluxGitRepository {uid: $s}) RETURN count(r)",
		map[string]any{"k": repoID + ":kustomization-apps", "s": repoID + ":source-" + targetSuffix},
		want, "RECONCILES_FROM "+msg)
}

// assertFluxOutOfScopeSurvives asserts the out-of-scope RECONCILES_FROM edge
// (written once, never revisited) still targets its original namespace-a
// source after the in-scope gen2 write.
func assertFluxOutOfScopeSurvives(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	repoID := fluxOutRepoID

	assertEdgeCount(ctx, t, exec,
		"MATCH (k:FluxKustomization {uid: $k})-[r:RECONCILES_FROM]->(:FluxGitRepository {uid: $s}) RETURN count(r)",
		map[string]any{"k": repoID + ":kustomization-apps", "s": repoID + ":source-a"},
		1, "out-of-scope RECONCILES_FROM survives")
}

// assertFluxEndpointsSurvive asserts every "in"-scope endpoint node (the
// Kustomization and both source-a/source-b FluxGitRepository nodes) is
// present after the gen2 retract, proving the relationship retract removed
// only the edge, never the surviving nodes.
func assertFluxEndpointsSurvive(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	repoID := fluxInRepoID

	for _, q := range []struct {
		cypherText string
		key        string
	}{
		{"MATCH (n:FluxKustomization {uid: $u}) RETURN count(n)", repoID + ":kustomization-apps"},
		{"MATCH (n:FluxGitRepository {uid: $u}) RETURN count(n)", repoID + ":source-a"},
		{"MATCH (n:FluxGitRepository {uid: $u}) RETURN count(n)", repoID + ":source-b"},
	} {
		assertEdgeCount(ctx, t, exec, q.cypherText, map[string]any{"u": q.key}, 1, "node survives: "+q.key)
	}
}

// cleanupFluxEdgeScope removes every node the in/out scopes create.
func cleanupFluxEdgeScope(ctx context.Context, t *testing.T, exec deltaCleanupExecutor) {
	t.Helper()
	for _, repoID := range []string{fluxInRepoID, fluxOutRepoID} {
		for _, label := range []string{"FluxKustomization", "FluxGitRepository"} {
			if err := exec.Execute(ctx, cypher.Statement{
				Cypher:     "MATCH (n:" + label + ") WHERE n.repo_id = $repo_id DETACH DELETE n",
				Parameters: map[string]any{"repo_id": repoID},
			}); err != nil {
				t.Fatalf("cleanup flux-edge %s nodes for %s: %v", label, repoID, err)
			}
		}
		if err := exec.Execute(ctx, cypher.Statement{
			Cypher:     "MATCH (r:Repository {id: $repo_id}) DETACH DELETE r",
			Parameters: map[string]any{"repo_id": repoID},
		}); err != nil {
			t.Fatalf("cleanup flux-edge repository for %s: %v", repoID, err)
		}
	}
}
