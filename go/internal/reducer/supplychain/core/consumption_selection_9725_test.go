// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
)

// TestBuildSupplyChainImpactFindingsEmitsPerRepositoryManifestFindings9725
// pins issue #9725: a single representative consumption per package drops
// non-lockfile consumers. The mixed-case PyYAML==3.11 manifest row (no
// installed version, no lockfile flag) must classify alongside the lowercase
// lockfile row — never be silently ignored because the lockfile row won
// firstConsumption.
//
// The ==-pinned precision answer pinned here: a row with no observed version
// and only an == requirement classifies possibly_affected with reason
// range_only_manifest — an == pin is a requirement, not proof of the
// installed version, so it never claims affected_exact.
func TestBuildSupplyChainImpactFindingsEmitsPerRepositoryManifestFindings9725(t *testing.T) {
	t.Parallel()

	const (
		cveID     = "CVE-2020-14343"
		packageID = "pkg:pypi/pyyaml"
		// Loser row first in reader order: preference (not input order) is
		// what drops it, mirroring QA where the lowercase lockfile row won.
		manifestRepo = "repository:r_2812ef2a"
		lockfileRepo = "repository:r_ccdc21d1"
	)

	manifestRow := facts.Envelope{
		FactID:   "consume-9725-manifest",
		FactKind: correlation.PackageConsumptionFactKind,
		Payload: map[string]any{
			"package_id":       packageID,
			"repository_id":    manifestRepo,
			"dependency_range": "==3.11",
		},
	}
	lockfileRow := facts.Envelope{
		FactID:   "consume-9725-lockfile",
		FactKind: correlation.PackageConsumptionFactKind,
		Payload: map[string]any{
			"package_id":        packageID,
			"repository_id":     lockfileRepo,
			"dependency_range":  "6.0.3",
			"installed_version": "6.0.3",
			"lockfile":          true,
		},
	}

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-9725", cveID, 9.8),
		vulnerabilityAffectedPackageRangeFact(
			"affected-9725",
			cveID,
			packageID,
			"pypi",
			"pyyaml",
			"5.4",
		),
		manifestRow,
		lockfileRow,
	})

	byRepo := make(map[string]SupplyChainImpactFinding, len(findings))
	for _, finding := range findings {
		if finding.CVEID != cveID {
			continue
		}
		byRepo[finding.RepositoryID] = finding
	}

	lockfileFinding, ok := byRepo[lockfileRepo]
	if !ok {
		t.Fatalf("no finding for lockfile repo %q (regression): %#v", lockfileRepo, findings)
	}
	assertSupplyChainImpactStatus(t, lockfileFinding, SupplyChainImpactNotAffectedKnownFixed)

	manifestFinding, ok := byRepo[manifestRepo]
	if !ok {
		t.Fatalf("no finding for manifest repo %q: single-representative selection dropped the non-lockfile consumer: %#v", manifestRepo, findings)
	}
	assertSupplyChainImpactStatus(t, manifestFinding, SupplyChainImpactPossiblyAffected)
	if manifestFinding.MatchReason != supplyChainVersionReasonRangeOnlyManifest {
		t.Fatalf("MatchReason = %q, want %q (==-pinned row without an observed version is possibly_affected, never affected_exact)", manifestFinding.MatchReason, supplyChainVersionReasonRangeOnlyManifest)
	}
}
