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
			},
			{
				Keyspace:  reducer.GraphProjectionKeyspaceServiceUID,
				PhaseName: reducer.GraphProjectionPhaseWorkloadMaterialization,
				Required:  true,
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
			{
				Keyspace:  reducer.GraphProjectionKeyspaceTerraformResourceUID,
				PhaseName: reducer.GraphProjectionPhaseCrossSourceAnchorReady,
				Required:  true,
			},
		},
	},
	scope.CollectorAWS: {
		CollectorKind: scope.CollectorAWS,
		CanonicalKeyspaces: []reducer.GraphProjectionKeyspace{
			reducer.GraphProjectionKeyspaceCloudResourceUID,
		},
		RequiredPhases: []PhaseRequirement{
			{
				Keyspace:  reducer.GraphProjectionKeyspaceCloudResourceUID,
				PhaseName: reducer.GraphProjectionPhaseCanonicalNodesCommitted,
				Required:  true,
			},
			{
				Keyspace:  reducer.GraphProjectionKeyspaceCloudResourceUID,
				PhaseName: reducer.GraphProjectionPhaseCrossSourceAnchorReady,
				Required:  true,
			},
		},
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
