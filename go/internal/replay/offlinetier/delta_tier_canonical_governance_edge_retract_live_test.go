// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Canonical governance/structural edge retract coverage (C-14 #4367 retract
// axis): IMPORTS, HELM_VALUE_REFERENCE, MANAGES, ATLANTIS_DEPENDS_ON, and
// USES_WORKFLOW.
//
// IMPORTS and HELM_VALUE_REFERENCE were already safe shapes: IMPORTS runs in
// the CanonicalNodeWriter "retract" phase, which the NornicDB phase-group
// executor dispatches as sequential per-statement autocommit Execute calls
// (executeSequentialRetractPhase) whenever every statement in the phase is
// OperationCanonicalRetract; HELM_VALUE_REFERENCE was already Drain-marked
// (#4476) so the mixed structural_edges phase runs its retract as a
// standalone autocommit statement before the grouped MERGE upserts. Both are
// live-claimed here without a production fix.
//
// MANAGES, ATLANTIS_DEPENDS_ON, and USES_WORKFLOW were NOT Drain-marked
// (canonical_atlantis_edges.go): their UNWIND relationship DELETE statements
// ran grouped inside the same mixed structural_edges ExecuteWrite transaction
// as the sibling MANAGES/DEPENDS_ON/USES_WORKFLOW MERGE upserts, which
// silently no-ops on NornicDB v1.1.11 (#4476, the same class already fixed
// for the Helm and GitLab structural edges). Fixed by marking all three
// Atlantis retract statements Drain=true so the NornicDB phase-group executor
// runs them as standalone autocommit statements, mirroring
// retractGitlabDefinesJobEdgesCypher / retractGitlabNeedsEdgesCypher. The
// unit regression is TestAtlantisEdgeStatementsRetractsStaleEdgesBeforeMerge
// in go/internal/storage/cypher; this is the live NornicDB proof.
//
// The test drives the REAL production canonical node writer
// (cypher.CanonicalNodeWriter.Write) through livePhaseGroupExecutor, which
// mirrors the production NornicDB phase-group write path exactly (see
// executor_test.go). It writes an "in"-scope repository across two
// generations, changing every governance/structural edge's target between
// generations while both old and new endpoints survive, and writes a
// separate "out"-scope repository once (first generation only) as a survivor
// control never touched by the "in"-scope's second write.
//
// Skills active: golang-engineering, eshu-golden-corpus-rigor,
// cypher-query-rigor, concurrency-deadlock-rigor.

package offlinetier_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

const (
	govInRepoID    = "replay-gov-edge:in"
	govOutRepoID   = "replay-gov-edge:out"
	govInRepoPath  = "/repo/gov-edge-in"
	govOutRepoPath = "/repo/gov-edge-out"
)

// govScopeMaterialization builds the CanonicalMaterialization for one scope
// (in or out) carrying a File+Module+IMPORTS edge, an Atlantis governance
// triad (MANAGES/ATLANTIS_DEPENDS_ON/USES_WORKFLOW), and a Helm
// template-value edge. dirSuffix/moduleSuffix/workflowSuffix/helmSuffix pick
// which generation's targets the edges point at, so the same builder produces
// both gen1 and gen2 for the "in" scope (with the targets changed) and the
// single write for the "out" scope.
func govScopeMaterialization(repoID, repoPath, generationID string, firstGeneration bool, targetSuffix string) projector.CanonicalMaterialization {
	filePath := repoPath + "/main.py"
	atlantisFile := repoPath + "/atlantis.yaml"
	chartValuesFile := repoPath + "/chart/values.yaml"
	chartTemplateFile := repoPath + "/chart/templates/deploy.yaml"

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
		Directories: []projector.DirectoryRow{
			{Path: repoPath + "/dir-a", RepoID: repoID},
			{Path: repoPath + "/dir-b", RepoID: repoID},
		},
		Files: []projector.FileRow{
			{Path: filePath, RelativePath: "main.py", Name: "main.py", RepoID: repoID},
		},
		Modules: []projector.ModuleRow{
			{Name: repoID + ":module-a"},
			{Name: repoID + ":module-b"},
		},
		Imports: []projector.ImportRow{
			{FilePath: filePath, ModuleName: repoID + ":module-" + targetSuffix, ImportedName: "x", LineNumber: 1},
		},
		Entities: []projector.EntityRow{
			{
				EntityID: repoID + ":project-network", Label: "AtlantisProject",
				EntityName: "network", FilePath: atlantisFile, RepoID: repoID,
				Metadata: map[string]any{},
			},
			{
				EntityID: repoID + ":project-staging", Label: "AtlantisProject",
				EntityName: "staging", FilePath: atlantisFile, RepoID: repoID,
				Metadata: map[string]any{},
			},
			{
				EntityID: repoID + ":project-app", Label: "AtlantisProject",
				EntityName: "app", FilePath: atlantisFile, RepoID: repoID,
				Metadata: map[string]any{
					"dir":        "dir-" + targetSuffix,
					"depends_on": govAtlantisTargetName(targetSuffix),
					"workflow":   "wf-" + targetSuffix,
				},
			},
			{
				EntityID: repoID + ":wf-a", Label: "AtlantisWorkflow",
				EntityName: "wf-a", FilePath: atlantisFile, RepoID: repoID,
			},
			{
				EntityID: repoID + ":wf-b", Label: "AtlantisWorkflow",
				EntityName: "wf-b", FilePath: atlantisFile, RepoID: repoID,
			},
			{
				EntityID: repoID + ":helm-def-tag", Label: "HelmValueDefinition",
				EntityName: "image.tag", FilePath: chartValuesFile, RepoID: repoID,
			},
			{
				EntityID: repoID + ":helm-def-repo", Label: "HelmValueDefinition",
				EntityName: "image.repo", FilePath: chartValuesFile, RepoID: repoID,
			},
			{
				EntityID: repoID + ":helm-usage", Label: "HelmTemplateValueUsage",
				EntityName: govHelmTargetName(targetSuffix), FilePath: chartTemplateFile, RepoID: repoID,
			},
		},
	}
}

