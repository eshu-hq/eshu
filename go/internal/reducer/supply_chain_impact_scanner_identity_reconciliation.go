// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// reconcileSupplyChainScannerIdentityDigest cross-checks the scanner's image
// digest against every container_image_identity fact for the same repository.
// When the scanner digest matches no identity (empty repoID), or the matched
// identity has multiple repositories (ambiguous), no reconciliation evidence
// is produced. When the scanner digest matches an identity whose
// sourceRepositoryIDs name exactly one repository, this function iterates over
// ALL identities in the index to detect any that name the SAME repository with
// a DIFFERENT digest — a disagreement between the scanner pipeline and the
// CI/cloud identity pipeline. Disagreement is surfaced as explicit
// missing_evidence so an operator can triage it, rather than silently trusting
// either pipeline.
func reconcileSupplyChainScannerIdentityDigest(
	scannerDigest string,
	repoID string,
	identities map[string]supplyChainImageIdentity,
) []string {
	if scannerDigest == "" || repoID == "" {
		return nil
	}
	var mismatches []string
	for digest, identity := range identities {
		if digest == scannerDigest {
			continue
		}
		if len(identity.sourceRepositoryIDs) != 1 {
			continue
		}
		if identity.sourceRepositoryIDs[0] != repoID {
			continue
		}
		mismatches = append(mismatches,
			"scanner_identity_digest_mismatch: scanner="+scannerDigest+
				", identity="+digest)
	}
	return uniqueSortedStrings(mismatches)
}
