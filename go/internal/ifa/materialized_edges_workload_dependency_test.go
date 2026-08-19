// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// TestResolveWorkloadDependencyMaterializedEdgesReproducesExpectedSet pins
// the workload_dependency vacuity guard against the family's hand-derived
// expected-edge-set fixture. It proves the guard runs the real two-seam
// chain -- DiscoveredEvidence -> relationships.Resolve ->
// reducer.ExtractRepoDependencyIntentRows for the repo edge, and
// reducer.ExtractWorkloadCandidates -> BuildProjectionRowsWithInfrastructurePlatforms
// for the workload map -- over the cataloged Odù's own facts, feeding the
// real reducer.ReconcileWorkloadDependencyEdges, and reproduces exactly the
// one edge the positive pair's evidence implies.
func TestResolveWorkloadDependencyMaterializedEdgesReproducesExpectedSet(t *testing.T) {
	t.Parallel()

	odu := workloadDependencyFamilyOdu().Odu
	ok, detail := resolveWorkloadDependencyMaterializedEdges(odu, workloadDependencyFamilyExpectedEdgesPath(repoRootDir(t)))
	if !ok {
		t.Fatalf("resolveWorkloadDependencyMaterializedEdges() = (false, %q), want (true, ...)", detail)
	}
	if !strings.Contains(detail, odu.Name) {
		t.Fatalf("detail = %q, want it to name the odù %q", detail, odu.Name)
	}
}

// TestWorkloadDependencyFamilyResolvesThroughTheManifestResolver proves the
// vacuity guard is reachable by surface name through
// MaterializedEdgeOduResolver, not only by calling
// resolveWorkloadDependencyMaterializedEdges directly. Mirrors
// TestRepoDependencyFamilyResolvesThroughTheManifestResolver.
func TestWorkloadDependencyFamilyResolvesThroughTheManifestResolver(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	resolver := MaterializedEdgeOduResolver{Catalog: CatalogByName(), RepoRoot: repoRoot}

	ok, detail := resolver.Resolve(replaycoverage.CoverageEntry{
		Surface:      MaterializedEdgeSurfacePrefix + workloadDependencyEdgesFamily,
		Scenario:     replaycoverage.ScenarioOdu,
		ScenarioType: replaycoverage.ScenarioTypeBaseline,
		Ref:          workloadDependencyFamilyOduName,
	})
	if !ok {
		t.Fatalf("resolver.Resolve for %s: %s", workloadDependencyEdgesFamily, detail)
	}
	t.Logf("%s", detail)
}

// TestResolveWorkloadDependencyMaterializedEdgesRejectsWrongExpectedSet
// proves the guard is not vacuously true: an expected-edge fixture naming
// the wrong target workload must fail closed, not silently pass.
func TestResolveWorkloadDependencyMaterializedEdgesRejectsWrongExpectedSet(t *testing.T) {
	t.Parallel()

	odu := workloadDependencyFamilyOdu().Odu
	wrongPath := filepath.Join(t.TempDir(), "wrong-expected-edges.json")
	writeWorkloadDependencyExpectedEdgesFixture(t, wrongPath, []map[string]string{
		{"relationship_type": "DEPENDS_ON", "source_entity_id": "workload:workload-dependency-source", "target_entity_id": "workload:not-the-real-target"},
	})

	ok, detail := resolveWorkloadDependencyMaterializedEdges(odu, wrongPath)
	if ok {
		t.Fatalf("resolveWorkloadDependencyMaterializedEdges() = (true, %q), want (false, ...) for a deliberately wrong fixture", detail)
	}
}

