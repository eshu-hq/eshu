// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// TestAddManifestDependencySupplyChainConsumptionBoundsFanout is the
// regression test for the watermark stall of 2026-11-24: a single
// supply_chain_impact pass materialized one consumption row per matching
// (manifest dependency, affected package) pair with no bound, so a
// batch-scale intent (hundreds of dependencies times hundreds of advisories
// naming the same package) retained tens of thousands of near-duplicate rows
// for one package and OOMKilled the vulnerability engine at 24Gi. The
// classifier downstream only ever reads one representative row per
// (repository, observed version) — firstConsumption — plus the repository
// set, so the join must retain exactly those and nothing else.
func TestAddManifestDependencySupplyChainConsumptionBoundsFanout(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	const packageID = "npm://registry.npmjs.org/libfoo"

	envelopes := make([]facts.Envelope, 0, 220)
	addDeps := func(repo string, count int, versions []string, lockfile bool) {
		for i := range count {
			metadata := map[string]any{"section": "dependencies"}
			if lockfile {
				metadata["lockfile"] = true
			}
			envelopes = append(envelopes, packageManifestDependencyFactWithMetadata(
				repo,
				"test",
				fmt.Sprintf("lock-%s-%d.json", repo[len(repo)-1:], i),
				"libfoo",
				"npm",
				versions[i%len(versions)],
				observedAt,
				metadata,
			))
		}
	}
	// Four repositories with exact observed versions. R1's 1.0.0 rows are
	// lockfile-preferred so the representative pick is pinned, not just the
	// count: the first R1 lockfile row must win its group, while a plain row
	// wins every other group.
	addDeps("repository:test-bound-1", 25, []string{"1.0.0"}, true)
	addDeps("repository:test-bound-1", 25, []string{"2.0.0"}, false)
	addDeps("repository:test-bound-2", 50, []string{"1.0.0", "2.0.0"}, false)
	addDeps("repository:test-bound-3", 50, []string{"1.0.0", "2.0.0"}, false)
	addDeps("repository:test-bound-4", 50, []string{"1.0.0", "2.0.0"}, false)
	// One repository whose rows carry no observed version: they keep the
	// one-finding rule only because no versioned row names the repository.
	for i := range 10 {
		envelopes = append(envelopes, packageManifestDependencyFactWithMetadata(
			"repository:test-bound-5",
			"test",
			fmt.Sprintf("lock-5-%d.json", i),
			"libfoo",
			"npm",
			"^1.0.0",
			observedAt,
			map[string]any{"section": "dependencies", "dependency_resolution_state": "unresolved"},
		))
	}

	affected := make([]supplychainmodel.AffectedPackage, 0, 200)
	for i := range 200 {
		affected = append(affected, supplychainmodel.AffectedPackage{
			FactID:     fmt.Sprintf("affected-libfoo-%d", i),
			AdvisoryID: fmt.Sprintf("GHSA-test-libfoo-%d", i),
			PackageID:  packageID,
			Ecosystem:  "npm",
			Name:       "libfoo",
		})
	}

	index := &supplyChainImpactIndex{
		affectedPackages: map[string][]supplychainmodel.AffectedPackage{"ghsa-libfoo": affected},
		consumption:      map[string][]supplychainmodel.PackageConsumption{},
	}
	addManifestDependencySupplyChainConsumption(index, envelopes)

	rows := index.consumption[packageID]
	// Four versioned repositories times two versions, plus the one
	// versionless repository: every further matching pair duplicates one of
	// these representatives.
	const wantRows = 9
	if len(rows) != wantRows {
		t.Fatalf("consumption rows for one package = %d, want %d bounded representatives", len(rows), wantRows)
	}
	byGroup := map[string]supplychainmodel.PackageConsumption{}
	for _, row := range rows {
		if row.PackageID != packageID {
			t.Errorf("consumption row PackageID = %q, want %q", row.PackageID, packageID)
		}
		if row.RepositoryID == "" {
			t.Errorf("consumption row with blank repository must not be retained")
		}
		key := row.RepositoryID + "\x00" + consumptionObservedVersion("npm", row)
		if _, dup := byGroup[key]; dup {
			t.Errorf("consumption group %q has more than the representative row", key)
		}
		byGroup[key] = row
	}
	// The representative keeps the preference rule: a lockfile row wins its
	// version group, otherwise the group keeps a plain row.
	if got := byGroup["repository:test-bound-1\x001.0.0"]; !got.Lockfile {
		t.Errorf("R1 1.0.0 representative Lockfile = false, want the preferred lockfile row")
	}
	for _, key := range []string{
		"repository:test-bound-1\x002.0.0",
		"repository:test-bound-2\x001.0.0",
		"repository:test-bound-2\x002.0.0",
	} {
		if got := byGroup[key]; got.Lockfile {
			t.Errorf("%q representative Lockfile = true, want the plain first row", key)
		}
	}
	if got := byGroup["repository:test-bound-5\x00"]; consumptionObservedVersion("npm", got) != "" {
		t.Errorf("R5 representative carries version %q, want the versionless row", consumptionObservedVersion("npm", got))
	}
}
