// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// TestRepoDependencyFamilyCassetteSatisfiesProductionContract loads the exact
// committed fixture through the production replay validator before the
// family-specific Odù projection sees it. This prevents the offline proof from
// accepting a reduced JSON shape that the drive command rejects.
func TestRepoDependencyFamilyCassetteSatisfiesProductionContract(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	path := RepoDependencyFamilyCassetteFullPath(repoRoot)

	file, err := cassette.LoadFile(path)
	if err != nil {
		t.Fatalf("cassette.LoadFile: %v", err)
	}
	if got, want := file.SchemaVersion, cassette.SchemaVersionV1; got != want {
		t.Errorf("schema_version = %q, want %q", got, want)
	}
	if got, want := file.Collector, "git"; got != want {
		t.Errorf("collector = %q, want %q", got, want)
	}
	if got, want := len(file.Scopes), 1; got != want {
		t.Fatalf("scope count = %d, want %d", got, want)
	}
	scope := file.Scopes[0]
	if got, want := scope.SourceSystem, "git"; got != want {
		t.Errorf("source_system = %q, want %q", got, want)
	}
	if got, want := scope.ScopeKind, "repo"; got != want {
		t.Errorf("scope_kind = %q, want %q", got, want)
	}
	if got, want := scope.CollectorKind, "git"; got != want {
		t.Errorf("collector_kind = %q, want %q", got, want)
	}
	if scope.ObservedAt.IsZero() {
		t.Error("observed_at is zero")
	}
	if got, want := len(scope.Facts), 16; got != want {
		t.Fatalf("production cassette fact count = %d, want %d", got, want)
	}

	odu, err := LoadRepoDependencyFamilyOdu(path)
	if err != nil {
		t.Fatalf("loadRepoDependencyFamilyOdu: %v", err)
	}
	if got, want := len(odu.Facts), 16; got != want {
		t.Fatalf("Odù fact count = %d, want %d", got, want)
	}
}

func TestRepoDependencyFamilyCassetteEmitsProductionGeneration(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	source, err := cassette.NewSource(RepoDependencyFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("cassette.NewSource: %v", err)
	}

	generation, ok, err := source.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("Source.Next() = (_, %v, %v), want generation", ok, err)
	}
	if got, want := generation.Scope.ScopeID, "scope-ifa-repo-dependency-family"; got != want {
		t.Errorf("scope id = %q, want %q", got, want)
	}
	if got, want := string(generation.Scope.ScopeKind), "repo"; got != want {
		t.Errorf("scope kind = %q, want %q", got, want)
	}
	if got, want := generation.Scope.SourceSystem, "git"; got != want {
		t.Errorf("source system = %q, want %q", got, want)
	}
	if got, want := string(generation.Scope.CollectorKind), "git"; got != want {
		t.Errorf("collector kind = %q, want %q", got, want)
	}
	if got, want := generation.Generation.GenerationID, "gen-ifa-repo-dependency-family-1"; got != want {
		t.Errorf("generation id = %q, want %q", got, want)
	}
	if generation.Generation.ObservedAt.IsZero() {
		t.Error("generation observed_at is zero")
	}

	count := 0
	for envelope := range generation.Facts {
		count++
		if envelope.ScopeID != generation.Scope.ScopeID || envelope.GenerationID != generation.Generation.GenerationID {
			t.Errorf("envelope %d scope/generation = %q/%q, want %q/%q", count, envelope.ScopeID, envelope.GenerationID, generation.Scope.ScopeID, generation.Generation.GenerationID)
		}
		if envelope.CollectorKind != "git" || envelope.ObservedAt.IsZero() {
			t.Errorf("envelope %d collector/observed_at = %q/%v, want git/nonzero", count, envelope.CollectorKind, envelope.ObservedAt)
		}
	}
	if got, want := count, 16; got != want {
		t.Fatalf("emitted envelopes = %d, want %d", got, want)
	}
	if _, ok, err := source.Next(context.Background()); err != nil || ok {
		t.Fatalf("Source.Next() after sole scope = (_, %v, %v), want EOF-like (_, false, nil)", ok, err)
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
	odu, err := LoadRepoDependencyFamilyOdu(RepoDependencyFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadRepoDependencyFamilyOdu: %v", err)
	}

	var repoIDs []string
	for _, fact := range odu.Facts {
		if fact.FactKind != repositoryFactKind {
			continue
		}
		repoID, ok := fact.Payload["repo_id"].(string)
		if !ok {
			t.Fatalf("repository fact %q repo_id has type %T, want string", fact.StableFactKey, fact.Payload["repo_id"])
		}
		repoID = strings.TrimSpace(repoID)
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
	for _, sibling := range []string{"gen-1", "gen-ifa-code-call-family-1", "gen-ifa-deployable-unit-family-1"} {
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

	odu, err := LoadRepoDependencyFamilyOdu(RepoDependencyFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadRepoDependencyFamilyOdu: %v", err)
	}

	// Read the cassette independently so the comparison is against the file,
	// not against the loader's own view of it.
	raw, err := os.ReadFile(RepoDependencyFamilyCassetteFullPath(repoRoot))
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
