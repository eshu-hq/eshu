// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package worker

import (
	"strings"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// SharedProjectionReadinessPhase is the root spelling of the shared
// projection domain-to-readiness-phase mapping (moved here from the reducer
// root's sharedProjectionReadinessPhase, issue #6061). The code-call
// projection runner and selection files (still at the reducer root) also
// call it.
func SharedProjectionReadinessPhase(domain string) (gpphase.Phase, bool) {
	switch domain {
	case reducercontract.DomainCodeCalls, reducercontract.DomainInvokesCloudAction, reducercontract.DomainInheritanceEdges, reducercontract.DomainSQLRelationships, reducercontract.DomainShellExec, reducercontract.DomainRationaleEdges:
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
		return gpphase.PhaseCanonicalNodesCommitted, true
	case reducercontract.DomainDocumentationEdges:
		return gpphase.PhaseSemanticNodesCommitted, true
	case reducercontract.DomainHandlesRoute, reducercontract.DomainRunsIn:
		// Endpoints (handles_route) and Workloads (runs_in) both commit at
		// workload-materialization; Functions commit earlier at canonical-nodes.
		// Gating on workload-materialization guarantees both MATCH targets exist
		// before the MERGE runs (#2721, #2722).
		return gpphase.PhaseWorkloadMaterialization, true
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
func sharedProjectionReadinessKeyspace(domain string) gpphase.Keyspace {
	if domain == reducercontract.DomainHandlesRoute || domain == reducercontract.DomainRunsIn {
		return gpphase.KeyspaceServiceUID
	}
	return gpphase.KeyspaceCodeEntitiesUID
}

// GraphProjectionPhaseKeyForAcceptance is the root spelling of the
// acceptance-key-to-phase-key builder (moved here from the reducer root's
// graphProjectionPhaseKeyForAcceptance, issue #6061). The code-call
// projection selection file (still at the reducer root) also calls it.
func GraphProjectionPhaseKeyForAcceptance(
	key sharedintent.AcceptanceKey,
	generationID string,
	keyspace gpphase.Keyspace,
) (gpphase.PhaseKey, bool) {
	phaseKey := gpphase.PhaseKey{
		ScopeID:          strings.TrimSpace(key.ScopeID),
		AcceptanceUnitID: strings.TrimSpace(key.AcceptanceUnitID),
		SourceRunID:      strings.TrimSpace(key.SourceRunID),
		GenerationID:     strings.TrimSpace(generationID),
		Keyspace:         keyspace,
	}
	if err := phaseKey.Validate(); err != nil {
		return gpphase.PhaseKey{}, false
	}
	return phaseKey, true
}

func graphProjectionPhaseKeyForIntent(
	row sharedintent.Row,
	generationID string,
	keyspace gpphase.Keyspace,
) (gpphase.PhaseKey, bool) {
	acceptanceKey, ok := row.AcceptanceKey()
	if !ok {
		return gpphase.PhaseKey{}, false
	}
	return GraphProjectionPhaseKeyForAcceptance(acceptanceKey, generationID, keyspace)
}
