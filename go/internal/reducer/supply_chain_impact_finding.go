// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychaincore"

// SupplyChainImpactFinding is one reducer-owned vulnerability impact finding.
// It is the reducer-root spelling of
// [supplychaincore.SupplyChainImpactFinding]; the type and its field contract
// moved to the shared supply-chain leaf (#6061) so the impact-finding and
// suppression halves of the family can split into sibling packages without
// importing each other. Every reducer-root function keeps its unqualified use.
type SupplyChainImpactFinding = supplychaincore.SupplyChainImpactFinding

// bakeSupplyChainCIDeclaredArtifactIdentity persists the declared artifact
// identity from the strongest matching deployment. An exact subject-digest
// match outranks every image-reference match; fact order breaks ties within
// each strength. Repository, environment, and operational-anchor matches do
// not make an artifact identity claim, so they leave both fields blank.
//
// The selected deployment contributes both fields as one atomic pair, even
// when one field is empty. No other deployment can fill that field, because
// combining deployments could associate a digest and image reference that no
// source declared together. A deployment selected by image reference may carry
// a digest that differs from the finding's subject digest; preserving that
// disagreement lets query-time version resolution report it.
func bakeSupplyChainCIDeclaredArtifactIdentity(
	finding *SupplyChainImpactFinding,
	deployments []supplyChainDeploymentContext,
) {
	var firstImageRefMatch supplyChainDeploymentContext
	hasImageRefMatch := false
	for _, deployment := range deployments {
		strongDigestMatch := finding.SubjectDigest != "" && deployment.artifactDigest != "" &&
			deployment.artifactDigest == finding.SubjectDigest
		if strongDigestMatch {
			finding.CIDeclaredArtifactDigest = deployment.artifactDigest
			finding.CIDeclaredImageRef = deployment.imageRef
			return
		}
		if !hasImageRefMatch &&
			finding.ImageRef != "" &&
			deployment.imageRef != "" &&
			deployment.imageRef == finding.ImageRef {
			firstImageRefMatch = deployment
			hasImageRefMatch = true
		}
	}
	if hasImageRefMatch {
		finding.CIDeclaredArtifactDigest = firstImageRefMatch.artifactDigest
		finding.CIDeclaredImageRef = firstImageRefMatch.imageRef
	}
}
