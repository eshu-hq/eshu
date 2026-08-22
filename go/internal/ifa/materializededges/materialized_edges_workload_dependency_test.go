// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

func TestWorkloadDependencyFamilyGraphLookupMatchesProductionAnchoring(t *testing.T) {
	t.Parallel()

	lookup := workloadDependencyFamilyGraphLookup{
		repoEdges: []reducer.RepoDependencyEdge{
			{SourceRepoID: ifa.WorkloadDependencyFamilySourceRepoID, TargetRepoID: ifa.WorkloadDependencyFamilyTargetRepoID},
			{SourceRepoID: ifa.WorkloadDependencyFamilyMultiSourceRepoID, TargetRepoID: ifa.WorkloadDependencyFamilyMultiTargetRepoID},
			{SourceRepoID: ifa.WorkloadDependencyFamilyOrphanSourceRepoID, TargetRepoID: ifa.WorkloadDependencyFamilyOrphanTargetRepoID},
		},
		persistedWorkloads: []reducer.RepoWorkload{
			{RepoID: ifa.WorkloadDependencyFamilyOrphanSourceRepoID, WorkloadID: ifa.WorkloadDependencyFamilyOrphanSourcePersistedWorkloadID},
		},
	}

	currentRepoIDs := []string{
		ifa.WorkloadDependencyFamilySourceRepoID,
		ifa.WorkloadDependencyFamilyTargetRepoID,
		ifa.WorkloadDependencyFamilyMultiSourceRepoID,
		ifa.WorkloadDependencyFamilyMultiTargetRepoID,
	}
	edges, err := lookup.ListRepoDependencyEdges(context.Background(), currentRepoIDs)
	if err != nil {
		t.Fatalf("ListRepoDependencyEdges: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("ListRepoDependencyEdges(current repos) returned %d edges, want 2 anchored edges; the neither-current orphan pair is unreachable through production's query", len(edges))
	}
	for _, edge := range edges {
		if edge.SourceRepoID == ifa.WorkloadDependencyFamilyOrphanSourceRepoID || edge.TargetRepoID == ifa.WorkloadDependencyFamilyOrphanTargetRepoID {
			t.Fatalf("ListRepoDependencyEdges(current repos) returned unanchored orphan edge %#v", edge)
		}
	}

	workloads, err := lookup.ListRepoWorkloads(context.Background(), currentRepoIDs)
	if err != nil {
		t.Fatalf("ListRepoWorkloads: %v", err)
	}
	if len(workloads) != 0 {
		t.Fatalf("ListRepoWorkloads(current repos) = %#v, want no injected workload rows", workloads)
	}
}

// TestResolveWorkloadDependencyMaterializedEdgesReproducesExpectedSet pins
// the workload_dependency vacuity guard against the family's hand-derived
// expected-edge-set fixture. It proves the guard runs the real two-seam
// chain -- DiscoveredEvidence -> relationships.Resolve ->
// reducer.ExtractRepoDependencyIntentRows for the repo edge, and
// reducer.ExtractWorkloadCandidates -> BuildProjectionRowsWithInfrastructurePlatforms
// for the workload map -- over the cataloged Odù's own facts, feeding the
// real reducer.ReconcileWorkloadDependencyEdges, and reproduces exactly the
// two edges the catalog's production workload candidates imply.
func TestResolveWorkloadDependencyMaterializedEdgesReproducesExpectedSet(t *testing.T) {
	t.Parallel()

	odu := ifa.WorkloadDependencyFamilyOdu().Odu
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
	resolver := MaterializedEdgeOduResolver{Catalog: ifa.CatalogByName(), RepoRoot: repoRoot}

	ok, detail := resolver.Resolve(replaycoverage.CoverageEntry{
		Surface:      MaterializedEdgeSurfacePrefix + workloadDependencyEdgesFamily,
		Scenario:     replaycoverage.ScenarioOdu,
		ScenarioType: replaycoverage.ScenarioTypeBaseline,
		Ref:          ifa.WorkloadDependencyFamilyOduName,
	})
	if !ok {
		t.Fatalf("resolver.Resolve for %s: %s", workloadDependencyEdgesFamily, detail)
	}
	t.Logf("%s", detail)
}

func TestWorkloadDependencyFamilyCassetteDerivesTheExpectedEdgeSet(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu, err := ifa.LoadWorkloadDependencyFamilyOdu(ifa.WorkloadDependencyFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("ifa.LoadWorkloadDependencyFamilyOdu: %v", err)
	}
	if ok, detail := resolveWorkloadDependencyMaterializedEdges(odu, workloadDependencyFamilyExpectedEdgesPath(repoRoot)); !ok {
		t.Fatalf("resolveWorkloadDependencyMaterializedEdges(cassette odù) = (false, %q), want true", detail)
	}
}

func TestWorkloadDependencyFamilyCassetteMatchesCompiledCatalog(t *testing.T) {
	t.Parallel()
	compiled := ifa.WorkloadDependencyFamilyOdu().Odu
	fromCassette, err := ifa.LoadWorkloadDependencyFamilyOdu(ifa.WorkloadDependencyFamilyCassetteFullPath(repoRootDir(t)))
	if err != nil {
		t.Fatalf("ifa.LoadWorkloadDependencyFamilyOdu: %v", err)
	}
	if !reflect.DeepEqual(compiled, fromCassette) {
		t.Fatalf("compiled catalog Odù drifted from cassette projection\ncompiled: %#v\ncassette: %#v", compiled, fromCassette)
	}
}

// TestResolveWorkloadDependencyMaterializedEdgesRejectsWrongExpectedSet
// proves the guard is not vacuously true: an expected-edge fixture naming
// the wrong target workload must fail closed, not silently pass.
func TestResolveWorkloadDependencyMaterializedEdgesRejectsWrongExpectedSet(t *testing.T) {
	t.Parallel()

	odu := ifa.WorkloadDependencyFamilyOdu().Odu
	wrongPath := filepath.Join(t.TempDir(), "wrong-expected-edges.json")
	writeWorkloadDependencyExpectedEdgesFixture(t, wrongPath, []map[string]string{
		{"relationship_type": "DEPENDS_ON", "source_entity_id": "workload:workload-dependency-source", "target_entity_id": "workload:not-the-real-target"},
	})

	ok, detail := resolveWorkloadDependencyMaterializedEdges(odu, wrongPath)
	if ok {
		t.Fatalf("resolveWorkloadDependencyMaterializedEdges() = (true, %q), want (false, ...) for a deliberately wrong fixture", detail)
	}
}

// TestResolveWorkloadDependencyMaterializedEdgesAdmitsBothProductionPairs
// proves the guard matches both pairs the committed catalog produces.
func TestResolveWorkloadDependencyMaterializedEdgesAdmitsBothProductionPairs(t *testing.T) {
	t.Parallel()

	odu := ifa.WorkloadDependencyFamilyOdu().Odu
	path := filepath.Join(t.TempDir(), "two-production-pairs-expected-edges.json")
	writeWorkloadDependencyExpectedEdgesFixture(t, path, []map[string]string{
		{"relationship_type": "DEPENDS_ON", "source_entity_id": "workload:workload-dependency-source", "target_entity_id": "workload:workload-dependency-target"},
		{"relationship_type": "DEPENDS_ON", "source_entity_id": "workload:workload-dependency-multi-source", "target_entity_id": "workload:workload-dependency-multi-target"},
	})

	ok, detail := resolveWorkloadDependencyMaterializedEdges(odu, path)
	if !ok {
		t.Fatalf("resolveWorkloadDependencyMaterializedEdges() = (false, %q), want true for both production-derived pairs", detail)
	}
}

// TestResolveWorkloadDependencyMaterializedEdgesRejectsOrphanPairInFixture
// proves an expected fixture cannot claim the neither-current orphan pair;
// production's anchored graph lookup cannot return that pair to reconciliation.
func TestResolveWorkloadDependencyMaterializedEdgesRejectsOrphanPairInFixture(t *testing.T) {
	t.Parallel()

	odu := ifa.WorkloadDependencyFamilyOdu().Odu
	path := filepath.Join(t.TempDir(), "orphan-expected-edges.json")
	writeWorkloadDependencyExpectedEdgesFixture(t, path, []map[string]string{
		{"relationship_type": "DEPENDS_ON", "source_entity_id": "workload:workload-dependency-source", "target_entity_id": "workload:workload-dependency-target"},
		{"relationship_type": "DEPENDS_ON", "source_entity_id": "workload:workload-dependency-multi-source", "target_entity_id": "workload:workload-dependency-multi-target"},
		{"relationship_type": "DEPENDS_ON", "source_entity_id": "workload:workload-dependency-orphan-source-persisted", "target_entity_id": "workload:workload-dependency-orphan-target-persisted"},
	})

	ok, detail := resolveWorkloadDependencyMaterializedEdges(odu, path)
	if ok {
		t.Fatalf("resolveWorkloadDependencyMaterializedEdges() = (true, %q), want (false, ...) for a fixture claiming the orphan pair admits", detail)
	}
}

// TestWorkloadDependencyAssertSelectiveAdmissionCatchesMissingAndExtra proves
// workloadDependencyAssertSelectiveAdmission is not vacuous: it must require
// both production pairs, reject the orphan pair leaking past lookup anchoring,
// and reject an empty admitted set.
func TestWorkloadDependencyAssertSelectiveAdmissionCatchesMissingAndExtra(t *testing.T) {
	t.Parallel()

	positive := reducer.WorkloadDependencyEdgeRow{
		RepoID: ifa.WorkloadDependencyFamilySourceRepoID, WorkloadID: "workload:workload-dependency-source",
		TargetRepoID: ifa.WorkloadDependencyFamilyTargetRepoID, TargetWorkloadID: "workload:workload-dependency-target",
	}
	multiLeak := reducer.WorkloadDependencyEdgeRow{
		RepoID: ifa.WorkloadDependencyFamilyMultiSourceRepoID, WorkloadID: "workload:workload-dependency-multi-source",
		TargetRepoID: ifa.WorkloadDependencyFamilyMultiTargetRepoID, TargetWorkloadID: "workload:workload-dependency-multi-target",
	}
	orphanLeak := reducer.WorkloadDependencyEdgeRow{
		RepoID: ifa.WorkloadDependencyFamilyOrphanSourceRepoID, WorkloadID: ifa.WorkloadDependencyFamilyOrphanSourcePersistedWorkloadID,
		TargetRepoID: ifa.WorkloadDependencyFamilyOrphanTargetRepoID, TargetWorkloadID: ifa.WorkloadDependencyFamilyOrphanTargetPersistedWorkloadID,
	}

	if detail := workloadDependencyAssertSelectiveAdmission("odu:test", []reducer.WorkloadDependencyEdgeRow{positive}); detail == "" {
		t.Fatal("workloadDependencyAssertSelectiveAdmission(positive only) = empty, want missing second-pair failure")
	}
	if detail := workloadDependencyAssertSelectiveAdmission("odu:test", []reducer.WorkloadDependencyEdgeRow{positive, multiLeak}); detail != "" {
		t.Fatalf("workloadDependencyAssertSelectiveAdmission(two production pairs) = %q, want empty", detail)
	}
	if detail := workloadDependencyAssertSelectiveAdmission("odu:test", []reducer.WorkloadDependencyEdgeRow{positive, multiLeak, orphanLeak}); detail == "" {
		t.Fatal("workloadDependencyAssertSelectiveAdmission(orphan leak) = \"\", want a non-empty failure detail")
	}
	if detail := workloadDependencyAssertSelectiveAdmission("odu:test", nil); detail == "" {
		t.Fatal("workloadDependencyAssertSelectiveAdmission(empty admitted set) = \"\", want a non-empty failure detail (cannot distinguish selective drops from dropping everything)")
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
		Odu: ifa.WorkloadDependencyFamilyOduName,
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
