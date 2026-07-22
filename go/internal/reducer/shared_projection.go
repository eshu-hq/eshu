// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SharedProjectionDomain constants for the shared projection domains.
const (
	DomainRepoDependency      = "repo_dependency"
	DomainWorkloadDependency  = "workload_dependency"
	DomainCodeCalls           = "code_calls"
	DomainSQLRelationships    = "sql_relationships"
	DomainShellExec           = "shell_exec"
	DomainInheritanceEdges    = "inheritance_edges"
	DomainDocumentationEdges  = "documentation_edges"
	DomainRationaleEdges      = "rationale_edges"
	DomainDeployableUnitEdges = "deployable_unit_edges"
	// DomainHandlesRoute projects Function-[:HANDLES_ROUTE]->Endpoint edges from
	// parser-owned framework route handler bindings (#2721). Functions and
	// Endpoints are committed by different reducer domains with no ordering
	// guarantee, so the edge rides the ordering-safe shared-projection path the
	// same way CALLS edges do.
	DomainHandlesRoute = "handles_route"
	// DomainRunsIn projects Function-[:RUNS_IN]->Workload edges binding a route
	// handler Function to the deployed runtime it runs in (#2722). It scopes to the
	// same proven entrypoint Functions handles_route resolves and anchors each edge
	// through the Repository the handler belongs to: a handler binds to every
	// Workload its Repository DEFINES. Functions commit at canonical-nodes while
	// Workloads commit at workload-materialization, so the edge rides the same
	// ordering-safe shared-projection path and readiness gate as handles_route.
	DomainRunsIn = "runs_in"
	// DomainInvokesCloudAction projects Function-[:INVOKES_CLOUD_ACTION]->CloudAction
	// edges from Go AWS SDK call sites whose (service, method) maps to an action
	// in the closed CAN_PERFORM catalog (#2723). The Function is committed at
	// canonical-nodes; the CloudAction node is created inline by the same MERGE,
	// so unlike HANDLES_ROUTE there is no cross-acceptance-unit MATCH dependency.
	DomainInvokesCloudAction = "invokes_cloud_action"
	// DomainCodeownersOwnershipEdges projects Repository-[:DECLARES_CODEOWNER]->
	// CodeownerTeam edges from directly-emitted codeowners.ownership facts
	// (issue #5419 Phase 3). It is a distinct shared-projection domain from the
	// routed DomainCodeownersOwnership reducer domain that builds the intent
	// rows, mirroring the DomainDocumentationEdges/DomainDocumentationMaterialization
	// split: both the Repository and CodeownerTeam nodes are MERGEd inline by the
	// same edge write, so there is no cross-acceptance-unit MATCH dependency and
	// no readiness gate is required.
	DomainCodeownersOwnershipEdges = "codeowners_ownership_edges"
	// DomainSubmodulePinEdges projects Repository-[:PINS_SUBMODULE]->Repository
	// edges from directly-emitted submodule.pin facts (issue #5420 Phase 3). It
	// is a distinct shared-projection domain from the routed DomainSubmodulePin
	// reducer domain that builds the intent rows, mirroring the
	// DomainCodeownersOwnershipEdges/DomainCodeownersOwnership split. Both
	// endpoints are existing Repository nodes MERGEd inline by the same edge
	// write (no new node label, unlike codeowners' CodeownerTeam), so there is
	// no cross-acceptance-unit MATCH dependency and no readiness gate is
	// required.
	DomainSubmodulePinEdges = "submodule_pin_edges"
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
type SharedProjectionIntentRow struct {
	IntentID         string
	ProjectionDomain string
	PartitionKey     string
	ScopeID          string
	AcceptanceUnitID string
	RepositoryID     string
	SourceRunID      string
	GenerationID     string
	Payload          map[string]any
	CreatedAt        time.Time
	CompletedAt      *time.Time
}

// SharedProjectionIntentInput holds the parameters for building one
// deterministic shared projection intent row.
type SharedProjectionIntentInput struct {
	ProjectionDomain string
	PartitionKey     string
	// IdentityKey overrides the partition key only for deterministic intent ID
	// construction when several rows must share one stored partition key.
	IdentityKey      string
	ScopeID          string
	AcceptanceUnitID string
	RepositoryID     string
	SourceRunID      string
	GenerationID     string
	Payload          map[string]any
	CreatedAt        time.Time
}

// BuildSharedProjectionIntent builds one deterministic shared projection intent
// row. The intent ID is a SHA256 of the identity fields, matching the Python
// implementation exactly.
func BuildSharedProjectionIntent(input SharedProjectionIntentInput) SharedProjectionIntentRow {
	acceptanceUnitID := strings.TrimSpace(input.AcceptanceUnitID)
	if acceptanceUnitID == "" {
		acceptanceUnitID = strings.TrimSpace(input.RepositoryID)
	}
	identityPartitionKey := input.PartitionKey
	if strings.TrimSpace(input.IdentityKey) != "" {
		identityPartitionKey = strings.TrimSpace(input.IdentityKey)
	}

	intentID := stableIntentID(map[string]string{
		"acceptance_unit_id": acceptanceUnitID,
		"generation_id":      input.GenerationID,
		"partition_key":      identityPartitionKey,
		"projection_domain":  input.ProjectionDomain,
		"repository_id":      input.RepositoryID,
		"scope_id":           strings.TrimSpace(input.ScopeID),
		"source_run_id":      input.SourceRunID,
	})

	return SharedProjectionIntentRow{
		IntentID:         intentID,
		ProjectionDomain: input.ProjectionDomain,
		PartitionKey:     input.PartitionKey,
		ScopeID:          strings.TrimSpace(input.ScopeID),
		AcceptanceUnitID: acceptanceUnitID,
		RepositoryID:     input.RepositoryID,
		SourceRunID:      input.SourceRunID,
		GenerationID:     input.GenerationID,
		Payload:          input.Payload,
		CreatedAt:        input.CreatedAt,
		CompletedAt:      nil,
	}
}

// SharedProjectionAcceptanceKey identifies one authoritative freshness slice.
type SharedProjectionAcceptanceKey struct {
	ScopeID          string
	AcceptanceUnitID string
	SourceRunID      string
}

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

// AcceptanceKey returns the bounded-unit freshness key for the row.
func (row SharedProjectionIntentRow) AcceptanceKey() (SharedProjectionAcceptanceKey, bool) {
	scopeID := strings.TrimSpace(row.ScopeID)
	if scopeID == "" && row.Payload != nil {
		scopeID = strings.TrimSpace(anyToString(row.Payload["scope_id"]))
	}

	acceptanceUnitID := strings.TrimSpace(row.AcceptanceUnitID)
	if acceptanceUnitID == "" && row.Payload != nil {
		acceptanceUnitID = strings.TrimSpace(anyToString(row.Payload["acceptance_unit_id"]))
	}
	if acceptanceUnitID == "" {
		acceptanceUnitID = strings.TrimSpace(row.RepositoryID)
	}

	sourceRunID := strings.TrimSpace(row.SourceRunID)
	if scopeID == "" || acceptanceUnitID == "" || sourceRunID == "" {
		return SharedProjectionAcceptanceKey{}, false
	}

	return SharedProjectionAcceptanceKey{
		ScopeID:          scopeID,
		AcceptanceUnitID: acceptanceUnitID,
		SourceRunID:      sourceRunID,
	}, true
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

// stableIntentID computes a deterministic intent identifier matching the Python
// _stable_intent_id function. It serializes the identity dict as compact
// JSON with sorted keys: {"identity":{...sorted fields...}}
func stableIntentID(identity map[string]string) string {
	// Build the identity object with sorted keys. Since json.Marshal sorts
	// map keys by default in Go, this produces the same output as Python's
	// json.dumps(sort_keys=True, separators=(",", ":")).
	wrapper := map[string]any{
		"identity": identity,
	}

	payload, err := json.Marshal(wrapper)
	if err != nil {
		// Identity fields are plain strings; marshal cannot fail.
		panic(fmt.Sprintf("marshal identity: %v", err))
	}

	digest := sha256.Sum256(payload)
	return fmt.Sprintf("%x", digest)
}