// govAtlantisTargetName maps a target suffix ("a"/"b") to the sibling
// AtlantisProject name "app" depends on, so gen1 depends on "network" and
// gen2 depends on "staging" while both sibling nodes survive unchanged.
func govAtlantisTargetName(targetSuffix string) string {
	if targetSuffix == "a" {
		return "network"
	}
	return "staging"
}

// govHelmTargetName maps a target suffix to the HelmValueDefinition dotted
// path the usage entity names, so gen1 resolves to "image.tag" and gen2
// resolves to "image.repo" while both definitions survive unchanged.
func govHelmTargetName(targetSuffix string) string {
	if targetSuffix == "a" {
		return "image.tag"
	}
	return "image.repo"
}

// TestReducerCanonicalGovernanceEdgeRetractGraphTruth proves the IMPORTS,
// HELM_VALUE_REFERENCE, MANAGES, ATLANTIS_DEPENDS_ON, and USES_WORKFLOW
// retracts each delete only the stale generation's edge while both old and
// new endpoints survive, on a real NornicDB, through the production
// CanonicalNodeWriter.
func TestReducerCanonicalGovernanceEdgeRetractGraphTruth(t *testing.T) {
	if !liveTierEnabled() {
		t.Skipf("set %s=1 (and ESHU_GRAPH_BACKEND/NEO4J_URI/ESHU_NEO4J_DATABASE) to run the canonical governance edge retract tier against a real NornicDB", liveTierEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	exec, writer := openDeltaLiveBackend(ctx, t)
	cleanupGovEdgeScope(ctx, t, exec)
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		cleanupGovEdgeScope(cleanCtx, t, exec)
	})

	// "out" scope: written once (first generation) and never revisited — every
	// edge it writes must survive the "in" scope's second-generation retract.
	outMat := govScopeMaterialization(govOutRepoID, govOutRepoPath, "gen-1", true, "a")
	if err := writer.Write(ctx, outMat); err != nil {
		t.Fatalf("write out-of-scope generation: %v", err)
	}

	// "in" scope gen1: establishes the "a"-targeted edges.
	inGen1 := govScopeMaterialization(govInRepoID, govInRepoPath, "gen-1", true, "a")
	if err := writer.Write(ctx, inGen1); err != nil {
		t.Fatalf("write in-scope gen1: %v", err)
	}
	assertGovEdgeGraphTruth(ctx, t, exec, "a", 1, "gen1: \"a\"-targeted edges present")
	assertGovEdgeGraphTruth(ctx, t, exec, "b", 0, "gen1: \"b\"-targeted edges absent")

	// "in" scope gen2: retargets every edge to "b" while both old and new
	// endpoints (network/staging projects, wf-a/wf-b workflows,
	// image.tag/image.repo definitions, dir-a/dir-b directories,
	// module-a/module-b modules) survive.
	inGen2 := govScopeMaterialization(govInRepoID, govInRepoPath, "gen-2", false, "b")
	if err := writer.Write(ctx, inGen2); err != nil {
		t.Fatalf("write in-scope gen2: %v", err)
	}

	assertGovEdgeGraphTruth(ctx, t, exec, "a", 0, "gen2: stale \"a\"-targeted edges retracted")
	assertGovEdgeGraphTruth(ctx, t, exec, "b", 1, "gen2: fresh \"b\"-targeted edges present")

	// Out-of-scope survivor: untouched by the in-scope gen2 write.
	assertGovOutOfScopeSurvives(ctx, t, exec)

	// Endpoint node survival: both old and new targets persist even though the
	// relationship moved.
	assertGovEndpointsSurvive(ctx, t, exec)
}

