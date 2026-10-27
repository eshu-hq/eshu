// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// TestSupplyChainImpactFindingPrefersConsumptionRepositoryOverSBOMImageIdentity
// keeps unassociated manifest and image repositories on separate findings.
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

	if len(findings) != 2 {
		t.Fatalf("findings=%#v", findings)
	}
	image := supplyChainImpactFindingForDigest(t, findings, subjectDigest)
	manifest := supplyChainImpactFindingForDigest(t, findings, "")
	if !slices.Contains(image.EvidencePath, chain.SBOMComponentFactKind) || image.RepositoryID != imageOCIRepositoryID {
		t.Fatalf("image=%#v", image)
	}
	if manifest.RepositoryID != consumptionRepositoryID || slices.Contains(manifest.EvidencePath, chain.SBOMComponentFactKind) {
		t.Fatalf("manifest=%#v", manifest)
	}
}