// TestResolveWorkloadDependencyMaterializedEdgesRejectsMultiWorkloadPairInFixture
// proves the multi-workload drop reason actually fires: a fixture that
// (wrongly) claims the multi-workload pair as an admitted edge must fail
// closed, not be silently accepted.
func TestResolveWorkloadDependencyMaterializedEdgesRejectsMultiWorkloadPairInFixture(t *testing.T) {
	t.Parallel()

	odu := workloadDependencyFamilyOdu().Odu
	path := filepath.Join(t.TempDir(), "multi-workload-expected-edges.json")
	writeWorkloadDependencyExpectedEdgesFixture(t, path, []map[string]string{
		{"relationship_type": "DEPENDS_ON", "source_entity_id": "workload:workload-dependency-source", "target_entity_id": "workload:workload-dependency-target"},
		{"relationship_type": "DEPENDS_ON", "source_entity_id": "workload:workload-dependency-multi-source", "target_entity_id": "workload:workload-dependency-multi-target"},
	})

	ok, detail := resolveWorkloadDependencyMaterializedEdges(odu, path)
	if ok {
		t.Fatalf("resolveWorkloadDependencyMaterializedEdges() = (true, %q), want (false, ...) for a fixture claiming the multi-workload pair admits", detail)
	}
}

// TestResolveWorkloadDependencyMaterializedEdgesRejectsOrphanPairInFixture
// proves the neither-repo-is-current drop reason actually fires,
// independently of the multi-workload drop reason above.
func TestResolveWorkloadDependencyMaterializedEdgesRejectsOrphanPairInFixture(t *testing.T) {
	t.Parallel()

	odu := workloadDependencyFamilyOdu().Odu
	path := filepath.Join(t.TempDir(), "orphan-expected-edges.json")
	writeWorkloadDependencyExpectedEdgesFixture(t, path, []map[string]string{
		{"relationship_type": "DEPENDS_ON", "source_entity_id": "workload:workload-dependency-source", "target_entity_id": "workload:workload-dependency-target"},
		{"relationship_type": "DEPENDS_ON", "source_entity_id": "workload:workload-dependency-orphan-source-persisted", "target_entity_id": "workload:workload-dependency-orphan-target-persisted"},
	})

	ok, detail := resolveWorkloadDependencyMaterializedEdges(odu, path)
	if ok {
		t.Fatalf("resolveWorkloadDependencyMaterializedEdges() = (true, %q), want (false, ...) for a fixture claiming the orphan pair admits", detail)
	}
}

// TestWorkloadDependencyAssertDropReasonsCatchesEitherLeak proves
// workloadDependencyAssertDropReasons is not vacuous: it must reject the
// multi-workload pair leaking in, the orphan pair leaking in, and an empty
// admitted set (which would prove neither drop reason selective), while
// accepting exactly the positive pair alone.
func TestWorkloadDependencyAssertDropReasonsCatchesEitherLeak(t *testing.T) {
	t.Parallel()

	positive := reducer.WorkloadDependencyEdgeRow{
		RepoID: workloadDependencyFamilySourceRepoID, WorkloadID: "workload:workload-dependency-source",
		TargetRepoID: workloadDependencyFamilyTargetRepoID, TargetWorkloadID: "workload:workload-dependency-target",
	}
	multiLeak := reducer.WorkloadDependencyEdgeRow{
		RepoID: workloadDependencyFamilyMultiSourceRepoID, WorkloadID: "workload:workload-dependency-multi-source",
		TargetRepoID: workloadDependencyFamilyMultiTargetRepoID, TargetWorkloadID: "workload:workload-dependency-multi-target",
	}
	orphanLeak := reducer.WorkloadDependencyEdgeRow{
		RepoID: workloadDependencyFamilyOrphanSourceRepoID, WorkloadID: workloadDependencyFamilyOrphanSourcePersistedWorkloadID,
		TargetRepoID: workloadDependencyFamilyOrphanTargetRepoID, TargetWorkloadID: workloadDependencyFamilyOrphanTargetPersistedWorkloadID,
	}

	if detail := workloadDependencyAssertDropReasons("odu:test", []reducer.WorkloadDependencyEdgeRow{positive}); detail != "" {
		t.Fatalf("workloadDependencyAssertDropReasons(positive only) = %q, want empty", detail)
	}
	if detail := workloadDependencyAssertDropReasons("odu:test", []reducer.WorkloadDependencyEdgeRow{positive, multiLeak}); detail == "" {
		t.Fatal("workloadDependencyAssertDropReasons(multi-workload leak) = \"\", want a non-empty failure detail")
	}
	if detail := workloadDependencyAssertDropReasons("odu:test", []reducer.WorkloadDependencyEdgeRow{positive, orphanLeak}); detail == "" {
		t.Fatal("workloadDependencyAssertDropReasons(orphan leak) = \"\", want a non-empty failure detail")
	}
	if detail := workloadDependencyAssertDropReasons("odu:test", nil); detail == "" {
		t.Fatal("workloadDependencyAssertDropReasons(empty admitted set) = \"\", want a non-empty failure detail (cannot distinguish selective drops from dropping everything)")
	}
}

