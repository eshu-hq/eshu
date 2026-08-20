// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// TestResolveRepoDependencyMaterializedEdgesReproducesExpectedSet pins the
// repo_dependency vacuity guard against the family's hand-derived
// expected-edge-set fixture. It proves the guard runs the real
// DiscoveredEvidence -> relationships.Resolve ->
// reducer.ExtractRepoDependencyIntentRows chain over the cataloged Odù's own
// facts and reproduces exactly the seven edges the Terraform, Docker Compose,
// ArgoCD, and Kubernetes evidence implies -- never a hand-authored resolved
// relationship or endpoint identity.
func TestResolveRepoDependencyMaterializedEdgesReproducesExpectedSet(t *testing.T) {
	t.Parallel()

	odu := repoDependencyFamilyOdu().Odu
	ok, detail := resolveRepoDependencyMaterializedEdges(odu, repoDependencyFamilyExpectedEdgesPath(repoRootDir(t)))
	if !ok {
		t.Fatalf("resolveRepoDependencyMaterializedEdges() = (false, %q), want (true, ...)", detail)
	}
	if !strings.Contains(detail, odu.Name) {
		t.Fatalf("detail = %q, want it to name the odù %q", detail, odu.Name)
	}
}

// TestRepoDependencyFamilyResolvesThroughTheManifestResolver proves the
// vacuity guard is reachable by surface name through
// MaterializedEdgeOduResolver, not only by calling
// resolveRepoDependencyMaterializedEdges directly. This is the same class of
// regression test #5993 (deployable_unit_edges) needed: the guard can be
// fully correct and fully unit-tested while Resolve's dispatch switch still
// has no case for it. Mirrors
// TestDeployableUnitFamilyResolvesThroughTheManifestResolver.
func TestRepoDependencyFamilyResolvesThroughTheManifestResolver(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	resolver := MaterializedEdgeOduResolver{Catalog: CatalogByName(), RepoRoot: repoRoot}

	ok, detail := resolver.Resolve(replaycoverage.CoverageEntry{
		Surface:      MaterializedEdgeSurfacePrefix + repoDependencyEdgesFamily,
		Scenario:     replaycoverage.ScenarioOdu,
		ScenarioType: replaycoverage.ScenarioTypeBaseline,
		Ref:          repoDependencyFamilyOduName,
	})
	if !ok {
		t.Fatalf("resolver.Resolve for %s: %s", repoDependencyEdgesFamily, detail)
	}
	t.Logf("%s", detail)
}

// TestResolveRepoDependencyMaterializedEdgesRejectsWrongExpectedSet proves the
// guard is not vacuously true: a complete expected-edge fixture naming the
// wrong target repository must fail closed, not silently pass.
func TestResolveRepoDependencyMaterializedEdgesRejectsWrongExpectedSet(t *testing.T) {
	t.Parallel()

	odu := repoDependencyFamilyOdu().Odu
	wrongPath := filepath.Join(t.TempDir(), "wrong-expected-edges.json")
	writeRepoDependencyExpectedEdgesFixture(t, wrongPath, []map[string]string{
		{"relationship_type": "PROVISIONS_DEPENDENCY_FOR", "source_entity_id": repoDependencyFamilySourceRepoID, "target_entity_id": "not-the-real-target"},
		{"relationship_type": "USES_MODULE", "source_entity_id": repoDependencyFamilySourceRepoID, "target_entity_id": repoDependencyFamilyTargetUsesModuleRepoID},
		{"relationship_type": "DISCOVERS_CONFIG_IN", "source_entity_id": repoDependencyFamilySourceRepoID, "target_entity_id": repoDependencyFamilyTargetDiscoversConfigRepoID},
		{"relationship_type": "DEPENDS_ON", "source_entity_id": repoDependencyFamilySourceRepoID, "target_entity_id": repoDependencyFamilyTargetDependsOnRepoID},
		{"relationship_type": "DEPLOYS_FROM", "source_entity_id": repoDependencyFamilySourceRepoID, "target_entity_id": repoDependencyFamilyTargetDeploysFromRepoID},
		{"relationship_type": "READS_CONFIG_FROM", "source_entity_id": repoDependencyFamilySourceRepoID, "target_entity_id": repoDependencyFamilyTargetReadsConfigRepoID},
		{"relationship_type": "RUNS_ON", "source_entity_id": "workload-instance:" + repoDependencyFamilySourceName + ":" + repoDependencyFamilyEnvironment, "target_entity_id": "platform:kubernetes:none:cluster/" + repoDependencyFamilyDestinationName + ":none:none"},
	})

	ok, detail := resolveRepoDependencyMaterializedEdges(odu, wrongPath)
	if ok {
		t.Fatalf("resolveRepoDependencyMaterializedEdges() = (true, %q), want (false, ...) for a deliberately wrong fixture", detail)
	}
}

