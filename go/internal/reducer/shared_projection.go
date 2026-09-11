// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	worker "github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// SharedProjectionDomain constants for the shared projection domains.
const (
	DomainRepoDependency      = reducercontract.DomainRepoDependency
	DomainWorkloadDependency  = reducercontract.DomainWorkloadDependency
	DomainCodeCalls           = reducercontract.DomainCodeCalls
	DomainSQLRelationships    = reducercontract.DomainSQLRelationships
	DomainShellExec           = reducercontract.DomainShellExec
	DomainInheritanceEdges    = reducercontract.DomainInheritanceEdges
	DomainDocumentationEdges  = reducercontract.DomainDocumentationEdges
	DomainRationaleEdges      = reducercontract.DomainRationaleEdges
	DomainDeployableUnitEdges = reducercontract.DomainDeployableUnitEdges
	// DomainHandlesRoute projects Function-[:HANDLES_ROUTE]->Endpoint edges from
	// parser-owned framework route handler bindings (#2721). Functions and
	// Endpoints are committed by different reducer domains with no ordering
	// guarantee, so the edge rides the ordering-safe shared-projection path the
	// same way CALLS edges do.
	DomainHandlesRoute = reducercontract.DomainHandlesRoute
	// DomainRunsIn projects Function-[:RUNS_IN]->Workload edges binding a route
	// handler Function to the deployed runtime it runs in (#2722). It scopes to the
	// same proven entrypoint Functions handles_route resolves and anchors each edge
	// through the Repository the handler belongs to: a handler binds to every
	// Workload its Repository DEFINES. Functions commit at canonical-nodes while
	// Workloads commit at workload-materialization, so the edge rides the same
	// ordering-safe shared-projection path and readiness gate as handles_route.
	DomainRunsIn = reducercontract.DomainRunsIn
	// DomainInvokesCloudAction projects Function-[:INVOKES_CLOUD_ACTION]->CloudAction
	// edges from Go AWS SDK call sites whose (service, method) maps to an action
	// in the closed CAN_PERFORM catalog (#2723). The Function is committed at
	// canonical-nodes; the CloudAction node is created inline by the same MERGE,
	// so unlike HANDLES_ROUTE there is no cross-acceptance-unit MATCH dependency.
	DomainInvokesCloudAction = reducercontract.DomainInvokesCloudAction
	// DomainCodeownersOwnershipEdges projects Repository-[:DECLARES_CODEOWNER]->
	// CodeownerTeam edges from directly-emitted codeowners.ownership facts
	// (issue #5419 Phase 3). It is a distinct shared-projection domain from the
	// routed DomainCodeownersOwnership reducer domain that builds the intent
	// rows, mirroring the DomainDocumentationEdges/DomainDocumentationMaterialization
	// split: both the Repository and CodeownerTeam nodes are MERGEd inline by the
	// same edge write, so there is no cross-acceptance-unit MATCH dependency and
	// no readiness gate is required.
	DomainCodeownersOwnershipEdges = reducercontract.DomainCodeownersOwnershipEdges
	// DomainSubmodulePinEdges projects Repository-[:PINS_SUBMODULE]->Repository
	// edges from directly-emitted submodule.pin facts (issue #5420 Phase 3). It
	// is a distinct shared-projection domain from the routed DomainSubmodulePin
	// reducer domain that builds the intent rows, mirroring the
	// DomainCodeownersOwnershipEdges/DomainCodeownersOwnership split. Both
	// endpoints are existing Repository nodes MERGEd inline by the same edge
	// write (no new node label, unlike codeowners' CodeownerTeam), so there is
	// no cross-acceptance-unit MATCH dependency and no readiness gate is
	// required.
	DomainSubmodulePinEdges = reducercontract.DomainSubmodulePinEdges
)

// allProjectionDomains forwards to [reducercontract.ProjectionDomains].
var allProjectionDomains = reducercontract.ProjectionDomains()

// SharedProjectionIntentRow is one durable shared-domain projection intent.
// Alias for [sharedintent.Row]: the shape, its deterministic builder, and its
// freshness-key method live in that leaf so a domain family can construct and
// read an intent without importing this package.
type SharedProjectionIntentRow = sharedintent.Row

// SharedProjectionIntentInput holds the parameters for building one
// deterministic shared projection intent row. Alias for [sharedintent.Input].
type SharedProjectionIntentInput = sharedintent.Input

// BuildSharedProjectionIntent forwards to [sharedintent.Build].
func BuildSharedProjectionIntent(input SharedProjectionIntentInput) SharedProjectionIntentRow {
	return sharedintent.Build(input)
}

// SharedProjectionAcceptanceKey identifies one authoritative freshness slice.
// Alias for [sharedintent.AcceptanceKey].
type SharedProjectionAcceptanceKey = sharedintent.AcceptanceKey

// sharedProjectionReadinessPhase forwards to
// [worker.ReadinessPhase].
func sharedProjectionReadinessPhase(domain string) (GraphProjectionPhase, bool) {
	return worker.ReadinessPhase(domain)
}

// sharedProjectionReadinessKeyspace returns the graph-projection keyspace whose
// readiness phase gates a domain's edge projection. The generic shared
// projection worker reads this so each domain's readiness lookup targets the
// keyspace its prerequisite phase was published under: code_calls and the
// semantic edge domains key on code_entities_uid, while handles_route and
// runs_in key on service_uid because the workload_materialization phase that
// commits Endpoint and Workload nodes is published under the service identity
// keyspace (#2721, #2722). A wrong keyspace here would make the readiness lookup
// miss forever and silently drop every edge.
func sharedProjectionReadinessKeyspace(domain string) GraphProjectionKeyspace {
	if domain == DomainHandlesRoute || domain == DomainRunsIn {
		return GraphProjectionKeyspaceServiceUID
	}
	return GraphProjectionKeyspaceCodeEntitiesUID
}

// graphProjectionPhaseKeyForAcceptance forwards to
// [worker.GraphProjectionPhaseKeyForAcceptance].
func graphProjectionPhaseKeyForAcceptance(
	key SharedProjectionAcceptanceKey,
	generationID string,
	keyspace GraphProjectionKeyspace,
) (GraphProjectionPhaseKey, bool) {
	return worker.GraphProjectionPhaseKeyForAcceptance(key, generationID, keyspace)
}

func graphProjectionPhaseKeyForIntent(
	row SharedProjectionIntentRow,
	generationID string,
	keyspace GraphProjectionKeyspace,
) (GraphProjectionPhaseKey, bool) {
	acceptanceKey, ok := row.AcceptanceKey()
	if !ok {
		return GraphProjectionPhaseKey{}, false
	}
	return graphProjectionPhaseKeyForAcceptance(acceptanceKey, generationID, keyspace)
}

// RowsForPartition forwards to [sharedintent.RowsForPartition].
func RowsForPartition(rows []SharedProjectionIntentRow, partitionID, partitionCount int) []SharedProjectionIntentRow {
	return sharedintent.RowsForPartition(rows, partitionID, partitionCount)
}