// assertGovEdgeGraphTruth asserts every governance/structural edge for the
// "in" scope pointed at the given target suffix has the wanted count.
func assertGovEdgeGraphTruth(ctx context.Context, t *testing.T, exec liveExecutor, targetSuffix string, want int64, msg string) {
	t.Helper()
	repoID := govInRepoID

	assertEdgeCount(ctx, t, exec,
		"MATCH (f:File {path: $f})-[r:IMPORTS]->(:Module {name: $m}) RETURN count(r)",
		map[string]any{"f": govInRepoPath + "/main.py", "m": repoID + ":module-" + targetSuffix},
		want, "IMPORTS "+msg)

	assertEdgeCount(ctx, t, exec,
		"MATCH (u:HelmTemplateValueUsage {uid: $u})-[r:HELM_VALUE_REFERENCE]->(:HelmValueDefinition {uid: $d}) RETURN count(r)",
		map[string]any{"u": repoID + ":helm-usage", "d": repoID + ":helm-def-" + govHelmDefSuffix(targetSuffix)},
		want, "HELM_VALUE_REFERENCE "+msg)

	assertEdgeCount(ctx, t, exec,
		"MATCH (p:AtlantisProject {uid: $p})-[r:MANAGES]->(:Directory {path: $d}) RETURN count(r)",
		map[string]any{"p": repoID + ":project-app", "d": govInRepoPath + "/dir-" + targetSuffix},
		want, "MANAGES "+msg)

	assertEdgeCount(ctx, t, exec,
		"MATCH (p:AtlantisProject {uid: $p})-[r:ATLANTIS_DEPENDS_ON]->(:AtlantisProject {uid: $q}) RETURN count(r)",
		map[string]any{"p": repoID + ":project-app", "q": repoID + ":project-" + govAtlantisProjectSuffix(targetSuffix)},
		want, "ATLANTIS_DEPENDS_ON "+msg)

	assertEdgeCount(ctx, t, exec,
		"MATCH (p:AtlantisProject {uid: $p})-[r:USES_WORKFLOW]->(:AtlantisWorkflow {uid: $w}) RETURN count(r)",
		map[string]any{"p": repoID + ":project-app", "w": repoID + ":wf-" + targetSuffix},
		want, "USES_WORKFLOW "+msg)
}

// govHelmDefSuffix maps a target suffix to the HelmValueDefinition uid suffix
// ("tag" for "a", "repo" for "b").
func govHelmDefSuffix(targetSuffix string) string {
	if targetSuffix == "a" {
		return "tag"
	}
	return "repo"
}

// govAtlantisProjectSuffix maps a target suffix to the ATLANTIS_DEPENDS_ON
// sibling project uid suffix ("network" for "a", "staging" for "b").
func govAtlantisProjectSuffix(targetSuffix string) string {
	if targetSuffix == "a" {
		return "network"
	}
	return "staging"
}

// assertGovOutOfScopeSurvives asserts every out-of-scope edge (written once,
// never revisited) still carries its original "a" targets after the in-scope
// gen2 write.
func assertGovOutOfScopeSurvives(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	repoID := govOutRepoID

	assertEdgeCount(ctx, t, exec,
		"MATCH (f:File {path: $f})-[r:IMPORTS]->(:Module {name: $m}) RETURN count(r)",
		map[string]any{"f": govOutRepoPath + "/main.py", "m": repoID + ":module-a"},
		1, "out-of-scope IMPORTS survives")
	assertEdgeCount(ctx, t, exec,
		"MATCH (u:HelmTemplateValueUsage {uid: $u})-[r:HELM_VALUE_REFERENCE]->(:HelmValueDefinition {uid: $d}) RETURN count(r)",
		map[string]any{"u": repoID + ":helm-usage", "d": repoID + ":helm-def-tag"},
		1, "out-of-scope HELM_VALUE_REFERENCE survives")
	assertEdgeCount(ctx, t, exec,
		"MATCH (p:AtlantisProject {uid: $p})-[r:MANAGES]->(:Directory {path: $d}) RETURN count(r)",
		map[string]any{"p": repoID + ":project-app", "d": govOutRepoPath + "/dir-a"},
		1, "out-of-scope MANAGES survives")
	assertEdgeCount(ctx, t, exec,
		"MATCH (p:AtlantisProject {uid: $p})-[r:ATLANTIS_DEPENDS_ON]->(:AtlantisProject {uid: $q}) RETURN count(r)",
		map[string]any{"p": repoID + ":project-app", "q": repoID + ":project-network"},
		1, "out-of-scope ATLANTIS_DEPENDS_ON survives")
	assertEdgeCount(ctx, t, exec,
		"MATCH (p:AtlantisProject {uid: $p})-[r:USES_WORKFLOW]->(:AtlantisWorkflow {uid: $w}) RETURN count(r)",
		map[string]any{"p": repoID + ":project-app", "w": repoID + ":wf-a"},
		1, "out-of-scope USES_WORKFLOW survives")
}

