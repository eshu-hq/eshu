// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSupplyChainImpactFindingPrefersConsumptionRepositoryOverSBOMImageIdentity
// is the #5780 regression guard for the SBOM path, the sibling of the #5779
// fix for the OS-package path (supply_chain_impact_repository_anchor_test.go).
// A finding with BOTH a per-package consumption anchor (a git "github.com/..."
// repository) AND SBOM component/attachment/image evidence — but NO OS-package
// evidence — must keep the consumption anchor. The SBOM branch of
// classifySupplyChainImpactPackage (supply_chain_impact_index.go) overwrote
// RepositoryID with the image's OCI registry path
// (containerImageIdentity.repository_id), which #5463 forbids as an anchor: an
// OCI registry path can never equal a git "repository:..." workload/service/
// deployment-lane id, so the finding would be unit-green but a dead anchor,
// unreachable from runtime context in production. #5779 added the
// consumptionRepositoryID guard to the OS-package path only; this proves the
// same precedence now holds when the image evidence arrives via SBOM instead.
//
// It drives the production subject through BuildSupplyChainImpactFindings ->
// classifySupplyChainImpactPackage; unlike the OS-package path, the SBOM
// branch's image.repositoryID comes straight from the container_image_identity
// fact in the same fact set, so no cross-scope digest-seeded load is needed and
// the simpler direct driver exercises the real branch.
func TestSupplyChainImpactFindingPrefersConsumptionRepositoryOverSBOMImageIdentity(t *testing.T) {
	t.Parallel()

	const (
		packageID               = "pkg:npm/precedence-sbom"
		purl                    = "pkg:npm/precedence-sbom@1.2.3"
		documentID              = "sbom-doc-precedence"
		subjectDigest           = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
		consumptionRepositoryID = "github.com/example/consumption-sbom-app"
		imageOCIRepositoryID    = "oci-registry://ghcr.io/example/precedence-sbom-app"
	)

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-precedence-sbom", "CVE-2026-5780", 7.5),
		vulnerabilityAffectedPackageFact(
			"affected-precedence-sbom", "CVE-2026-5780", packageID, "npm", "precedence-sbom", "1.2.3", "1.3.0",
		),
		packageConsumptionFactWithRange("consume-precedence-sbom", packageID, consumptionRepositoryID, "1.2.3"),
		sbomComponentImpactFact("component-precedence-sbom", documentID, purl),
		sbomAttachmentImpactFact("attachment-precedence-sbom", documentID, subjectDigest),
		containerImageIdentityImpactFact("image-precedence-sbom", subjectDigest, imageOCIRepositoryID),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-5780"]
	// Sanity: the SBOM component path fired (this is an SBOM-tier finding, not an
	// os-package one), so the assertion below exercises the SBOM branch's guard.
	if !slices.Contains(got.EvidencePath, facts.SBOMComponentFactKind) {
		t.Fatalf("EvidencePath = %#v, want the SBOM component path to have fired", got.EvidencePath)
	}
	if got.RepositoryID != consumptionRepositoryID {
		t.Fatalf(
			"RepositoryID = %q, want the consumption-derived %q to win over the SBOM image-identity OCI path %q",
			got.RepositoryID, consumptionRepositoryID, imageOCIRepositoryID,
		)
	}
}