// TestMissingWorkloadDependencyExpectedTypesCatchesGap proves the coverage
// check is not vacuous: an expected set missing the registry's one type must
// be reported, and a complete one must report nothing.
func TestMissingWorkloadDependencyExpectedTypesCatchesGap(t *testing.T) {
	t.Parallel()

	registry := map[string]struct{}{"DEPENDS_ON": {}}
	complete := []ExpectedEdge{
		{RelationshipType: "DEPENDS_ON", SourceEntityID: "workload:a", TargetEntityID: "workload:b"},
	}
	if missing := missingWorkloadDependencyExpectedTypes(complete, registry); len(missing) != 0 {
		t.Fatalf("missingWorkloadDependencyExpectedTypes(complete) = %v, want none", missing)
	}

	missing := missingWorkloadDependencyExpectedTypes(nil, registry)
	if len(missing) != 1 || missing[0] != "DEPENDS_ON" {
		t.Fatalf("missingWorkloadDependencyExpectedTypes(empty) = %v, want [DEPENDS_ON]", missing)
	}
}

// TestCompareWorkloadDependencyExpectedEdgesCatchesExtraAndMissing proves the
// exact-set comparison is not vacuous in either direction.
func TestCompareWorkloadDependencyExpectedEdgesCatchesExtraAndMissing(t *testing.T) {
	t.Parallel()

	expected := []ExpectedEdge{
		{RelationshipType: "DEPENDS_ON", SourceEntityID: "workload:a", TargetEntityID: "workload:b"},
	}

	if detail := compareWorkloadDependencyExpectedEdges("odu:test", expected, expected); detail != "" {
		t.Fatalf("compareWorkloadDependencyExpectedEdges(exact match) = %q, want empty", detail)
	}

	extra := append(append([]ExpectedEdge{}, expected...), ExpectedEdge{
		RelationshipType: "DEPENDS_ON", SourceEntityID: "workload:a", TargetEntityID: "workload:c",
	})
	if detail := compareWorkloadDependencyExpectedEdges("odu:test", expected, extra); detail == "" {
		t.Fatal("compareWorkloadDependencyExpectedEdges(spurious extra edge) = \"\", want a non-empty failure detail")
	}

	if detail := compareWorkloadDependencyExpectedEdges("odu:test", expected, nil); detail == "" {
		t.Fatal("compareWorkloadDependencyExpectedEdges(missing edge) = \"\", want a non-empty failure detail")
	}
}

// writeWorkloadDependencyExpectedEdgesFixture writes a throwaway
// expected-edges JSON fixture matching the shape LoadExpectedEdges parses.
// Mirrors writeRepoDependencyExpectedEdgesFixture.
func writeWorkloadDependencyExpectedEdgesFixture(t *testing.T, path string, edges []map[string]string) {
	t.Helper()
	fixture := struct {
		Odu   string `json:"odu"`
		Edges []struct {
			RelationshipType string `json:"relationship_type"`
			SourceEntityID   string `json:"source_entity_id"`
			TargetEntityID   string `json:"target_entity_id"`
		} `json:"edges"`
	}{
		Odu: workloadDependencyFamilyOduName,
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
