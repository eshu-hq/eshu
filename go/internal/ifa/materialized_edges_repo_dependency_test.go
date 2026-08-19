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
// facts and reproduces exactly the six edges the Terraform/Docker-Compose
// content evidence implies -- never a hand-authored resolved relationship.
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
// guard is not vacuously true: an expected-edge fixture naming the wrong
// target repository must fail closed, not silently pass.
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
	})

	ok, detail := resolveRepoDependencyMaterializedEdges(odu, wrongPath)
	if ok {
		t.Fatalf("resolveRepoDependencyMaterializedEdges() = (true, %q), want (false, ...) for a deliberately wrong fixture", detail)
	}
}

// TestResolveRepoDependencyMaterializedEdgesRejectsRunsOnInFixture proves the
// explicit RUNS_ON carve-out actually fires: a fixture that (wrongly) claims
// RUNS_ON coverage must fail closed with a message naming why, not be
// silently accepted or silently ignored.
func TestResolveRepoDependencyMaterializedEdgesRejectsRunsOnInFixture(t *testing.T) {
	t.Parallel()

	odu := repoDependencyFamilyOdu().Odu
	path := filepath.Join(t.TempDir(), "runs-on-expected-edges.json")
	writeRepoDependencyExpectedEdgesFixture(t, path, []map[string]string{
		{"relationship_type": "PROVISIONS_DEPENDENCY_FOR", "source_entity_id": repoDependencyFamilySourceRepoID, "target_entity_id": repoDependencyFamilyTargetProvisionsRepoID},
		{"relationship_type": "USES_MODULE", "source_entity_id": repoDependencyFamilySourceRepoID, "target_entity_id": repoDependencyFamilyTargetUsesModuleRepoID},
		{"relationship_type": "DISCOVERS_CONFIG_IN", "source_entity_id": repoDependencyFamilySourceRepoID, "target_entity_id": repoDependencyFamilyTargetDiscoversConfigRepoID},
		{"relationship_type": "DEPENDS_ON", "source_entity_id": repoDependencyFamilySourceRepoID, "target_entity_id": repoDependencyFamilyTargetDependsOnRepoID},
		{"relationship_type": "DEPLOYS_FROM", "source_entity_id": repoDependencyFamilySourceRepoID, "target_entity_id": repoDependencyFamilyTargetDeploysFromRepoID},
		{"relationship_type": "READS_CONFIG_FROM", "source_entity_id": repoDependencyFamilySourceRepoID, "target_entity_id": repoDependencyFamilyTargetReadsConfigRepoID},
		{"relationship_type": repoDependencyRunsOnType, "source_entity_id": "some-workload-instance", "target_entity_id": "some-platform"},
	})

	ok, detail := resolveRepoDependencyMaterializedEdges(odu, path)
	if ok {
		t.Fatalf("resolveRepoDependencyMaterializedEdges() = (true, %q), want (false, ...) for a fixture claiming RUNS_ON coverage", detail)
	}
	if !strings.Contains(detail, repoDependencyRunsOnType) {
		t.Fatalf("detail = %q, want it to name %s as the rejected type", detail, repoDependencyRunsOnType)
	}
}

// TestRepoDependencyProvableRegistryTypesExcludesRunsOn proves the carve-out
// helper is not a no-op: RUNS_ON must be the ONLY type it drops, and every
// other registry type must survive.
func TestRepoDependencyProvableRegistryTypesExcludesRunsOn(t *testing.T) {
	t.Parallel()

	registry, err := MaterializedEdgeDomainEdgeTypes(repoDependencyEdgesFamily)
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(%s): %v", repoDependencyEdgesFamily, err)
	}
	if _, ok := registry[repoDependencyRunsOnType]; !ok {
		t.Fatalf("registry does not contain %s; this test's premise (RUNS_ON is IN the registry) no longer holds", repoDependencyRunsOnType)
	}

	provable := repoDependencyProvableRegistryTypes(registry)
	if _, ok := provable[repoDependencyRunsOnType]; ok {
		t.Fatalf("repoDependencyProvableRegistryTypes() kept %s; it must be excluded", repoDependencyRunsOnType)
	}
	if got, want := len(provable), len(registry)-1; got != want {
		t.Fatalf("len(provable) = %d, want %d (registry minus exactly RUNS_ON)", got, want)
	}
	for edgeType := range registry {
		if edgeType == repoDependencyRunsOnType {
			continue
		}
		if _, ok := provable[edgeType]; !ok {
			t.Errorf("repoDependencyProvableRegistryTypes() dropped %s, which is not RUNS_ON and must survive", edgeType)
		}
	}
}

// TestMissingRepoDependencyExpectedTypesCatchesGap proves the coverage check
// is not vacuous: an expected set missing one provable type must be reported,
// and a complete one must report nothing.
func TestMissingRepoDependencyExpectedTypesCatchesGap(t *testing.T) {
	t.Parallel()

	provable := map[string]struct{}{"DEPENDS_ON": {}, "USES_MODULE": {}}
	complete := []ExpectedEdge{
		{RelationshipType: "DEPENDS_ON", SourceEntityID: "a", TargetEntityID: "b"},
		{RelationshipType: "USES_MODULE", SourceEntityID: "a", TargetEntityID: "c"},
	}
	if missing := missingRepoDependencyExpectedTypes(complete, provable); len(missing) != 0 {
		t.Fatalf("missingRepoDependencyExpectedTypes(complete) = %v, want none", missing)
	}

	incomplete := []ExpectedEdge{
		{RelationshipType: "DEPENDS_ON", SourceEntityID: "a", TargetEntityID: "b"},
	}
	missing := missingRepoDependencyExpectedTypes(incomplete, provable)
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
