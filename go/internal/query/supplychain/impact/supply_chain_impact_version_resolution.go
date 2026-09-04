// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "github.com/eshu-hq/eshu/go/internal/truth"

// Agreement is the closed, three-state vocabulary a
// SupplyChainVersionResolutionCorroboration entry reports (issue #5469
// review finding R6). Raw textual equality across incomparable identity axes
// -- a dpkg version string can never equal a sha256 digest -- would make
// "disagrees" a guaranteed-false comparison an operator could mistake for a
// real contradiction. The axis (digest, image_ref, or version) of the
// corroborating claim is compared against the winner's own axis first: only
// a same-axis pair is ever judged agrees/disagrees, and a cross-axis pair is
// always not_comparable.
const (
	supplyChainVersionResolutionAgrees        = "agrees"
	supplyChainVersionResolutionDisagrees     = "disagrees"
	supplyChainVersionResolutionNotComparable = "not_comparable"
)

// supplyChainVersionResolutionAxis names which identity space one claim's
// digest_or_version value belongs to. Two claims are only ever compared for
// agreement when their axes match.
type supplyChainVersionResolutionAxis string

const (
	axisDigest   supplyChainVersionResolutionAxis = "digest"
	axisImageRef supplyChainVersionResolutionAxis = "image_ref"
	axisVersion  supplyChainVersionResolutionAxis = "version"
)

// SupplyChainVersionResolutionCorroboration is one weaker deployment-truth
// tier's own digest/version claim for a finding whose judged version/digest
// (version_resolution_tier) was resolved from a stronger tier. Every present
// weaker tier is disclosed here, including a tier whose claim disagrees with
// the winner -- Agreement is "disagrees" in that case rather than the entry
// being dropped, so an operator can see that two evidence sources disagree
// instead of only ever seeing confirmation. A cross-axis pair (for example a
// version compared against a digest) can never textually agree, so it is
// reported "not_comparable" rather than a misleading "disagrees".
type SupplyChainVersionResolutionCorroboration struct {
	Tier            string `json:"tier"`
	DigestOrVersion string `json:"digest_or_version,omitempty"`
	EvidenceKind    string `json:"evidence_kind,omitempty"`
	Agreement       string `json:"agreement"`
}

// supplyChainVersionResolutionCandidate is one deployment-truth tier's claim
// under consideration for version resolution. Ineligible is set only for a
// provenance_ci_declared claim that contradicts the finding's own
// SubjectDigest (review finding R1): such a candidate is still disclosed as
// corroboration but can never be selected as the winner.
type supplyChainVersionResolutionCandidate struct {
	tier       truth.DeploymentTruthTier
	value      string
	kind       string
	axis       supplyChainVersionResolutionAxis
	ineligible bool
}

// supplyChainVersionResolutionClaim returns the digest or version one
// deployment-truth tier's evidence asserts for the finding row, the
// evidence_kind label naming that evidence's source, and the identity axis
// that value belongs to. An empty value means the tier makes no claim for
// this row -- either its evidence is entirely absent, or evidence exists but
// carries no concrete artifact identity to disclose. Never fabricates a
// claim: a tier with present evidence but no identity signal returns ""
// rather than guessing.
func supplyChainVersionResolutionClaim(
	tier truth.DeploymentTruthTier,
	row *SupplyChainImpactFindingRow,
) (value string, evidenceKind string, axis supplyChainVersionResolutionAxis) {
	switch tier {
	case truth.TierRuntimeConfirmed:
		// Both runtime probes only populate their refs after an exact digest
		// match against row.SubjectDigest, so the winning claim is the parent
		// digest by construction. Prefer the cloud evidence label when both
		// independent sources are present to preserve the established wire
		// value; otherwise identify the Kubernetes probe explicitly.
		if row.SubjectDigest == "" {
			return "", "", ""
		}
		if len(row.CloudRuntimeResourceRefs) > 0 {
			return row.SubjectDigest, "cloud_runtime_probe", axisDigest
		}
		if len(row.KubernetesRuntimeWorkloadRefs) > 0 {
			return row.SubjectDigest, "kubernetes_runtime_probe", axisDigest
		}
		return "", "", ""

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
		// config_only instead of fabricating one -- or is omitted entirely
		// if config_only itself has no claim either (no ObservedVersion,
		// SubjectDigest, or ImageRef). A claim that DOES exist
		// here may still be ineligible to win (see
		// supplyChainCIDeclaredDigestContradictsFinding) when it contradicts
		// the finding's own SubjectDigest -- that ineligibility is applied
		// by the caller, not this function, since this function only
		// reports what the evidence asserts.
		if row.CIDeclaredArtifactDigest != "" {
			return row.CIDeclaredArtifactDigest, "cicd_run_correlation", axisDigest
		}
		if row.CIDeclaredImageRef != "" {
			return row.CIDeclaredImageRef, "cicd_run_correlation", axisImageRef
		}
		return "", "", ""

	case truth.TierDeclaredRef:
		// #5393 (a DEPLOYS_REF edge naming a branch/SHA declared deployed) is
		// unshipped: no evidence producer exists anywhere in this pipeline.
		// Fail closed -- this tier must never be emitted until #5393 lands
		// and wires real evidence through this function.
		return "", "", ""

	case truth.TierConfigOnly:
		if row.ObservedVersion != "" {
			return row.ObservedVersion, "config_materialization", axisVersion
		}
		if row.SubjectDigest != "" {
			return row.SubjectDigest, "config_materialization", axisDigest
		}
		if row.ImageRef != "" {
			return row.ImageRef, "config_materialization", axisImageRef
		}
		return "", "", ""

	default:
		return "", "", ""
	}
}

