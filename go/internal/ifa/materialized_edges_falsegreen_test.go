// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// TestMaterializedEdgeFalseGreenBaselineSQLRelationshipsCovered proves the
// honest-green case FIRST (apirecording discipline, mirrored from
// coverage_falsegreen_test.go): the real manifest's materialized_edges:
// sql_relationships must resolve covered for the determinism (baseline) gate
// before either deliberate break below is trusted to mean anything. Only the
// baseline is a covered claim; SQL delta is checked separately below and the
// fault gate is waived (#5555), not resolved.
func TestMaterializedEdgeFalseGreenBaselineSQLRelationshipsCovered(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	resolver := MaterializedEdgeOduResolver{Catalog: CatalogByName(), RepoRoot: repoRoot}
	ok, detail := resolver.Resolve(replaycoverage.CoverageEntry{
		Surface: MaterializedEdgeSurfacePrefix + "sql_relationships", Scenario: replaycoverage.ScenarioOdu,
		Ref: sqlFamilyOduName, ProofGate: materializedEdgeProofGateBaseline,
	})
	if !ok {
		t.Fatalf("baseline resolve(sql_relationships, proof_gate=%s) = false, detail=%q, want true", materializedEdgeProofGateBaseline, detail)
	}
}

func TestMaterializedEdgeFalseGreenDeltaSQLRelationshipsCovered(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	resolver := MaterializedEdgeOduResolver{Catalog: CatalogByName(), RepoRoot: repoRoot}
	ok, detail := resolver.Resolve(replaycoverage.CoverageEntry{
		Surface: MaterializedEdgeSurfacePrefix + "sql_relationships", Scenario: replaycoverage.ScenarioOdu,
		ScenarioType: replaycoverage.ScenarioTypeDeltaTombstone,
		Ref:          sqlFamilyDeltaOduName,
		ProofGate:    materializedEdgeProofGateBaseline,
	})
	if !ok {
		t.Fatalf("delta resolve(sql_relationships, proof_gate=%s) = false, detail=%q, want true", materializedEdgeProofGateBaseline, detail)
	}
}

// TestMaterializedEdgeFalseGreenWrongOduBreaksSQLRelationships is the
// deliberate false-green break: binding materialized_edges:sql_relationships
// to odu:aws-pack (a cataloged Odù that carries no SQL content_entity facts
// at all) must not resolve covered. A resolver that stayed green here could
// not tell a real SQL-family binding apart from an unrelated cataloged Odù.
func TestMaterializedEdgeFalseGreenWrongOduBreaksSQLRelationships(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	resolver := MaterializedEdgeOduResolver{Catalog: CatalogByName(), RepoRoot: repoRoot}
	ok, detail := resolver.Resolve(replaycoverage.CoverageEntry{
		Surface: MaterializedEdgeSurfacePrefix + "sql_relationships", Scenario: replaycoverage.ScenarioOdu,
		Ref: "odu:aws-pack", ProofGate: "ifa-determinism",
	})
	if ok {
		t.Fatal("materialized_edges:sql_relationships bound to odu:aws-pack must not resolve covered (false green): odu:aws-pack carries no SQL content_entity facts")
	}
	if detail == "" {
		t.Error("expected a non-empty detail explaining the false-green break")
	}
}

