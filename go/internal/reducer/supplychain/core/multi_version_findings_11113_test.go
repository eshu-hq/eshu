// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"cmp"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	multiVersionCVEID     = "CVE-2026-11113"
	multiVersionPackageID = "npm://registry.npmjs.org/brace-expansion"
	multiVersionRepoID    = "repository:r_11113"
	multiVersionAffected  = "1.1.11"
	multiVersionFixed     = "5.0.13"
)

// multiVersionLockfileRow builds one package-lock.json row of
// brace-expansion at version. Each row has its own fact id, as each
// installed copy is its own content entity.
func multiVersionLockfileRow(repositoryID, factID, version, scope string) facts.Envelope {
	row := packageManifestDependencyFactWithMetadata(
		repositoryID,
		"api",
		"package-lock.json",
		"brace-expansion",
		"npm",
		version,
		time.Date(2026, 11, 21, 3, 0, 0, 0, time.UTC),
		map[string]any{
			"section":          "package-lock",
			"lockfile":         true,
			"dependency_scope": scope,
		},
	)
	row.FactID = factID
	row.StableFactKey = "content_entity:" + factID
	return row
}

func multiVersionAdvisory() []facts.Envelope {
	return []facts.Envelope{
		vulnerabilityCVEFact("cve-11113", multiVersionCVEID, 7.5),
		vulnerabilityAffectedPackageRangeFact(
			"affected-11113",
			multiVersionCVEID,
			multiVersionPackageID,
			"npm",
			"brace-expansion",
			"2.0.2",
		),
	}
}

// multiVersionFindingKey is the part of a finding the winners table keys on
// (writer_identity.go) plus the row that represents it: none of it may depend
// on row order.
type multiVersionFindingKey struct {
	repositoryID    string
	version         string
	status          SupplyChainImpactStatus
	dependencyScope string
}

func multiVersionFindingKeys(findings []SupplyChainImpactFinding) []multiVersionFindingKey {
	keys := make([]multiVersionFindingKey, 0, len(findings))
	for _, finding := range findings {
		if finding.CVEID != multiVersionCVEID {
			continue
		}
		keys = append(keys, multiVersionFindingKey{
			repositoryID:    finding.RepositoryID,
			version:         finding.ObservedVersion,
			status:          finding.Status,
			dependencyScope: finding.DependencyScope,
		})
	}
	slices.SortFunc(keys, func(a, b multiVersionFindingKey) int {
		return cmp.Or(cmp.Compare(a.repositoryID, b.repositoryID), cmp.Compare(a.version, b.version))
	})
	return keys
}

// TestManifestFindingsClassifyEachInstalledVersion11113 pins issue #11113: a
// repository whose lockfile holds an affected and a fixed version of one
// package used to get one finding, judged on whichever row loaded first, so
// the fixed row could hide the vulnerable one. Each distinct installed
// version now classifies once, and the finding set is the same in both row
// orders.
func TestManifestFindingsClassifyEachInstalledVersion11113(t *testing.T) {
	t.Parallel()

	affectedRow := multiVersionLockfileRow(multiVersionRepoID, "manifest-dep:11113:affected", multiVersionAffected, "development")
	fixedRow := multiVersionLockfileRow(multiVersionRepoID, "manifest-dep:11113:fixed", multiVersionFixed, "runtime")

	want := []multiVersionFindingKey{
		{repositoryID: multiVersionRepoID, version: multiVersionAffected, status: SupplyChainImpactAffectedExact, dependencyScope: "development"},
		{repositoryID: multiVersionRepoID, version: multiVersionFixed, status: SupplyChainImpactNotAffectedKnownFixed, dependencyScope: "runtime"},
	}
	for _, tc := range []struct {
		name string
		rows []facts.Envelope
	}{
		{name: "fixed row first", rows: []facts.Envelope{fixedRow, affectedRow}},
		{name: "affected row first", rows: []facts.Envelope{affectedRow, fixedRow}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			envelopes := append(multiVersionAdvisory(), tc.rows...)
			got := multiVersionFindingKeys(BuildSupplyChainImpactFindings(envelopes))
			if !slices.Equal(got, want) {
				t.Fatalf("findings = %+v, want %+v: each installed version must classify once, whatever the row order", got, want)
			}
		})
	}
}