// supplyChainCIDeclaredDigestContradictsFinding reports whether the
// CI-declared artifact digest the reducer baked (issue #5469) actively
// contradicts the finding's own subject digest -- the same contradiction
// supplyChainDeploymentPromotesRuntimeReachability (supply_chain_impact_runtime.go)
// already treats as decisive evidence the vulnerable artifact was NOT what
// shipped. This can happen even for a STRONG-branch match: a deployment can
// match through image-ref equality while its own artifact_digest genuinely
// differs from the finding's SubjectDigest (a tag that moved to a different
// build) -- bakeSupplyChainCIDeclaredArtifactIdentity bakes that contradicting
// digest deliberately, as real disagreement evidence, not a matching
// artifact.
//
// Review finding R1 (P0): a contradicting CI-declared digest must never win
// version resolution -- doing so would let a foreign artifact's digest
// become the judged value while config_only (or runtime_confirmed) still
// reports the finding's own subject_digest, so two surfaces would read
// identical evidence in opposite directions. The contradicting claim still
// appears as corroboration (with a disagrees Agreement, since it shares the
// digest axis with the finding's own identity) -- it is only barred from
// being the winner.
func supplyChainCIDeclaredDigestContradictsFinding(row *SupplyChainImpactFindingRow) bool {
	return row.SubjectDigest != "" &&
		row.CIDeclaredArtifactDigest != "" &&
		row.CIDeclaredArtifactDigest != row.SubjectDigest
}

// supplyChainVersionResolutionAgreement classifies one corroborating claim
// against the winning claim (issue #5469 review finding R6): same-axis
// values are compared textually (agrees/disagrees), a cross-axis pair (for
// example a config-materialized version compared against a digest-based
// winner) is always not_comparable, since no textual comparison between them
// could ever be meaningful.
func supplyChainVersionResolutionAgreement(
	value string, axis supplyChainVersionResolutionAxis,
	winnerValue string, winnerAxis supplyChainVersionResolutionAxis,
) string {
	if axis != winnerAxis {
		return supplyChainVersionResolutionNotComparable
	}
	if value == winnerValue {
		return supplyChainVersionResolutionAgrees
	}
	return supplyChainVersionResolutionDisagrees
}