func TestRepoDependencyFamilyRunsOnPrerequisites(t *testing.T) {
	t.Parallel()

	odu := repoDependencyFamilyOdu().Odu
	instanceID, platformID, err := repoDependencyFamilyRunsOnPrerequisites(odu)
	if err != nil {
		t.Fatalf("repoDependencyFamilyRunsOnPrerequisites() error = %v, want nil", err)
	}
	if want := "workload-instance:" + repoDependencyFamilySourceName + ":" + repoDependencyFamilyEnvironment; instanceID != want {
		t.Fatalf("instance id = %q, want %q", instanceID, want)
	}
	if want := "platform:kubernetes:none:cluster/" + repoDependencyFamilyDestinationName + ":none:none"; platformID != want {
		t.Fatalf("platform id = %q, want %q", platformID, want)
	}
}

func TestRepoDependencyFamilyRunsOnPrerequisitesRejectsMissingGraphID(t *testing.T) {
	t.Parallel()

	odu := repoDependencyFamilyOdu().Odu
	for i := range odu.Facts {
		if odu.Facts[i].FactKind == repositoryFactKind && anyToStringValue(odu.Facts[i].Payload["repo_id"]) == repoDependencyFamilySourceRepoID {
			delete(odu.Facts[i].Payload, "graph_id")
		}
	}
	_, _, err := repoDependencyFamilyRunsOnPrerequisites(odu)
	if err == nil || !strings.Contains(err.Error(), "Repository -> Workload prerequisite") {
		t.Fatalf("repoDependencyFamilyRunsOnPrerequisites() error = %v, want missing Repository -> Workload prerequisite", err)
	}
}

func TestRepoDependencyFamilyRunsOnPrerequisitesRejectsAmbiguousInstances(t *testing.T) {
	t.Parallel()

	odu := repoDependencyFamilyOdu().Odu
	for i := range odu.Facts {
		if odu.Facts[i].StableFactKey != "file:"+repoDependencyFamilySourceRepoID+":deploy/application.yaml" {
			continue
		}
		parsed, ok := odu.Facts[i].Payload["parsed_file_data"].(map[string]any)
		if !ok {
			t.Fatalf("fixture parsed_file_data has type %T, want map[string]any", odu.Facts[i].Payload["parsed_file_data"])
		}
		parsed["k8s_resources"] = []any{
			map[string]any{"kind": "Deployment", "namespace": repoDependencyFamilyEnvironment},
			map[string]any{"kind": "Deployment", "namespace": "stage"},
		}
	}
	_, _, err := repoDependencyFamilyRunsOnPrerequisites(odu)
	if err == nil || !strings.Contains(err.Error(), "got 2") || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("repoDependencyFamilyRunsOnPrerequisites() error = %v, want deterministic ambiguity failure naming 2 instances", err)
	}
}

