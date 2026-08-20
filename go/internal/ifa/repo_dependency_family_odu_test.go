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

// TestRepoDependencyFamilyCassetteDerivesTheExpectedEdgeSet is the offline
// vacuity guard for #5999: the production evidence/resolution/extraction
// seams, over the committed cassette, reproduce the hand-derived expected set
// EXACTLY.
//
// This is deliberately NOT called coverage. It proves the extractor, not the
// gate: the live edge write is a MATCH-MATCH-MERGE on both endpoint ids, so a
// missing endpoint Repository node makes the write a silent no-op this test
// cannot see. The live ifa-determinism assertion closes that half by replaying
// the same cassette and querying the materialized graph.
func TestRepoDependencyFamilyCassetteDerivesTheExpectedEdgeSet(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	odu, err := loadRepoDependencyFamilyOdu(repoDependencyFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadRepoDependencyFamilyOdu: %v", err)
	}

	ok, detail := resolveRepoDependencyMaterializedEdges(odu, repoDependencyFamilyExpectedEdgesPath(repoRoot))
	if !ok {
		t.Fatalf("resolveRepoDependencyMaterializedEdges(cassette odù) = (false, %q), want (true, ...)", detail)
	}
	t.Log(detail)
}

// TestRepoDependencyFamilyCoversEveryRegisteredType stops the fixture
// degrading into a vacuous proof. Every registered repo_dependency type must
// appear in the expected-edge set; an expected set that quietly dropped one
// would still parse as valid JSON with fewer edges asserted for the family.
func TestRepoDependencyFamilyCoversEveryRegisteredType(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	expectedEdges, err := LoadExpectedEdges(repoDependencyFamilyExpectedEdgesPath(repoRoot), repoDependencyEdgesFamily)
	if err != nil {
		t.Fatalf("LoadExpectedEdges: %v", err)
	}

	present := map[string]struct{}{}
	for _, e := range expectedEdges {
		present[e.RelationshipType] = struct{}{}
	}
	registered, err := MaterializedEdgeDomainEdgeTypes(repoDependencyEdgesFamily)
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(%s): %v", repoDependencyEdgesFamily, err)
	}
	var uncovered []string
	for edgeType := range registered {
		if _, ok := present[edgeType]; !ok {
			uncovered = append(uncovered, edgeType)
		}
	}
	sort.Strings(uncovered)
	if len(uncovered) > 0 {
		t.Errorf("the expected-edge set exercises no %v edge; the family registers them, so the fixture proves exhaustiveness over less than this guard claims", uncovered)
	}
}

// TestRepoDependencyFamilyCassetteMatchesCompiledCatalog pins the compiled,
// binary-portable Odù (repoDependencyFamilyOdu, registered in catalog_seed.go)
// to the committed cassette's strict projection, so a one-sided edit to
// either fails this focused suite.
//
// WHAT THIS DOES NOT PROVE (#5993 postmortem, deployable_unit_family_odu_test.go).
// reflect.DeepEqual only detects DRIFT between the two sides -- it can never
// catch an error baked into shared source both sides derive the same way
// from. Catching that class needs an assertion against an INDEPENDENT source
// of truth, which for this family is the evidence-kind-specific parser and
// typed workload-projection coverage already committed under
// go/internal/relationships and go/internal/reducer, not a tighter equality
// check between the two things that would share the same defect.
func TestRepoDependencyFamilyCassetteMatchesCompiledCatalog(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	compiled := repoDependencyFamilyOdu().Odu
	fromCassette, err := loadRepoDependencyFamilyOdu(repoDependencyFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadRepoDependencyFamilyOdu: %v", err)
	}
	if !reflect.DeepEqual(compiled, fromCassette) {
		t.Fatalf("compiled catalog Odù drifted from strict cassette projection\ncompiled: %#v\ncassette: %#v", compiled, fromCassette)
	}
}

