// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestDerivedPassReportsNewestCopyDrops pins the #11048 item-3 fix: the
// derived pass applies the #10735 newest-copy admission, so the products it
// drops must reach the products_dropped_by_newest_copy sub-signal the same
// way the static pass reports them. The dropped product forms no derived
// finding (a derived finding needs scan evidence in its evidence set), which
// is exactly why the operator signal carries the pre-anchor caveat: it
// counts matches that would not have produced a finding.
func TestDerivedPassReportsNewestCopyDrops(t *testing.T) {
	t.Parallel()

	const cveID = "CVE-2026-9106"
	advisories := []facts.Envelope{
		newestCopyNVDProductCVE("cve-10735-d-base", "scope:nvd-baseline", cveID, 7.5, "HIGH", newestCopyProductBaselineStamp),
		newestCopyProduct("product-10735-d-a", "scope:nvd-baseline", cveID, newestCopyProductCriteriaA, newestCopyProductMatchA),
		newestCopyNVDProductCVE("cve-10735-d-win", "scope:nvd-window", cveID, 9.8, "CRITICAL", newestCopyProductWindowStamp),
		newestCopyProduct("product-10735-d-b", "scope:nvd-window", cveID, newestCopyProductCriteriaB, newestCopyProductMatchB),
	}
	authorityFor := func(scope string) AdvisoryAuthorityDependency {
		return AdvisoryAuthorityDependency{
			Kind: "public_registration", ID: "registration-1", RevisionID: "revision-1",
			AdvisoryScopeID: scope, AdvisoryGenerationID: "generation:" + scope,
			ConfigFingerprint: "config-1", ProviderID: "nvd",
			TrustedSourceClass: "nvd", ContentProfile: "supply_chain_impact_v1",
		}
	}
	dependencies := []AdvisoryAuthorityDependency{
		authorityFor("scope:nvd-baseline"),
		authorityFor("scope:nvd-window"),
	}
	const digest = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	targetFor := func(id, target, generation string) AuthorizedScanImpactEvidence {
		scan := derivedPartitionScan(id, target, generation, digest, "3.0.11-1~deb12u2")
		return AuthorizedScanImpactEvidence{
			ScanTargetID: target, ScanGenerationID: generation,
			OSPackages: []facts.Envelope{scan[0]}, Analyses: []facts.Envelope{scan[1]},
			Advisories: advisories, AuthorityDependencies: dependencies, Complete: true,
		}
	}
	const (
		firstTarget  = "image://derived/eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
		secondTarget = "image://derived/ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	)

	t.Run("derived drop reaches the sub-signal", func(t *testing.T) {
		t.Parallel()

		_, _, stats, err := buildAuthorizedSupplyChainImpactFindings(supplyChainImpactLoadedEvidence{
			asAuthorized: &AuthorizedSupplyChainImpactLoad{
				Targets: []AuthorizedScanImpactEvidence{targetFor("drop-a", firstTarget, "generation-a")},
			},
		})
		if err != nil {
			t.Fatalf("buildAuthorizedSupplyChainImpactFindings() error = %v", err)
		}
		if stats.productsDroppedByNewestCopy != 1 {
			t.Fatalf("productsDroppedByNewestCopy = %d, want 1 (the baseline copy's product drops in the derived pass)", stats.productsDroppedByNewestCopy)
		}
		subSignals := map[string]float64{}
		SupplyChainImpactHandler{}.recordBuildStats(context.Background(), stats, subSignals)
		// The literal is the operator-facing contract; do not route it through the constant.
		if got, ok := subSignals["products_dropped_by_newest_copy"]; !ok || got != 1 {
			t.Fatalf("SubSignals[products_dropped_by_newest_copy] = %v (present %v), want 1", got, ok)
		}
	})

	t.Run("per-target drops sum across targets", func(t *testing.T) {
		t.Parallel()

		_, _, stats, err := buildAuthorizedSupplyChainImpactFindings(supplyChainImpactLoadedEvidence{
			asAuthorized: &AuthorizedSupplyChainImpactLoad{
				// Both targets share one advisory slice, so the second
				// target reads the cached index: the summed count must
				// still count each target's drop.
				Targets: []AuthorizedScanImpactEvidence{
					targetFor("drop-a", firstTarget, "generation-a"),
					targetFor("drop-b", secondTarget, "generation-b"),
				},
			},
		})
		if err != nil {
			t.Fatalf("buildAuthorizedSupplyChainImpactFindings() error = %v", err)
		}
		if stats.productsDroppedByNewestCopy != 2 {
			t.Fatalf("productsDroppedByNewestCopy = %d, want 2 (one drop per target)", stats.productsDroppedByNewestCopy)
		}
	})
}