// TestMaterializedEdgeFalseGreenMissingWaiverFailsNamingFamily proves an
// uncovered (surface × proof_gate) row with NO waiver is a required, blocking
// failure — removing the code_calls BASELINE waiver (leaving its fault waiver
// in place) and re-running the gate must fail on the baseline row, and the
// failing finding must be traceable back to that exact family. Leaving the
// fault waiver untouched also proves per-(surface, proof_gate) isolation: the
// surviving fault waiver does not rescue the now-unwaived baseline row.
func TestMaterializedEdgeFalseGreenMissingWaiverFailsNamingFamily(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	specsDir := filepath.Join(repoRoot, "specs")

	manifest, err := replaycoverage.LoadManifest(filepath.Join(specsDir, MaterializedEdgeManifestFileName))
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	waivers, err := LoadMaterializedEdgeWaivers(filepath.Join(specsDir, MaterializedEdgeManifestFileName))
	if err != nil {
		t.Fatalf("LoadMaterializedEdgeWaivers: %v", err)
	}

	var trimmed []MaterializedEdgeWaiver
	removed := false
	for _, w := range waivers {
		if w.Surface == MaterializedEdgeSurfacePrefix+"code_calls" && w.ProofGate == materializedEdgeProofGateBaseline {
			removed = true
			continue
		}
		trimmed = append(trimmed, w)
	}
	if !removed {
		t.Fatal("test setup: materialized_edges:code_calls baseline waiver not found in the real manifest (fixture drifted)")
	}

	_, gate, _ := RunMaterializedEdgeCoverage(MaterializedEdgeCoverageInputs{
		Families: reducer.MaterializedEdgeFamilies(),
		Manifest: manifest,
		Waivers:  trimmed,
		Catalog:  CatalogByName(),
		RepoRoot: repoRoot,
		Blocking: true,
	})
	if !gate.Failed() {
		t.Fatal("removing the code_calls waiver must fail the blocking gate (uncovered family with no coverage and no waiver)")
	}

	foundCodeCalls := false
	for _, f := range gate.Findings {
		if f.Check == MaterializedEdgeSurfacePrefix+"code_calls" && !f.OK && f.Required {
			foundCodeCalls = true
		}
	}
	if !foundCodeCalls {
		t.Errorf("gate findings do not name materialized_edges:code_calls as a required failure: %+v", gate.Findings)
	}
}

// TestMaterializedEdgeFalseGreenSQLDeltaCannotUseBaselineWaiver proves the
// required SQL delta-live row stays unwaivable even though it shares the
// ifa-determinism proof gate with the baseline row. A future baseline waiver
// must neither soften an unresolved delta nor become stale merely because the
// distinct delta row is covered.
func TestMaterializedEdgeFalseGreenSQLDeltaCannotUseBaselineWaiver(t *testing.T) {
	t.Parallel()

	surface := MaterializedEdgeSurfacePrefix + "sql_relationships"
	waivers := map[materializedEdgeWaiverKey]MaterializedEdgeWaiver{
		{Surface: surface, ProofGate: materializedEdgeProofGateBaseline}: {
			Surface:   surface,
			ProofGate: materializedEdgeProofGateBaseline,
			Issue:     "#test-baseline-waiver",
			Waived:    "2026-07-21",
			Reason:    "test-only baseline gap",
		},
	}
	delta := replaycoverage.SurfaceCoverage{
		Surface: replaycoverage.SupportedSurface{
			Registry: RegistryMaterializedEdges,
			Key:      surface,
		},
		ScenarioType: replaycoverage.ScenarioTypeDeltaTombstone,
		Status:       replaycoverage.StatusUncovered,
		Detail:       "test-only missing delta proof",
	}

	finding := materializedEdgeFinding(delta, waivers, true)
	if finding.OK || !finding.Required {
		t.Fatalf("unresolved SQL delta with baseline waiver = %+v, want required failure", finding)
	}
	if got := materializedEdgeWaiverProofGateFor(replaycoverage.ScenarioTypeDeltaTombstone); got != "" {
		t.Fatalf("delta waiver lookup gate = %q, want empty so covered delta cannot stale or consume a baseline waiver", got)
	}
}

