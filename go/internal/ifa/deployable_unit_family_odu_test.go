// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestDeployableUnitFamilyCassetteDerivesTheExpectedEdgeSet is the offline
// vacuity guard for #5993: the production evidence/resolution/extraction
// seams, over the committed cassette, reproduce the hand-derived expected set
// EXACTLY.
//
// This is deliberately NOT called coverage. It proves the extractor, not the
// gate: the live edge write is a MATCH-MATCH-MERGE on both endpoint ids, so a
// missing endpoint Repository node makes the write a silent no-op this test
// cannot see. The live ifa-determinism assertion closes that half.
func TestDeployableUnitFamilyCassetteDerivesTheExpectedEdgeSet(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	odu, err := loadDeployableUnitFamilyOdu(deployableUnitFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadDeployableUnitFamilyOdu: %v", err)
	}

	ok, detail := resolveDeployableUnitMaterializedEdges(odu, deployableUnitFamilyExpectedEdgesPath(repoRoot))
	if !ok {
		t.Fatalf("resolveDeployableUnitMaterializedEdges(cassette odù) = (false, %q), want (true, ...)", detail)
	}
	t.Log(detail)
}

// TestDeployableUnitFamilyCoversAllRegistryTypes stops the fixture degrading
// into a vacuous proof. The family owns one edge type today
// (CORRELATES_DEPLOYABLE_UNIT); an expected set that quietly dropped it would
// still parse as valid JSON with zero edges asserted for the family.
func TestDeployableUnitFamilyCoversAllRegistryTypes(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	expectedEdges, err := LoadExpectedEdges(deployableUnitFamilyExpectedEdgesPath(repoRoot), "deployable_unit_edges")
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}

	present := map[string]struct{}{}
	for _, e := range expectedEdges {
		present[e.RelationshipType] = struct{}{}
	}
	registered, err := MaterializedEdgeDomainEdgeTypes("deployable_unit_edges")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(deployable_unit_edges): %v", err)
	}
	var uncovered []string
	for edgeType := range registered {
		if _, ok := present[edgeType]; !ok {
			uncovered = append(uncovered, edgeType)
		}
	}
	sort.Strings(uncovered)
	if len(uncovered) > 0 {
		t.Errorf("the expected-edge set exercises no %v edge; the family registers them, so the fixture proves exhaustiveness over less than the family owns", uncovered)
	}
}

// TestDeployableUnitFamilyCassetteMatchesCompiledCatalog pins the compiled,
// binary-portable Odù (deployableUnitFamilyOdu, what catalog_seed.go will
// register) to the committed cassette's strict projection, so a one-sided
// edit to either fails this focused suite. It does not (yet) assert
// CatalogByName()/the Resolve dispatch arm: those wire through
// catalog_seed.go and materialized_edges.go, which this issue's coordinating
// agent owns; TestResolveDeployableUnitMaterializedEdgesReproducesExpectedSet
// and this test together prove the guard resolves both the compiled and the
// cassette-loaded Odù correctly ahead of that one-line wiring.
func TestDeployableUnitFamilyCassetteMatchesCompiledCatalog(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	compiled := deployableUnitFamilyOdu().Odu
	fromCassette, err := loadDeployableUnitFamilyOdu(deployableUnitFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadDeployableUnitFamilyOdu: %v", err)
	}
	if !reflect.DeepEqual(compiled, fromCassette) {
		t.Fatalf("compiled catalog Odù drifted from strict cassette projection\ncompiled: %#v\ncassette: %#v", compiled, fromCassette)
	}
}