// assertGovEndpointsSurvive asserts every "in"-scope endpoint node (both the
// old "a"-generation targets and the new "b"-generation targets) is present
// after the gen2 retract, proving the relationship retract removed only the
// edges, never the surviving nodes.
func assertGovEndpointsSurvive(ctx context.Context, t *testing.T, exec liveExecutor) {
	t.Helper()
	repoID := govInRepoID

	for _, q := range []struct {
		cypherText string
		key        string
	}{
		{"MATCH (n:File {path: $u}) RETURN count(n)", govInRepoPath + "/main.py"},
		{"MATCH (n:Module {name: $u}) RETURN count(n)", repoID + ":module-a"},
		{"MATCH (n:Module {name: $u}) RETURN count(n)", repoID + ":module-b"},
		{"MATCH (n:Directory {path: $u}) RETURN count(n)", govInRepoPath + "/dir-a"},
		{"MATCH (n:Directory {path: $u}) RETURN count(n)", govInRepoPath + "/dir-b"},
		{"MATCH (n:AtlantisProject {uid: $u}) RETURN count(n)", repoID + ":project-app"},
		{"MATCH (n:AtlantisProject {uid: $u}) RETURN count(n)", repoID + ":project-network"},
		{"MATCH (n:AtlantisProject {uid: $u}) RETURN count(n)", repoID + ":project-staging"},
		{"MATCH (n:AtlantisWorkflow {uid: $u}) RETURN count(n)", repoID + ":wf-a"},
		{"MATCH (n:AtlantisWorkflow {uid: $u}) RETURN count(n)", repoID + ":wf-b"},
		{"MATCH (n:HelmTemplateValueUsage {uid: $u}) RETURN count(n)", repoID + ":helm-usage"},
		{"MATCH (n:HelmValueDefinition {uid: $u}) RETURN count(n)", repoID + ":helm-def-tag"},
		{"MATCH (n:HelmValueDefinition {uid: $u}) RETURN count(n)", repoID + ":helm-def-repo"},
	} {
		assertEdgeCount(ctx, t, exec, q.cypherText, map[string]any{"u": q.key}, 1, "node survives: "+q.key)
	}
}

// cleanupGovEdgeScope removes every node the in/out scopes create, including
// entities MERGEd by the write templates.
func cleanupGovEdgeScope(ctx context.Context, t *testing.T, exec deltaCleanupExecutor) {
	t.Helper()
	for _, repoID := range []string{govInRepoID, govOutRepoID} {
		for _, label := range []string{
			"AtlantisProject", "AtlantisWorkflow",
			"HelmTemplateValueUsage", "HelmValueDefinition",
			"File", "Directory",
		} {
			if err := exec.Execute(ctx, cypher.Statement{
				Cypher:     "MATCH (n:" + label + ") WHERE n.repo_id = $repo_id DETACH DELETE n",
				Parameters: map[string]any{"repo_id": repoID},
			}); err != nil {
				t.Fatalf("cleanup gov-edge %s nodes for %s: %v", label, repoID, err)
			}
		}
		if err := exec.Execute(ctx, cypher.Statement{
			Cypher:     "MATCH (m:Module) WHERE m.name STARTS WITH $prefix DETACH DELETE m",
			Parameters: map[string]any{"prefix": repoID + ":module-"},
		}); err != nil {
			t.Fatalf("cleanup gov-edge modules for %s: %v", repoID, err)
		}
		if err := exec.Execute(ctx, cypher.Statement{
			Cypher:     "MATCH (r:Repository {id: $repo_id}) DETACH DELETE r",
			Parameters: map[string]any{"repo_id": repoID},
		}); err != nil {
			t.Fatalf("cleanup gov-edge repository for %s: %v", repoID, err)
		}
	}
}
