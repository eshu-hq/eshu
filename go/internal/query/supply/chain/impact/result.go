// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

// BuildFindingResult converts row into its API response shape, deriving the
// missing-evidence list, deployment-truth tier, and version-resolution tier
// and corroboration from the row's existing fields.
func BuildFindingResult(row *FindingRow) packet.FindingResult {
	result := packet.FindingResult(*row)
	result.MissingEvidence = normalizedSupplyChainImpactMissingEvidence(row)
	result.DeploymentTruthTier = string(supplyChainDeploymentTruthTier(row))
	result.VersionResolutionTier, result.VersionResolutionCorroboration = supplyChainVersionResolution(row)
	return result
}

// supplyChainDeploymentTruthTier classifies the strongest deployment
// evidence tier available from the finding row's existing fields, using the
// shared truth.DeploymentTruthTier vocabulary so this surface applies the
// same tiers as trace_deployment_chain and service story.
//
// The three deployment-evidence classes are now distinct (#5452):
//
//   - runtime_confirmed: a live cloud resource (running ECS task /
//     image-package Lambda) or current authorized Kubernetes workload actually
//     runs the finding's subject digest — surfaced at read time as
//     CloudRuntimeResourceRefs or KubernetesRuntimeWorkloadRefs.
//   - provenance_ci_declared: a cicd_run_correlation matched the finding (CI
//     declared the deployment). The match may be on the finding's digest or
//     image reference, or on repository plus environment plus an operational
//     anchor — the weaker branch still counts as CI-declared evidence here even
//     though it does not on its own raise runtime_reachability to
//     deployed_image (#5426). Before #5452 this collapsed into config_only
//     because no runtime tier existed on this surface.
//   - config_only: only config-materialized deployment anchors or config
//     environments exist, with no runtime or CI-declared evidence.
func supplyChainDeploymentTruthTier(row *FindingRow) truth.DeploymentTruthTier {
	if len(row.CloudRuntimeResourceRefs) > 0 || len(row.KubernetesRuntimeWorkloadRefs) > 0 {
		return truth.ClassifyDeploymentTruthTier(true, false, false, false)
	}
	if rowHasCIDeclaredDeploymentEvidence(row) {
		return truth.TierProvenanceCIDeclared
	}
	hasDeploymentAnchor := len(row.WorkloadIDs) > 0 ||
		len(row.DeploymentIDs) > 0 ||
		row.ImageRef != "" ||
		row.SubjectDigest != ""
	hasConfigEnvs := len(row.Environments) > 0
	return truth.ClassifyDeploymentTruthTier(
		false, // hasLiveEvidence: no runtime-observed cloud resource
		false, // instances not surfaced in finding row
		hasDeploymentAnchor,
		hasConfigEnvs,
	)
}

// rowHasCIDeclaredDeploymentEvidence reports whether the finding row carries a
// cicd_run_correlation deployment hop — the reducer appends this evidence-path
// entry whenever a CI/CD run correlation matched the finding, so it is the
// row-level signal that a deployment was CI-declared.
//
// A match on the finding's digest or image reference is not required: a
// correlation that links only through repository plus environment plus an
// operational anchor also appends this hop, and deliberately so (#5426). Such a
// deployment does not on its own raise runtime_reachability to deployed_image,
// but it is still CI-declared deployment evidence and holds the row at
// deployment_truth_tier=provenance_ci_declared rather than config_only.
func rowHasCIDeclaredDeploymentEvidence(row *FindingRow) bool {
	for _, hop := range row.EvidencePath {
		if hop == cicdRunCorrelationFactKind {
			return true
		}
	}
	return false
}

func normalizedSupplyChainImpactMissingEvidence(row *FindingRow) []string {
	if supplyChainImpactMissingEvidenceIsNormalized(row) {
		return row.MissingEvidence
	}

	missing := make([]string, 0, len(row.MissingEvidence))
	hasServiceCatalogEvidence := rowHasServiceCatalogEvidence(row)
	hasResolvedServiceCatalogAnchor := rowHasResolvedServiceCatalogAnchor(row)
	for _, reason := range row.MissingEvidence {
		if reason == packet.ServiceCatalogAnchorMissingReason && hasResolvedServiceCatalogAnchor {
			continue
		}
		if reason == packet.ServiceCatalogCorrelationMissingReason && hasServiceCatalogEvidence {
			if hasResolvedServiceCatalogAnchor {
				continue
			}
			reason = packet.ServiceCatalogAnchorMissingReason
		}
		missing = append(missing, reason)
	}
	return explanationUniqueStrings(missing)
}

// supplyChainImpactMissingEvidenceIsNormalized reports whether the existing
// slice is already the exact trimmed, unique, sorted output and no
// service-catalog reason needs rewriting. Reusing that slice avoids an
// otherwise unconditional per-finding allocation; rows and results already
// share the other immutable decoded slices.
func supplyChainImpactMissingEvidenceIsNormalized(row *FindingRow) bool {
	hasServiceCatalogEvidence := rowHasServiceCatalogEvidence(row)
	hasResolvedServiceCatalogAnchor := rowHasResolvedServiceCatalogAnchor(row)
	for i, reason := range row.MissingEvidence {
		if reason == packet.ServiceCatalogAnchorMissingReason && hasResolvedServiceCatalogAnchor {
			return false
		}
		if reason == packet.ServiceCatalogCorrelationMissingReason && hasServiceCatalogEvidence {
			return false
		}
		if reason == "" || strings.TrimSpace(reason) != reason {
			return false
		}
		if i > 0 && row.MissingEvidence[i-1] >= reason {
			return false
		}
	}
	return true
}

func rowHasServiceCatalogEvidence(row *FindingRow) bool {
	for _, hop := range row.EvidencePath {
		if hop == serviceCatalogCorrelationFactKind {
			return true
		}
	}
	correlationFactIDPrefix := serviceCatalogCorrelationFactKind + ":"
	for _, factID := range row.EvidenceFactIDs {
		if strings.HasPrefix(factID, correlationFactIDPrefix) {
			return true
		}
	}
	return false
}

func rowHasResolvedServiceCatalogAnchor(row *FindingRow) bool {
	if len(row.ServiceIDs) > 0 {
		return true
	}
	return len(row.WorkloadIDs) > 0 && len(row.CatalogEntityRefs) > 0
}
