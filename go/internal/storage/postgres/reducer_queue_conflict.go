package postgres

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const (
	reducerConflictDomainScope         = "scope"
	reducerConflictDomainCodeGraph     = "code_graph"
	reducerConflictDomainPlatformGraph = "platform_graph"
)

// reducerConflictDomainKey returns the durable claim fence for one reducer
// intent. The key remains scope-scoped so newer generations cannot overtake
// older work for the same repo, while the domain separates graph families that
// do not share the same NornicDB write-hot spots.
func reducerConflictDomainKey(intent projector.ReducerIntent) (string, string) {
	scopeKey := strings.TrimSpace(intent.ScopeID)
	switch intent.Domain {
	case reducer.DomainCodeCallMaterialization,
		reducer.DomainSemanticEntityMaterialization,
		reducer.DomainSQLRelationshipMaterialization,
		reducer.DomainInheritanceMaterialization:
		return reducerConflictDomainCodeGraph, scopeKey
	case reducer.DomainWorkloadIdentity,
		reducer.DomainDeployableUnitCorrelation,
		reducer.DomainCloudAssetResolution,
		reducer.DomainDeploymentMapping,
		reducer.DomainWorkloadMaterialization:
		return reducerConflictDomainPlatformGraph, scopeKey
	default:
		return reducerConflictDomainScope, scopeKey
	}
}
