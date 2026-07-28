// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "github.com/eshu-hq/eshu/go/internal/truth"

// SupplyChainVersionResolutionCorroboration is one weaker deployment-truth
// tier's own digest/version claim for a finding whose judged version/digest
// (version_resolution_tier) was resolved from a stronger tier. Every present
// weaker tier is disclosed here, including a tier whose claim disagrees with
// the winner -- Agrees is false in that case rather than the entry being
// dropped, so an operator can see that two evidence sources disagree instead
// of only ever seeing confirmation.
type SupplyChainVersionResolutionCorroboration struct {
	Tier            string `json:"tier"`
	DigestOrVersion string `json:"digest_or_version,omitempty"`
	EvidenceKind    string `json:"evidence_kind,omitempty"`
	Agrees          bool   `json:"agrees"`
}

// supplyChainVersionResolutionClaim returns the digest or version one
// deployment-truth tier's evidence asserts for the finding row, and the
// evidence_kind label naming that evidence's source. An empty value means the
// tier makes no claim for this row -- either its evidence is entirely absent,
// or evidence exists but carries no concrete artifact identity to disclose.
// Never fabricates a claim: a tier with present evidence but no identity
// signal returns "" rather than guessing.
func supplyChainVersionResolutionClaim(
	tier truth.DeploymentTruthTier,
	row SupplyChainImpactFindingRow,
) (value string, evidenceKind string) {
	switch tier {
	case truth.TierRuntimeConfirmed:
		// probeSupplyChainCloudRuntimeResources (supply_chain_impact_cloud_runtime_probe.go)
		// only ever populates CloudRuntimeResourceRefs by matching a
		// CloudResource's running_image_digest against row.SubjectDigest
		// exactly, so the winning claim IS that digest by construction --
		// there is no separate "runtime digest" field to read.
		if len(row.CloudRuntimeResourceRefs) == 0 || row.SubjectDigest == "" {
			return "", ""
		}
		return row.SubjectDigest, "cloud_runtime_probe"

	case truth.TierProvenanceCIDeclared:
		// The claim exists iff the reducer baked a CI-declared artifact
		// identity (#5469, applySupplyChainRuntimeContext /
		// bakeSupplyChainCIDeclaredArtifactIdentity): that only happens when
		// the matched cicd_run_correlation deployment matched through a
		// STRONG identity branch (its own artifact_digest or image_ref
		// equalled the finding's), never the weak
		// repository+environment+operational-anchor branch (#5426's branch
		// 3). The claim's digest_or_version is the deployment's OWN declared
		// value -- never row.SubjectDigest/ImageRef, which would silently
		// borrow the finding's own identity and could never disagree with
		// itself. Digest is preferred first (same axis as the digest-based
		// runtime_confirmed and config_only claims), image ref only when the
		// deployment carried no digest.
		//
		// A row whose only CI/CD evidence matched through the weak branch
		// still drives deployment_truth_tier=provenance_ci_declared (see
		// rowHasCIDeclaredDeploymentEvidence, supply_chain_impact_result.go)
		// but makes no version/digest claim here, so it falls through to
		// config_only instead of fabricating one.
		if row.CIDeclaredArtifactDigest != "" {
			return row.CIDeclaredArtifactDigest, "cicd_run_correlation"
		}
		if row.CIDeclaredImageRef != "" {
			return row.CIDeclaredImageRef, "cicd_run_correlation"
		}
		return "", ""

	case truth.TierDeclaredRef:
		// #5393 (a DEPLOYS_REF edge naming a branch/SHA declared deployed) is
		// unshipped: no evidence producer exists anywhere in this pipeline.
		// Fail closed -- this tier must never be emitted until #5393 lands
		// and wires real evidence through this function.
		return "", ""

	case truth.TierConfigOnly:
		if row.ObservedVersion != "" {
			return row.ObservedVersion, "config_materialization"
		}
		if row.SubjectDigest != "" {
			return row.SubjectDigest, "config_materialization"
		}
		if row.ImageRef != "" {
			return row.ImageRef, "config_materialization"
		}
		return "", ""

	default:
		return "", ""
	}
}

// supplyChainVersionResolution resolves the finding row's judged
// version/digest tier and its corroboration block (#5469): the strongest
// deployment-truth tier that makes a concrete version/digest claim wins, and
// every weaker tier that also makes a claim is preserved as corroboration,
// flagged Agrees=false when its claim differs textually from the winner's.
// The tier vocabulary is truth.DeploymentTruthTier verbatim (no new enum),
// evaluated strongest-first via truth.AllDeploymentTruthTiers so the winner
// is always the first tier with a non-empty claim.
//
// This is read-time only: it classifies fields the row already carries (no
// new graph or Postgres query), mirroring how supplyChainDeploymentTruthTier
// (supply_chain_impact_result.go) classifies the same row for
// deployment_truth_tier. A finding with no version/digest evidence at all --
// not even a config-materialized one -- returns ("", nil).
func supplyChainVersionResolution(
	row SupplyChainImpactFindingRow,
) (tier string, corroboration []SupplyChainVersionResolutionCorroboration) {
	type candidate struct {
		tier  truth.DeploymentTruthTier
		value string
		kind  string
	}

	var candidates []candidate
	for _, t := range truth.AllDeploymentTruthTiers() {
		value, kind := supplyChainVersionResolutionClaim(t, row)
		if value == "" {
			continue
		}
		candidates = append(candidates, candidate{tier: t, value: value, kind: kind})
	}
	if len(candidates) == 0 {
		return "", nil
	}

	winner := candidates[0]
	if len(candidates) > 1 {
		corroboration = make([]SupplyChainVersionResolutionCorroboration, 0, len(candidates)-1)
		for _, c := range candidates[1:] {
			corroboration = append(corroboration, SupplyChainVersionResolutionCorroboration{
				Tier:            string(c.tier),
				DigestOrVersion: c.value,
				EvidenceKind:    c.kind,
				Agrees:          c.value == winner.value,
			})
		}
	}
	return string(winner.tier), corroboration
}
