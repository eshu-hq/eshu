// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// TestWorkloadDependencyFamilyRepositoryIdentityDoesNotCollideWithSiblings
// pins the repository and generation identity boundaries the live
// determinism matrix would depend on. A shared repo_id lets one family's
// Repository node silently absorb another family's edges (both MERGE on the
// same Repository{id}); a shared generation ID violates the durable
// active-generation uniqueness constraint when both scopes ACK. Mirrors
// TestRepoDependencyFamilyRepositoryIdentityDoesNotCollideWithSiblings.
func TestWorkloadDependencyFamilyRepositoryIdentityDoesNotCollideWithSiblings(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	odu, err := loadWorkloadDependencyFamilyOdu(workloadDependencyFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadWorkloadDependencyFamilyOdu: %v", err)
	}

	var repoIDs []string
	for _, fact := range odu.Facts {
		if fact.FactKind != repositoryFactKind {
			continue
		}
		repoID, ok := fact.Payload["repo_id"].(string)
		if !ok || strings.TrimSpace(repoID) == "" {
			t.Fatalf("repository fact %q has no repo_id", fact.StableFactKey)
		}
		repoIDs = append(repoIDs, repoID)
	}
	if len(repoIDs) != 6 {
		t.Fatalf("expected 6 repository facts (positive source/target, multi-workload source/target, orphan source/target), got %d", len(repoIDs))
	}
	seen := make(map[string]struct{}, len(repoIDs))
	for _, repoID := range repoIDs {
		if _, dup := seen[repoID]; dup {
			t.Fatalf("two workload_dependency-family repositories share repo_id %q", repoID)
		}
		seen[repoID] = struct{}{}
	}

	siblingRepoIDs := []string{
		deployableUnitFamilyAppRepoID,
		deployableUnitFamilyDeployRepoID,
		deployableUnitFamilyRejectedRepoID,
		deployableUnitFamilyAdmittedNoDeployRepoID,
		repoDependencyFamilySourceRepoID,
		repoDependencyFamilyTargetProvisionsRepoID,
		repoDependencyFamilyTargetUsesModuleRepoID,
		repoDependencyFamilyTargetDiscoversConfigRepoID,
		repoDependencyFamilyTargetDependsOnRepoID,
		repoDependencyFamilyTargetDeploysFromRepoID,
		repoDependencyFamilyTargetReadsConfigRepoID,
	}
	for _, sibling := range siblingRepoIDs {
		if _, collide := seen[sibling]; collide {
			t.Fatalf("workload_dependency-family repo_id collides with sibling family repo_id %q", sibling)
		}
	}

	if len(odu.Facts) == 0 || strings.TrimSpace(odu.Facts[0].GenerationID) == "" {
		t.Fatal("workload_dependency-family Odù has no generation ID; active-generation publication would be unidentifiable")
	}
	for _, sibling := range []string{SQLFamilyGenerationID, CodeCallFamilyGenerationID, deployableUnitFamilyGenerationID, repoDependencyFamilyGenerationID} {
		if odu.Facts[0].GenerationID == sibling {
			t.Fatalf("workload_dependency-family generation ID %q collides with a sibling family's generation; only one live-matrix scope can publish it as active", odu.Facts[0].GenerationID)
		}
	}
}

