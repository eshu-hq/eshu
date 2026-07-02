// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// CollectorContract captures the accepted reducer-facing phase contract for one
// collector family.
type CollectorContract struct {
	CollectorKind      scope.CollectorKind
	CanonicalKeyspaces []reducer.GraphProjectionKeyspace
	RequiredPhases     []PhaseRequirement
}

var collectorContracts = map[scope.CollectorKind]CollectorContract{
	scope.CollectorGit: {
		CollectorKind: scope.CollectorGit,
		CanonicalKeyspaces: []reducer.GraphProjectionKeyspace{
			reducer.GraphProjectionKeyspaceCodeEntitiesUID,
			reducer.GraphProjectionKeyspaceDeployableUnitUID,
			reducer.GraphProjectionKeyspaceServiceUID,
		},
		RequiredPhases: []PhaseRequirement{
			{
				Keyspace:  reducer.GraphProjectionKeyspaceCodeEntitiesUID,
				PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
				Required:  true,
			},
			{
				Keyspace:  reducer.GraphProjectionKeyspaceCodeEntitiesUID,
				PhaseName: reducer.GraphProjectionPhaseSemanticNodesCommitted,
				Required:  true,
			},
			{
				Keyspace:  reducer.GraphProjectionKeyspaceDeployableUnitUID,
				PhaseName: reducer.GraphProjectionPhaseDeployableUnitCorrelation,
				Required:  true,
			},
			{
				Keyspace:  reducer.GraphProjectionKeyspaceServiceUID,
				PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
				Required:  true,
			},
			{
				Keyspace:  reducer.GraphProjectionKeyspaceServiceUID,
				PhaseName: reducer.GraphProjectionPhaseDeploymentMapping,
				Required:  true,
				// DomainDeploymentMapping is the sole reducer domain that
				// publishes this phase (platform_materialization.go), so a
				// terminal dead-letter for it can be attributed directly
				// (#4459).
				DeadLetterDomain: reducer.DomainDeploymentMapping,
			},
			{
				Keyspace:  reducer.GraphProjectionKeyspaceServiceUID,
				PhaseName: reducer.GraphProjectionPhaseWorkloadMaterialization,
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
		CanonicalKeyspaces: []reducer.GraphProjectionKeyspace{
			reducer.GraphProjectionKeyspaceTerraformResourceUID,
			reducer.GraphProjectionKeyspaceTerraformModuleUID,
		},
		RequiredPhases: []PhaseRequirement{
			{
				Keyspace:  reducer.GraphProjectionKeyspaceTerraformResourceUID,
				PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
				Required:  true,
			},
			{
				Keyspace:  reducer.GraphProjectionKeyspaceTerraformModuleUID,
				PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
				Required:  true,
			},
		},
	},
	scope.CollectorAWS: {
		// AWS currently publishes fact-backed scan and drift read models. The
		// cloud-resource graph projection contract is scaffolded in
		// internal/reducer/aws, but no live runtime publishes its phase rows yet.
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
		CanonicalKeyspaces: []reducer.GraphProjectionKeyspace{
			reducer.GraphProjectionKeyspaceWebhookEventUID,
		},
		RequiredPhases: []PhaseRequirement{
			{
				Keyspace:  reducer.GraphProjectionKeyspaceWebhookEventUID,
				PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
				Required:  true,
			},
			{
				Keyspace:  reducer.GraphProjectionKeyspaceWebhookEventUID,
				PhaseName: reducer.GraphProjectionPhaseCrossSourceAnchorReady,
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
func CanonicalKeyspacesForCollector(kind scope.CollectorKind) []reducer.GraphProjectionKeyspace {
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
