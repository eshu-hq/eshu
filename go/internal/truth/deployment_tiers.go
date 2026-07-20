// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package truth

import (
	"fmt"
	"strings"
)

// DeploymentTruthTier classifies the strongest class of deployment evidence
// available for a traced workload, in strict descending rank order. It
// replaces ad-hoc confidence-reason strings with a closed, typed vocabulary
// that consumers (trace_deployment_chain, supply_chain_impact, service story)
// read through the same shared constants.
type DeploymentTruthTier string

const (
	// TierRuntimeConfirmed is the strongest tier: a live observation (such
	// as an exact kubernetes_live correlation producing a RUNS_IMAGE edge,
	// or a cloud-observed instance) confirms the workload is running in a
	// measurable environment.
	TierRuntimeConfirmed DeploymentTruthTier = "runtime_confirmed"

	// TierProvenanceCIDeclared represents CI/CD or supply-chain provenance
	// that declares a deployment (e.g. ci_cd run correlation, attestation).
	TierProvenanceCIDeclared DeploymentTruthTier = "provenance_ci_declared"

	// TierDeclaredRef represents a named ref (branch/SHA) declared as
	// deployed through a future DEPLOYS_REF edge (#5393). Define the
	// constant now so consumers are forward-compatible; the evidence
	// source is not yet wired.
	TierDeclaredRef DeploymentTruthTier = "declared_ref"

	// TierConfigOnly is the weakest tier: only config materialization
	// evidence (config-derived WorkloadInstance, deployment sources, or
	// config environments) exists, with no live or CI-declared evidence.
	TierConfigOnly DeploymentTruthTier = "config_only"
)

// rank returns the integer rank of the tier. Higher values represent
// stronger evidence classes. The ordering is:
//
//	runtime_confirmed (4) > provenance_ci_declared (3) > declared_ref (2) > config_only (1)
func (tier DeploymentTruthTier) rank() int {
	switch tier {
	case TierRuntimeConfirmed:
		return 4
	case TierProvenanceCIDeclared:
		return 3
	case TierDeclaredRef:
		return 2
	case TierConfigOnly:
		return 1
	default:
		return 0
	}
}

// Rank returns the integer rank of the tier. Higher values represent
// stronger evidence classes. Unknown tiers return 0.
func (tier DeploymentTruthTier) Rank() int {
	return tier.rank()
}

// Compare reports whether the receiver is stronger (+1), weaker (-1), or
// equal (0) relative to the argument tier. Unknown tiers are treated as
// weaker than any known tier.
func (tier DeploymentTruthTier) Compare(other DeploymentTruthTier) int {
	left := tier.rank()
	right := other.rank()
	switch {
	case left > right:
		return 1
	case left < right:
		return -1
	default:
		return 0
	}
}

// ParseDeploymentTruthTier converts a raw string into a known
// DeploymentTruthTier. Leading and trailing whitespace is trimmed before
// matching. Unknown values return an error.
func ParseDeploymentTruthTier(raw string) (DeploymentTruthTier, error) {
	tier := DeploymentTruthTier(strings.TrimSpace(raw))
	switch tier {
	case TierRuntimeConfirmed, TierProvenanceCIDeclared, TierDeclaredRef, TierConfigOnly:
		return tier, nil
	default:
		return "", fmt.Errorf("unknown deployment truth tier %q", raw)
	}
}

// AllDeploymentTruthTiers returns every known tier in strict descending
// rank order (strongest first). The slice is deterministic and exhaustively
// covers the closed vocabulary.
func AllDeploymentTruthTiers() []DeploymentTruthTier {
	return []DeploymentTruthTier{
		TierRuntimeConfirmed,
		TierProvenanceCIDeclared,
		TierDeclaredRef,
		TierConfigOnly,
	}
}

// ClassifyDeploymentTruthTier determines the strongest deployment truth
// tier from the available evidence signals. It is the single shared
// classification helper used by trace_deployment_chain,
// supply_chain_impact, and service story so the tier vocabulary is applied
// consistently across all surfaces.
//
// Evidence is checked in descending strength order:
//
//   - hasLiveEvidence (runtime_confirmed): an exact kubernetes_live
//     RUNS_IMAGE edge or equivalent live observation binds to the workload.
//   - hasInstances (config_only): config-materialized WorkloadInstance
//     rows exist. Despite the legacy "materialized_runtime_instances"
//     confidence reason string, these are config-derived, not live.
//   - hasDeploymentSources (config_only): DEPLOYMENT_SOURCE or DEPLOYS_FROM
//     edges connect the workload to a repository.
//   - hasConfigEnvironments (config_only): config files declare
//     environments for the workload.
//
// When none of the signals are present the function returns the zero value
// (""), meaning no classified tier.
func ClassifyDeploymentTruthTier(
	hasLiveEvidence bool,
	hasInstances bool,
	hasDeploymentSources bool,
	hasConfigEnvironments bool,
) DeploymentTruthTier {
	if hasLiveEvidence {
		return TierRuntimeConfirmed
	}
	if hasInstances || hasDeploymentSources || hasConfigEnvironments {
		return TierConfigOnly
	}
	return ""
}
