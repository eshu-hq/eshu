// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// CollectorContract captures the accepted reducer-facing phase contract for one
// collector family.
type CollectorContract struct {
	CollectorKind      scope.CollectorKind
	CanonicalKeyspaces []gpphase.Keyspace
	RequiredPhases     []PhaseRequirement
}

var collectorContracts = map[scope.CollectorKind]CollectorContract{
	scope.CollectorGit: {
		CollectorKind: scope.CollectorGit,
		CanonicalKeyspaces: []gpphase.Keyspace{
			gpphase.KeyspaceCodeEntitiesUID,
			gpphase.KeyspaceDeployableUnitUID,
			gpphase.KeyspaceServiceUID,
		},
		RequiredPhases: []PhaseRequirement{
			{
				Keyspace:  gpphase.KeyspaceCodeEntitiesUID,
				PhaseName: gpphase.PhaseCanonicalNodesCommitted,
				Required:  true,
			},
			{
				Keyspace:  gpphase.KeyspaceCodeEntitiesUID,
				PhaseName: gpphase.PhaseSemanticNodesCommitted,
				Required:  true,
			},
			{
				Keyspace:  gpphase.KeyspaceDeployableUnitUID,
				PhaseName: gpphase.PhaseDeployableUnitCorrelation,
				Required:  true,
			},
			{
				Keyspace:  gpphase.KeyspaceServiceUID,
				PhaseName: gpphase.PhaseCanonicalNodesCommitted,
				Required:  true,
			},
			{
				Keyspace:  gpphase.KeyspaceServiceUID,
				PhaseName: gpphase.PhaseDeploymentMapping,
				Required:  true,
				// DomainDeploymentMapping is the sole reducer domain that
				// publishes this phase (reducer/platformfam/platform_materialization.go),
				// so a
				// terminal dead-letter for it can be attributed directly
				// (#4459).
				DeadLetterDomain: reducer.DomainDeploymentMapping,
			},
			{
				Keyspace:  gpphase.KeyspaceServiceUID,
				PhaseName: gpphase.PhaseWorkloadMaterialization,
				Required:  true,
				// DomainWorkloadMaterialization is the sole reducer domain
				// that publishes this phase
				// (workload_materialization_handler.go), so a terminal
				// dead-letter for it can be attributed directly (#4459).
				DeadLetterDomain: reducer.DomainWorkloadMaterialization,
			},
		},
	},
	scope.CollectorTerraformState: {
		CollectorKind: scope.CollectorTerraformState,
		CanonicalKeyspaces: []gpphase.Keyspace{
			gpphase.KeyspaceTerraformResourceUID,
			gpphase.KeyspaceTerraformModuleUID,
		},
		RequiredPhases: []PhaseRequirement{
			{
				Keyspace:  gpphase.KeyspaceTerraformResourceUID,
				PhaseName: gpphase.PhaseCanonicalNodesCommitted,
				Required:  true,
			},
			{
				Keyspace:  gpphase.KeyspaceTerraformModuleUID,
				PhaseName: gpphase.PhaseCanonicalNodesCommitted,
				Required:  true,
			},
		},
	},
	scope.CollectorAWS: {
		// AWS currently publishes fact-backed scan and drift read models. It
		// has no cloud-resource graph-readiness phase requirements because no
		// live runtime publishes its cloud-resource projection phase rows yet.
		CollectorKind:      scope.CollectorAWS,
		CanonicalKeyspaces: nil,
		RequiredPhases:     nil,
	},
	scope.CollectorGCP: {
		// GCP is a known collector contract for fixture-backed and gated live
		// cloud facts. It has no graph-readiness phase requirements until the
		// cloud-resource graph writer and anchor publisher are implemented.
		CollectorKind:      scope.CollectorGCP,
		CanonicalKeyspaces: nil,
		RequiredPhases:     nil,
	},
	scope.CollectorAzure: {
		// Azure is a known collector contract for fixture-backed and gated live
		// cloud facts. It has no graph-readiness phase requirements until the
		// cloud-resource graph writer and anchor publisher are implemented,
		// matching the GCP cloud contract.
		CollectorKind:      scope.CollectorAzure,
		CanonicalKeyspaces: nil,
		RequiredPhases:     nil,
	},
	scope.CollectorWebhook: {
		CollectorKind: scope.CollectorWebhook,
		CanonicalKeyspaces: []gpphase.Keyspace{
			gpphase.KeyspaceWebhookEventUID,
		},
		RequiredPhases: []PhaseRequirement{
			{
				Keyspace:  gpphase.KeyspaceWebhookEventUID,
				PhaseName: gpphase.PhaseCanonicalNodesCommitted,
				Required:  true,
			},
			{
				Keyspace:  gpphase.KeyspaceWebhookEventUID,
				PhaseName: gpphase.PhaseCrossSourceAnchorReady,
				Required:  true,
			},
		},
	},
	scope.CollectorDocumentation: {
		CollectorKind:      scope.CollectorDocumentation,
		CanonicalKeyspaces: nil,
		RequiredPhases:     nil,
	},
	scope.CollectorOCIRegistry: {
		CollectorKind:      scope.CollectorOCIRegistry,
		CanonicalKeyspaces: nil,
		RequiredPhases:     nil,
	},
	scope.CollectorPackageRegistry: {
		CollectorKind:      scope.CollectorPackageRegistry,
		CanonicalKeyspaces: nil,
		RequiredPhases:     nil,
	},
	scope.CollectorVulnerabilityIntelligence: {
		CollectorKind:      scope.CollectorVulnerabilityIntelligence,
		CanonicalKeyspaces: nil,
		RequiredPhases:     nil,
	},
	scope.CollectorSBOMAttestation: {
		CollectorKind:      scope.CollectorSBOMAttestation,
		CanonicalKeyspaces: nil,
		RequiredPhases:     nil,
	},
	scope.CollectorSecurityAlert: {
		CollectorKind:      scope.CollectorSecurityAlert,
		CanonicalKeyspaces: nil,
		RequiredPhases:     nil,
	},
	scope.CollectorCICDRun: {
		CollectorKind:      scope.CollectorCICDRun,
		CanonicalKeyspaces: nil,
		RequiredPhases:     nil,
	},
	scope.CollectorPagerDuty: {
		CollectorKind:      scope.CollectorPagerDuty,
		CanonicalKeyspaces: nil,
		RequiredPhases:     nil,
	},
	scope.CollectorJira: {
		CollectorKind:      scope.CollectorJira,
		CanonicalKeyspaces: nil,
		RequiredPhases:     nil,
	},
	scope.CollectorScannerWorker: {
		CollectorKind:      scope.CollectorScannerWorker,
		CanonicalKeyspaces: nil,
		RequiredPhases:     nil,
	},
	scope.CollectorVaultLive: {
		CollectorKind:      scope.CollectorVaultLive,
		CanonicalKeyspaces: nil,
		RequiredPhases:     nil,
	},
}

