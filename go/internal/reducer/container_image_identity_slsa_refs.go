// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "sort"

// addSLSADigestRefs ensures every verified SLSA provenance anchor (issue
// #5810 Part B) reaches at least one containerImageRefEvidence entry, so a
// digest attested ONLY by a verified SLSA attestation still produces a
// decision. Before this, extractSLSADigestAnchorsWithQuarantine's
// digest->anchor map fed only applySLSADigestRevision, which enriches an
// EXISTING decision but cannot create one, so such a digest never reached
// BuildContainerImageIdentityDecisionsWithQuarantine's classification loop.
//
// It must be called AFTER byRef already holds every other evidence source's
// refs (see the call site comment in extractContainerImageRefsWithQuarantine):
// for a digest ALREADY named by an existing ref (a git/AWS/Azure/GCP/
// ci.artifact reference), the anchor's build provenance is ATTACHED to that
// ref instead of raising a second, competing bare-digest ref for the same
// digest -- BuildContainerImageIdentityDecisionsWithQuarantine's per-ref loop
// applies applySLSADigestRevision to every decision matching a digest
// unconditionally, so two refs resolving one digest would duplicate every
// BUILT_FROM/DERIVED_FROM row keyed on that (digest, repository_id) pair
// (caught by TestApplySLSADigestRevisionPopulatesBuildProvenanceRepositoryIDs
// once a bare-digest ref could coexist with a gitImageRefFact-derived one). A
// bare-digest ref (mirroring the ci.artifact pattern,
// addCICDArtifactImageReference) is synthesized only when no existing ref
// names that exact digest, so a genuinely ref-less digest still gets one.
// Iterating sorted digest keys keeps evidence fact ID ordering deterministic.
func addSLSADigestRefs(byRef map[string]containerImageRefEvidence, slsaDigest map[string]slsaDigestAnchor) {
	digests := make([]string, 0, len(slsaDigest))
	for digest := range slsaDigest {
		digests = append(digests, digest)
	}
	sort.Strings(digests)
	for _, digest := range digests {
		anchor := slsaDigest[digest]
		if attachSLSADigestAnchorToExistingRefs(byRef, digest, anchor) {
			continue
		}
		addContainerImageDigestRef(
			byRef,
			digest,
			containerImageRefAnchors{buildProvenanceRepositoryIDs: anchor.sourceRepositoryIDs},
			anchor.factIDs...,
		)
	}
}

// attachSLSADigestAnchorToExistingRefs merges a verified SLSA anchor's build
// provenance repositories and evidence fact IDs onto every EXISTING ref whose
// parsed digest already equals the anchor's digest, reporting whether at
// least one match was found. It never creates a new byRef entry -- that stays
// addSLSADigestRefs's job for the no-existing-ref case.
func attachSLSADigestAnchorToExistingRefs(
	byRef map[string]containerImageRefEvidence,
	digest string,
	anchor slsaDigestAnchor,
) bool {
	found := false
	for key, ref := range byRef {
		if ref.parsed.digest != digest {
			continue
		}
		ref.buildProvenanceRepositoryIDs = uniqueSortedStrings(
			append(ref.buildProvenanceRepositoryIDs, anchor.sourceRepositoryIDs...),
		)
		ref.factIDs = uniqueSortedStrings(append(ref.factIDs, anchor.factIDs...))
		byRef[key] = ref
		found = true
	}
	return found
}