// TestMaterializedEdgeFalseGreenIncompleteExpectedSetFailsMissingType proves
// the exhaustiveness half of the vacuity guard: an expected-edge-set file
// that drops one of the seven registry types must fail resolution, naming
// the missing type. This is the regression an 8th writer type (or a removed
// one) added later without a matching fixture update would trip.
func TestMaterializedEdgeFalseGreenIncompleteExpectedSetFailsMissingType(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	full, err := loadSQLRelationshipExpectedEdges(sqlFamilyExpectedEdgesPath(repoRoot))
	if err != nil {
		t.Fatalf("loadSQLRelationshipExpectedEdges: %v", err)
	}

	// Drop the MIGRATES edge, leaving six of the seven registry types.
	var incomplete []sqlRelationshipExpectedEdge
	for _, e := range full {
		if e.RelationshipType == "MIGRATES" {
			continue
		}
		incomplete = append(incomplete, e)
	}
	if len(incomplete) != len(full)-1 {
		t.Fatalf("test setup: expected to drop exactly one MIGRATES edge, dropped %d", len(full)-len(incomplete))
	}

	path := writeTempExpectedEdges(t, incomplete)
	odu := CatalogByName()[sqlFamilyOduName]
	ok, detail := resolveSQLRelationshipMaterializedEdges(odu, path)
	if ok {
		t.Fatal("resolveSQLRelationshipMaterializedEdges must not resolve covered when the expected-edge-set is missing a registry type (MIGRATES)")
	}
	if detail == "" {
		t.Error("expected a non-empty detail naming the missing type")
	}
}

// TestMaterializedEdgeFalseGreenExtraExpectedEdgeNotProducedFails proves the
// other exactness direction: an expected-edge-set that names an edge the
// fixture's facts do NOT actually derive must fail, not silently pass because
// the real edges happen to be a subset.
func TestMaterializedEdgeFalseGreenExtraExpectedEdgeNotProducedFails(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	full, err := loadSQLRelationshipExpectedEdges(sqlFamilyExpectedEdgesPath(repoRoot))
	if err != nil {
		t.Fatalf("loadSQLRelationshipExpectedEdges: %v", err)
	}
	withPhantom := append(append([]sqlRelationshipExpectedEdge(nil), full...), sqlRelationshipExpectedEdge{
		RelationshipType: "MIGRATES",
		SourceEntityID:   "content-entity:sql-mig-does-not-exist",
		TargetEntityID:   "content-entity:sql-tbl-users",
	})

	path := writeTempExpectedEdges(t, withPhantom)
	odu := CatalogByName()[sqlFamilyOduName]
	ok, detail := resolveSQLRelationshipMaterializedEdges(odu, path)
	if ok {
		t.Fatal("resolveSQLRelationshipMaterializedEdges must not resolve covered when the expected-edge-set names an edge the fixture's facts do not derive")
	}
	if detail == "" {
		t.Error("expected a non-empty detail naming the count mismatch")
	}
}

// TestMaterializedEdgeFalseGreenReferencesTableNeverExpected proves
// REFERENCES_TABLE (retract-superset-only since #5345) can never satisfy the
// exhaustiveness check: it is not in the writer registry, so an
// expected-edge-set containing ONLY REFERENCES_TABLE edges (and none of the
// seven real registry types) must fail as missing every real type.
func TestMaterializedEdgeFalseGreenReferencesTableNeverExpected(t *testing.T) {
	t.Parallel()

	path := writeTempExpectedEdges(t, []sqlRelationshipExpectedEdge{
		{RelationshipType: "REFERENCES_TABLE", SourceEntityID: "content-entity:x", TargetEntityID: "content-entity:y"},
	})
	odu := CatalogByName()[sqlFamilyOduName]
	ok, detail := resolveSQLRelationshipMaterializedEdges(odu, path)
	if ok {
		t.Fatal("an expected-edge-set naming only REFERENCES_TABLE must not resolve covered")
	}
	if detail == "" {
		t.Error("expected a non-empty detail naming the missing registry types")
	}
}

func writeTempExpectedEdges(t *testing.T, edges []sqlRelationshipExpectedEdge) string {
	t.Helper()
	payload := sqlRelationshipExpectedEdgesFile{Odu: sqlFamilyOduName, Edges: edges}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal temp expected edges: %v", err)
	}
	path := filepath.Join(t.TempDir(), "expected-edges.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write temp expected edges: %v", err)
	}
	return path
}
