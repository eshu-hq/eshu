// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
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

	raw, err := os.ReadFile(workloadDependencyFamilyCassetteFullPath(repoRoot))
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
	if len(followups) != 1 {
		t.Fatalf("workload_materialization followups = %d, want exactly 1 so live replay schedules the handler that writes workload_dependency edges", len(followups))
	}
	followup := followups[0]
	if followup.Payload["repo_id"] != workloadDependencyFamilySourceRepoID {
		t.Fatalf("workload_materialization followup repo_id = %#v, want %q", followup.Payload["repo_id"], workloadDependencyFamilySourceRepoID)
	}
	if followup.StableFactKey != "shared_followup:"+workloadDependencyFamilySourceRepoID+":workload_materialization" {
		t.Fatalf("workload_materialization followup stable_fact_key = %q", followup.StableFactKey)
	}
}