// CollectorContractFor returns the accepted reducer-facing contract for one
// collector family.
func CollectorContractFor(kind scope.CollectorKind) (CollectorContract, bool) {
	contract, ok := collectorContracts[kind]
	if !ok {
		return CollectorContract{}, false
	}
	contract.CanonicalKeyspaces = slices.Clone(contract.CanonicalKeyspaces)
	contract.RequiredPhases = slices.Clone(contract.RequiredPhases)
	return contract, true
}

// CanonicalKeyspacesForCollector returns the accepted canonical keyspaces for
// one collector family.
func CanonicalKeyspacesForCollector(kind scope.CollectorKind) []gpphase.Keyspace {
	contract, ok := CollectorContractFor(kind)
	if !ok {
		return nil
	}
	return contract.CanonicalKeyspaces
}

// RequiredPhasesForCollector returns the currently required reducer-owned
// phases for the supplied collector family.
func RequiredPhasesForCollector(kind scope.CollectorKind) []PhaseRequirement {
	contract, ok := CollectorContractFor(kind)
	if !ok {
		return nil
	}
	return contract.RequiredPhases
}

// CollectorKindsRequiringPhase returns, in sorted order, every collector
// family whose contract requires the given (keyspace, phase). The
// canonical-code quiescence gate uses it with (code_entities_uid,
// canonical_nodes_committed) to decide which scopes can hold the code-call
// lane (#7133): only a scope whose collector is contracted to publish that
// phase can ever release the gate, so no other scope may hold it.
func CollectorKindsRequiringPhase(
	keyspace gpphase.Keyspace,
	phase gpphase.Phase,
) []scope.CollectorKind {
	kinds := make([]scope.CollectorKind, 0, 1)
	for kind, contract := range collectorContracts {
		for _, requirement := range contract.RequiredPhases {
			if requirement.Required && requirement.Keyspace == keyspace && requirement.PhaseName == phase {
				kinds = append(kinds, kind)
				break
			}
		}
	}
	slices.Sort(kinds)
	return kinds
}