// TestMissingRepoDependencyExpectedTypesCatchesGap proves the coverage check
// is not vacuous: an expected set missing one registered type must be reported,
// and a complete one must report nothing.
func TestMissingRepoDependencyExpectedTypesCatchesGap(t *testing.T) {
	t.Parallel()

	registry := map[string]struct{}{"DEPENDS_ON": {}, "USES_MODULE": {}}
	complete := []ExpectedEdge{
		{RelationshipType: "DEPENDS_ON", SourceEntityID: "a", TargetEntityID: "b"},
		{RelationshipType: "USES_MODULE", SourceEntityID: "a", TargetEntityID: "c"},
	}
	if missing := missingRepoDependencyExpectedTypes(complete, registry); len(missing) != 0 {
		t.Fatalf("missingRepoDependencyExpectedTypes(complete) = %v, want none", missing)
	}

	incomplete := []ExpectedEdge{
		{RelationshipType: "DEPENDS_ON", SourceEntityID: "a", TargetEntityID: "b"},
	}
	missing := missingRepoDependencyExpectedTypes(incomplete, registry)
	if len(missing) != 1 || missing[0] != "USES_MODULE" {
		t.Fatalf("missingRepoDependencyExpectedTypes(incomplete) = %v, want [USES_MODULE]", missing)
	}
}

// TestCompareRepoDependencyExpectedEdgesCatchesExtraAndMissing proves the
// exact-set comparison is not vacuous in either direction: a spurious extra
// edge and a dropped expected edge must both fail, and an exact match must
// report clean.
func TestCompareRepoDependencyExpectedEdgesCatchesExtraAndMissing(t *testing.T) {
	t.Parallel()

	expected := []ExpectedEdge{
		{RelationshipType: "DEPENDS_ON", SourceEntityID: "repo-a", TargetEntityID: "repo-b"},
	}

	if detail := compareRepoDependencyExpectedEdges("odu:test", expected, expected); detail != "" {
		t.Fatalf("compareRepoDependencyExpectedEdges(exact match) = %q, want empty", detail)
	}

	extra := append(append([]ExpectedEdge{}, expected...), ExpectedEdge{
		RelationshipType: "USES_MODULE", SourceEntityID: "repo-a", TargetEntityID: "repo-c",
	})
	if detail := compareRepoDependencyExpectedEdges("odu:test", expected, extra); detail == "" {
		t.Fatal("compareRepoDependencyExpectedEdges(spurious extra edge) = \"\", want a non-empty failure detail")
	}

	if detail := compareRepoDependencyExpectedEdges("odu:test", expected, nil); detail == "" {
		t.Fatal("compareRepoDependencyExpectedEdges(missing edge) = \"\", want a non-empty failure detail")
	}
}

// writeRepoDependencyExpectedEdgesFixture writes a throwaway expected-edges
// JSON fixture matching the shape LoadExpectedEdges parses
// (sqlRelationshipExpectedEdgesFile via loadSQLRelationshipExpectedEdges).
func writeRepoDependencyExpectedEdgesFixture(t *testing.T, path string, edges []map[string]string) {
	t.Helper()
	fixture := struct {
		Odu   string `json:"odu"`
		Edges []struct {
			RelationshipType string `json:"relationship_type"`
			SourceEntityID   string `json:"source_entity_id"`
			TargetEntityID   string `json:"target_entity_id"`
		} `json:"edges"`
	}{
		Odu: repoDependencyFamilyOduName,
	}
	for _, edge := range edges {
		fixture.Edges = append(fixture.Edges, struct {
			RelationshipType string `json:"relationship_type"`
			SourceEntityID   string `json:"source_entity_id"`
			TargetEntityID   string `json:"target_entity_id"`
		}{
			RelationshipType: edge["relationship_type"],
			SourceEntityID:   edge["source_entity_id"],
			TargetEntityID:   edge["target_entity_id"],
		})
	}
	raw, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("json.Marshal(fixture) error = %v, want nil", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("os.WriteFile(%s) error = %v, want nil", path, err)
	}
}
