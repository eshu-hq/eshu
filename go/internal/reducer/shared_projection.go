// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
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

// allProjectionDomains is the complete set of reducer-owned shared/edge
// projection domains. It is the authoritative registry for enumerating these
// domains (AllDomains uses it for the capability surface inventory). It is a
// superset of sharedProjectionDomains, which is only the subset the shared
// partition worker drains: code_calls, repo_dependency, and deployable_unit_edges
// are driven by dedicated projection runners but are still reducer-owned domains
// that must appear in the inventory.
var allProjectionDomains = []Domain{
	DomainRepoDependency,
	DomainWorkloadDependency,
	DomainCodeCalls,
	DomainSQLRelationships,
	DomainShellExec,
	DomainInheritanceEdges,
	DomainDocumentationEdges,
	DomainRationaleEdges,
	DomainDeployableUnitEdges,
	DomainHandlesRoute,
	DomainRunsIn,
	DomainInvokesCloudAction,
	DomainCodeownersOwnershipEdges,
	DomainSubmodulePinEdges,
}

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

func sharedProjectionReadinessPhase(domain string) (GraphProjectionPhase, bool) {
	switch domain {
	case DomainCodeCalls, DomainInvokesCloudAction, DomainInheritanceEdges, DomainSQLRelationships, DomainShellExec, DomainRationaleEdges:
		// Functions commit at canonical-nodes. The CloudAction target is created
		// inline by the same INVOKES_CLOUD_ACTION MERGE, so canonical-nodes is the
		// only prerequisite phase: there is no cross-acceptance-unit dependency to
		// wait on the way HANDLES_ROUTE waits on Endpoint materialization (#2723).
		//
		// inheritance_edges connects :Class canonical code entities, which commit at
		// canonical-nodes too (#2867). It must NOT gate on semantic-nodes: that phase
		// is published only when the semantic-entity reducer runs, which does not
		// happen for every repo, so gating inheritance on it stalls projection
		// forever even though the class nodes already exist (confirmed by a remote
		// run: canonical_nodes_committed matched the intent's acceptance key exactly
		// while semantic_nodes_committed was never published).
		//
		// sql_relationships connects SqlTable/SqlColumn/SqlView/SqlFunction/
		// SqlTrigger/SqlIndex/SqlMigration nodes. Those are CANONICAL nodes: projector/canonical.go
		// maps the sql_* canonical entity kinds to those labels and the canonical node
		// writer commits them at canonical-nodes; the semantic-entity reducer never
		// emits any Sql* label. Gating sql on semantic-nodes was the same latent stall
		// as inheritance — that phase is only published when the semantic-entity
		// reducer runs, so a repo with SQL entities but no semantic entities would
		// defer its SQL edges forever even though the canonical Sql* nodes already
		// exist (#2868).
		//
		// rationale_edges connects an identity-only :Rationale node to a canonical
		// code entity (:Function|:Class|:Struct|:Interface|:TypeAlias|:Enum|:File).
		// The Rationale node is MERGEd inline by the EXPLAINS edge writer itself
		// (canonical_rationale_edges.go), not by the semantic-entity reducer, so the
		// only prerequisite is that the canonical target node exists — which commits
		// at canonical-nodes. Gating it on semantic-nodes was the same latent stall
		// as inheritance and sql: that phase is published only when the
		// semantic-entity reducer runs, so a repo with rationale comments but no
		// semantic entities would defer its EXPLAINS edges forever even though the
		// canonical code-entity nodes already exist (#2869).
		return GraphProjectionPhaseCanonicalNodesCommitted, true
	case DomainDocumentationEdges:
		return GraphProjectionPhaseSemanticNodesCommitted, true
	case DomainHandlesRoute, DomainRunsIn:
		// Endpoints (handles_route) and Workloads (runs_in) both commit at
		// workload-materialization; Functions commit earlier at canonical-nodes.
		// Gating on workload-materialization guarantees both MATCH targets exist
		// before the MERGE runs (#2721, #2722).
		return GraphProjectionPhaseWorkloadMaterialization, true
	default:
		return "", false
	}
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

func graphProjectionPhaseKeyForAcceptance(
	key SharedProjectionAcceptanceKey,
	generationID string,
	keyspace GraphProjectionKeyspace,
) (GraphProjectionPhaseKey, bool) {
	phaseKey := GraphProjectionPhaseKey{
		ScopeID:          strings.TrimSpace(key.ScopeID),
		AcceptanceUnitID: strings.TrimSpace(key.AcceptanceUnitID),
		SourceRunID:      strings.TrimSpace(key.SourceRunID),
		GenerationID:     strings.TrimSpace(generationID),
		Keyspace:         keyspace,
	}
	if err := phaseKey.Validate(); err != nil {
		return GraphProjectionPhaseKey{}, false
	}
	return phaseKey, true
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

// RowsForPartition returns intent rows whose partition key belongs to one
// worker partition.
func RowsForPartition(rows []SharedProjectionIntentRow, partitionID, partitionCount int) []SharedProjectionIntentRow {
	var result []SharedProjectionIntentRow
	for _, row := range rows {
		p, err := PartitionForKey(row.PartitionKey, partitionCount)
		if err != nil {
			continue
		}
		if p == partitionID {
			result = append(result, row)
		}
	}
	return result
}