// supplyChainVersionResolution resolves the finding row's judged
// version/digest tier and its corroboration block (#5469): the strongest
// deployment-truth tier that makes a concrete, ELIGIBLE version/digest claim
// about the finding's own identity wins, and every other tier that also
// makes a claim is preserved as corroboration, classified agrees/disagrees/
// not_comparable against the winner (review finding R6). The tier vocabulary
// is truth.DeploymentTruthTier verbatim (no new enum), evaluated
// strongest-first via truth.AllDeploymentTruthTiers.
//
// A provenance_ci_declared claim that contradicts the finding's own
// SubjectDigest is INELIGIBLE to win (review finding R1): it still appears
// as corroboration -- disagrees, since it shares the digest axis with the
// finding's own identity -- but resolution falls through to the next tier
// with an eligible claim (typically config_only's own SubjectDigest/
// ObservedVersion). A contradicting CI claim is real evidence the deployed
// artifact was NOT the vulnerable one; crediting it as the judged winner
// would silently substitute a foreign artifact's identity for the finding's
// own, exactly the substitution
// supplyChainDeploymentPromotesRuntimeReachability already refuses to trust
// for runtime-reachability promotion.
//
// This is read-time only: it classifies fields the row already carries (no
// new graph or Postgres query), mirroring how supplyChainDeploymentTruthTier
// (supply_chain_impact_result.go) classifies the same row for
// deployment_truth_tier. A finding with no eligible version/digest evidence
// at all -- not even a config-materialized one -- returns ("", nil).
func supplyChainVersionResolution(
	row *SupplyChainImpactFindingRow,
) (tier string, corroboration []SupplyChainVersionResolutionCorroboration) {
	ciContradicts := supplyChainCIDeclaredDigestContradictsFinding(row)

	// truth.AllDeploymentTruthTiers has exactly four members; the candidate
	// set can never exceed that. Keep the bounded working set on the stack
	// rather than allocating or copying the large finding row on this
	// per-row read path.
	var candidateStorage [4]supplyChainVersionResolutionCandidate
	candidateCount := 0
	for _, t := range truth.AllDeploymentTruthTiers() {
		value, kind, axis := supplyChainVersionResolutionClaim(t, row)
		if value == "" {
			continue
		}
		ineligible := t == truth.TierProvenanceCIDeclared && ciContradicts
		candidateStorage[candidateCount] = supplyChainVersionResolutionCandidate{
			tier: t, value: value, kind: kind, axis: axis, ineligible: ineligible,
		}
		candidateCount++
	}
	candidates := candidateStorage[:candidateCount]
	if len(candidates) == 0 {
		return "", nil
	}

	winnerIndex := -1
	for i, c := range candidates {
		if !c.ineligible {
			winnerIndex = i
			break
		}
	}
	if winnerIndex == -1 {
		// No candidate is eligible to win. Structurally unreachable today --
		// ineligible is only ever set on provenance_ci_declared, and only
		// when ciContradicts is true, which itself requires
		// row.SubjectDigest to be non-empty; config_only's claim always
		// resolves to at least that SubjectDigest whenever it is non-empty,
		// so an ineligible CI claim can never be the row's only candidate.
		// Kept as an explicit, fail-closed branch rather than silently
		// defaulting to candidates[0] (which could be the ineligible,
		// contradicting claim itself): never credit an ineligible claim as
		// winner by omission.
		return "", supplyChainVersionResolutionCorroborationEntries(candidates, -1)
	}

	winner := candidates[winnerIndex]
	return string(winner.tier), supplyChainVersionResolutionCorroborationEntries(candidates, winnerIndex)
}

// supplyChainVersionResolutionCorroborationEntries builds the corroboration
// list for every candidate other than the one at winnerIndex. winnerIndex
// -1 means no candidate won: every candidate is then reported
// not_comparable, since there is no winning value to compare against.
// Returns nil when there is nothing to disclose, matching the "omitted when
// no weaker tier makes a claim" contract on
// SupplyChainImpactFindingResult.VersionResolutionCorroboration.
func supplyChainVersionResolutionCorroborationEntries(
	candidates []supplyChainVersionResolutionCandidate,
	winnerIndex int,
) []SupplyChainVersionResolutionCorroboration {
	entryCount := len(candidates)
	var winner supplyChainVersionResolutionCandidate
	if winnerIndex != -1 {
		entryCount--
		winner = candidates[winnerIndex]
	}
	if entryCount == 0 {
		return nil
	}
	entries := make([]SupplyChainVersionResolutionCorroboration, 0, entryCount)
	for i, c := range candidates {
		if i == winnerIndex {
			continue
		}
		agreement := supplyChainVersionResolutionNotComparable
		if winnerIndex != -1 {
			agreement = supplyChainVersionResolutionAgreement(c.value, c.axis, winner.value, winner.axis)
		}
		entries = append(entries, SupplyChainVersionResolutionCorroboration{
			Tier:            string(c.tier),
			DigestOrVersion: c.value,
			EvidenceKind:    c.kind,
			Agreement:       agreement,
		})
	}
	if len(entries) == 0 {
		return nil
	}
	return entries
}