// TestRepoDependencyFamilyRepositoryIdentityDoesNotCollideWithSiblings pins
// the repository and generation identity boundaries the live determinism
// matrix would depend on. A shared repo_id lets one family's Repository node
// silently absorb another family's edges (both MERGE on the same
// Repository{id}); a shared generation ID violates the durable
// active-generation uniqueness constraint when both scopes ACK.
func TestRepoDependencyFamilyRepositoryIdentityDoesNotCollideWithSiblings(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu, err := loadRepoDependencyFamilyOdu(repoDependencyFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadRepoDependencyFamilyOdu: %v", err)
	}

	var repoIDs []string
	for _, fact := range odu.Facts {
		if fact.FactKind != repositoryFactKind {
			continue
		}
		repoID := strings.TrimSpace(anyToStringValue(fact.Payload["repo_id"]))
		if repoID == "" {
			t.Fatalf("repository fact %q has no repo_id", fact.StableFactKey)
		}
		repoIDs = append(repoIDs, repoID)
	}
	if len(repoIDs) != 7 {
		t.Fatalf("expected 7 repository facts (1 source + 6 targets), got %d", len(repoIDs))
	}
	seen := make(map[string]struct{}, len(repoIDs))
	for _, repoID := range repoIDs {
		if _, dup := seen[repoID]; dup {
			t.Fatalf("two repo_dependency-family repositories share repo_id %q", repoID)
		}
		seen[repoID] = struct{}{}
	}

	// Known sibling repo_ids/generation_ids from other cataloged families,
	// asserted by literal so a collision fails loudly rather than silently
	// merging two families' Repository nodes into one graph identity.
	siblingRepoIDs := []string{
		deployableUnitFamilyAppRepoID,
		deployableUnitFamilyDeployRepoID,
		deployableUnitFamilyRejectedRepoID,
		deployableUnitFamilyAdmittedNoDeployRepoID,
	}
	for _, sibling := range siblingRepoIDs {
		if _, collide := seen[sibling]; collide {
			t.Fatalf("repo_dependency-family repo_id collides with sibling family repo_id %q", sibling)
		}
	}

	if len(odu.Facts) == 0 || strings.TrimSpace(odu.Facts[0].GenerationID) == "" {
		t.Fatal("repo_dependency-family Odù has no generation ID; active-generation publication would be unidentifiable")
	}
	for _, sibling := range []string{sqlFamilyGenerationID, codeCallFamilyGenerationID, deployableUnitFamilyGenerationID} {
		if odu.Facts[0].GenerationID == sibling {
			t.Fatalf("repo_dependency-family generation ID %q collides with a sibling family's generation; only one live-matrix scope can publish it as active", odu.Facts[0].GenerationID)
		}
	}
}

// TestRepoDependencyFamilyOduPreservesEnvelopeFields stops the loader being
// more permissive than production. schema_version is load-bearing: an empty
// version reads as "latest", so a cassette carrying an unsupported major
// would satisfy this guard while live replay preserved the version and
// quarantined the fact -- the offline proof would certify input a live gate
// would reject, which is the one failure this fixture exists to make
// impossible.
func TestRepoDependencyFamilyOduPreservesEnvelopeFields(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	odu, err := loadRepoDependencyFamilyOdu(repoDependencyFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadRepoDependencyFamilyOdu: %v", err)
	}

	// Read the cassette independently so the comparison is against the file,
	// not against the loader's own view of it.
	raw, err := os.ReadFile(repoDependencyFamilyCassetteFullPath(repoRoot))
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

// TestRepoDependencyFamilyOduInCatalogSeed proves repoDependencyFamilyOdu is
// actually registered in catalogSeed (catalog_seed.go), not merely
// constructible. A guard reachable through MaterializedEdgeOduResolver but
// pointed at an unregistered Odù name fails at Resolve time with "no
// cataloged Odù named ..."; this test catches the same gap earlier and more
// specifically.
func TestRepoDependencyFamilyOduInCatalogSeed(t *testing.T) {
	t.Parallel()
	catalog := CatalogByName()
	odu, ok := catalog[repoDependencyFamilyOduName]
	if !ok {
		t.Fatalf("CatalogByName() is missing %q; repoDependencyFamilyOdu() must be added to catalogSeed", repoDependencyFamilyOduName)
	}
	if len(odu.Facts) == 0 {
		t.Fatalf("cataloged %q carries no facts", repoDependencyFamilyOduName)
	}
}