// TestDeployableUnitFamilyRepositoryIdentityDoesNotCollideWithSiblings pins
// the repository and generation identity boundaries the live determinism
// matrix depends on. A shared local_path lets canonical path cleanup delete
// a sibling family's repository; a shared generation ID violates the durable
// active-generation uniqueness constraint when both scopes ACK.
func TestDeployableUnitFamilyRepositoryIdentityDoesNotCollideWithSiblings(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu, err := loadDeployableUnitFamilyOdu(deployableUnitFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadDeployableUnitFamilyOdu: %v", err)
	}

	var localPaths []string
	for _, fact := range odu.Facts {
		if fact.FactKind != "repository" {
			continue
		}
		localPath := strings.TrimSpace(anyToStringValue(fact.Payload["local_path"]))
		if localPath == "" {
			t.Fatalf("repository fact %q has no local_path; canonical file and entity paths would be unanchored", fact.StableFactKey)
		}
		localPaths = append(localPaths, localPath)
	}
	if len(localPaths) != 4 {
		t.Fatalf("expected 4 repository facts (app, deploy, rejected, jenkins), got %d", len(localPaths))
	}
	seen := make(map[string]struct{}, len(localPaths))
	for _, localPath := range localPaths {
		if _, dup := seen[localPath]; dup {
			t.Fatalf("two deployable-unit repositories share local_path %q", localPath)
		}
		seen[localPath] = struct{}{}
	}
	for _, sibling := range []string{sqlFamilyLocalPath, "/repo-code-calls"} {
		for _, localPath := range localPaths {
			if localPath == sibling {
				t.Fatalf("deployable-unit local_path %q collides with a sibling family's repository path; canonical path cleanup can delete the other live-matrix repository", localPath)
			}
		}
	}

	if len(odu.Facts) == 0 || strings.TrimSpace(odu.Facts[0].GenerationID) == "" {
		t.Fatal("deployable-unit Odù has no generation ID; active-generation publication would be unidentifiable")
	}
	for _, sibling := range []string{sqlFamilyGenerationID, codeCallFamilyGenerationID} {
		if odu.Facts[0].GenerationID == sibling {
			t.Fatalf("deployable-unit generation ID %q collides with a sibling family's generation; only one live-matrix scope can publish it as active", odu.Facts[0].GenerationID)
		}
	}
}

// TestDeployableUnitFamilyOduPreservesEnvelopeFields stops the loader being
// more permissive than production. schema_version is load-bearing: an empty
// version reads as "latest", so a cassette carrying an unsupported major
// would satisfy this guard while live replay preserved the version and
// quarantined the fact -- the offline proof would certify input the live gate
// rejects, which is the one failure this fixture exists to make impossible.
func TestDeployableUnitFamilyOduPreservesEnvelopeFields(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	odu, err := loadDeployableUnitFamilyOdu(deployableUnitFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadDeployableUnitFamilyOdu: %v", err)
	}

	// Read the cassette independently so the comparison is against the file,
	// not against the loader's own view of it.
	raw, err := os.ReadFile(deployableUnitFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("read cassette: %v", err)
	}
	var onDisk struct {
		Scopes []struct {
			Facts []struct {
				FactKind      string `json:"fact_kind"`
				SchemaVersion string `json:"schema_version"`
				StableFactKey string `json:"stable_fact_key"`
				CollectorKind string `json:"collector_kind"`
			} `json:"facts"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("parse cassette: %v", err)
	}
	if len(onDisk.Scopes) != 1 || len(onDisk.Scopes[0].Facts) != len(odu.Facts) {
		t.Fatalf("fact count mismatch: cassette has %d, Odù has %d", len(onDisk.Scopes[0].Facts), len(odu.Facts))
	}

	checked := 0
	for i, want := range onDisk.Scopes[0].Facts {
		got := odu.Facts[i]
		if want.SchemaVersion == "" {
			t.Fatalf("fact %d (%s) declares no schema_version; this guard would be vacuous", i, want.FactKind)
		}
		if got.SchemaVersion != want.SchemaVersion {
			t.Errorf("fact %d (%s): schema_version %q dropped or altered (cassette says %q); an empty version reads as latest and hides an unsupported major",
				i, want.FactKind, got.SchemaVersion, want.SchemaVersion)
		}
		if got.StableFactKey != want.StableFactKey {
			t.Errorf("fact %d (%s): stable_fact_key %q != cassette %q", i, want.FactKind, got.StableFactKey, want.StableFactKey)
		}
		if got.CollectorKind != want.CollectorKind {
			t.Errorf("fact %d (%s): collector_kind %q != cassette %q", i, want.FactKind, got.CollectorKind, want.CollectorKind)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no facts compared; this guard would pass vacuously")
	}
	t.Logf("verified envelope fields preserved across %d facts", checked)
}