// TestWorkloadDependencyFamilyOduPreservesEnvelopeFields stops the loader
// being more permissive than production. schema_version is load-bearing: an
// empty version reads as "latest", so a cassette carrying an unsupported
// major would satisfy this guard while live replay preserved the version and
// quarantined the fact -- the offline proof would certify input a live gate
// would reject, which is the one failure this fixture exists to make
// impossible. Mirrors TestRepoDependencyFamilyOduPreservesEnvelopeFields.
func TestWorkloadDependencyFamilyOduPreservesEnvelopeFields(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	odu, err := loadWorkloadDependencyFamilyOdu(workloadDependencyFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadWorkloadDependencyFamilyOdu: %v", err)
	}

	onDisk, err := cassette.LoadFile(workloadDependencyFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("cassette.LoadFile: %v", err)
	}
	var cassetteFacts []cassette.Fact
	for _, scope := range onDisk.Scopes {
		cassetteFacts = append(cassetteFacts, scope.Facts...)
	}
	if len(onDisk.Scopes) != 6 || len(cassetteFacts) != len(odu.Facts) {
		t.Fatalf("fixture shape mismatch: cassette has %d scopes/%d facts, Odù has %d facts", len(onDisk.Scopes), len(cassetteFacts), len(odu.Facts))
	}

	checked := 0
	for i, want := range cassetteFacts {
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

// TestWorkloadDependencyFamilyOduInCatalogSeed proves
// workloadDependencyFamilyOdu is actually registered in catalogSeed
// (catalog_seed.go), not merely constructible. Mirrors
// TestRepoDependencyFamilyOduInCatalogSeed.
func TestWorkloadDependencyFamilyOduInCatalogSeed(t *testing.T) {
	t.Parallel()
	catalog := CatalogByName()
	odu, ok := catalog[workloadDependencyFamilyOduName]
	if !ok {
		t.Fatalf("CatalogByName() is missing %q; workloadDependencyFamilyOdu() must be added to catalogSeed", workloadDependencyFamilyOduName)
	}
	if len(odu.Facts) == 0 {
		t.Fatalf("cataloged %q carries no facts", workloadDependencyFamilyOduName)
	}
}

func TestWorkloadDependencyFamilyOduCarriesProductionFollowup(t *testing.T) {
	t.Parallel()

	var followups []facts.Envelope
	for _, fact := range workloadDependencyFamilyOdu().Odu.Facts {
		if fact.FactKind == "shared_followup" && fact.Payload["reducer_domain"] == "workload_materialization" {
			followups = append(followups, fact)
		}
	}
	wantRepoIDs := map[string]struct{}{
		workloadDependencyFamilySourceRepoID:      {},
		workloadDependencyFamilyTargetRepoID:      {},
		workloadDependencyFamilyMultiSourceRepoID: {},
		workloadDependencyFamilyMultiTargetRepoID: {},
	}
	if len(followups) != len(wantRepoIDs) {
		t.Fatalf("workload_materialization followups = %d, want exactly %d (one per workload-bearing scope)", len(followups), len(wantRepoIDs))
	}
	for _, followup := range followups {
		repoID, _ := followup.Payload["repo_id"].(string)
		if _, ok := wantRepoIDs[repoID]; !ok {
			t.Fatalf("unexpected workload_materialization followup repo_id = %#v", followup.Payload["repo_id"])
		}
		delete(wantRepoIDs, repoID)
		if followup.StableFactKey != "shared_followup:"+repoID+":workload_materialization" {
			t.Fatalf("workload_materialization followup stable_fact_key = %q", followup.StableFactKey)
		}
	}
}

func TestWorkloadDependencyFamilyOduCarriesRepoDependencyPrerequisiteFollowup(t *testing.T) {
	t.Parallel()

	var followups []facts.Envelope
	for _, fact := range workloadDependencyFamilyOdu().Odu.Facts {
		if fact.FactKind == "shared_followup" && fact.Payload["reducer_domain"] == "deployment_mapping" {
			followups = append(followups, fact)
		}
	}
	wantRepoIDs := map[string]struct{}{
		workloadDependencyFamilySourceRepoID:       {},
		workloadDependencyFamilyMultiSourceRepoID:  {},
		workloadDependencyFamilyOrphanSourceRepoID: {},
	}
	if len(followups) != len(wantRepoIDs) {
		t.Fatalf("deployment_mapping followups = %d, want exactly %d (one per dependency-source scope)", len(followups), len(wantRepoIDs))
	}
	for _, followup := range followups {
		repoID, _ := followup.Payload["repo_id"].(string)
		if _, ok := wantRepoIDs[repoID]; !ok {
			t.Fatalf("unexpected deployment_mapping followup repo_id = %#v", followup.Payload["repo_id"])
		}
		delete(wantRepoIDs, repoID)
		if followup.StableFactKey != "shared_followup:"+repoID+":deployment_mapping" {
			t.Fatalf("deployment_mapping followup stable_fact_key = %q", followup.StableFactKey)
		}
	}
}
