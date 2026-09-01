// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

// GraphProjectionKeyspace identifies the concrete conflict domain for graph
// projection coordination.
type GraphProjectionKeyspace = gpphase.Keyspace

// Keyspace identity constants, aliased from [gpphase] so every existing
// caller in this package keeps working unchanged (issue #6061). See
// [gpphase.Keyspace] for what each domain means.
const (
	GraphProjectionKeyspaceCodeEntitiesUID          = gpphase.KeyspaceCodeEntitiesUID
	GraphProjectionKeyspaceServiceUID               = gpphase.KeyspaceServiceUID
	GraphProjectionKeyspaceDeployableUnitUID        = gpphase.KeyspaceDeployableUnitUID
	GraphProjectionKeyspaceTerraformResourceUID     = gpphase.KeyspaceTerraformResourceUID
	GraphProjectionKeyspaceTerraformModuleUID       = gpphase.KeyspaceTerraformModuleUID
	GraphProjectionKeyspaceCloudResourceUID         = gpphase.KeyspaceCloudResourceUID
	GraphProjectionKeyspaceKubernetesWorkloadUID    = gpphase.KeyspaceKubernetesWorkloadUID
	GraphProjectionKeyspaceSecurityGroupEndpointUID = gpphase.KeyspaceSecurityGroupEndpointUID
	GraphProjectionKeyspaceSecurityGroupRuleUID     = gpphase.KeyspaceSecurityGroupRuleUID
	GraphProjectionKeyspaceWebhookEventUID          = gpphase.KeyspaceWebhookEventUID
	GraphProjectionKeyspaceCrossRepoEvidence        = gpphase.KeyspaceCrossRepoEvidence
	GraphProjectionKeyspaceAPIEndpointRepoPath      = gpphase.KeyspaceAPIEndpointRepoPath
	GraphProjectionKeyspaceRepoWorkloadPresence     = gpphase.KeyspaceRepoWorkloadPresence
)

// GraphProjectionPhase identifies one durable readiness milestone for a graph
// projection keyspace.
type GraphProjectionPhase = gpphase.Phase

// Phase identity constants, aliased from [gpphase] so every existing caller
// in this package keeps working unchanged (issue #6061). See [gpphase.Phase]
// for what each milestone means.
const (
	GraphProjectionPhaseCanonicalNodesCommitted   = gpphase.PhaseCanonicalNodesCommitted
	GraphProjectionPhaseDeployableUnitCorrelation = gpphase.PhaseDeployableUnitCorrelation
	GraphProjectionPhaseSemanticNodesCommitted    = gpphase.PhaseSemanticNodesCommitted
	GraphProjectionPhaseBackwardEvidenceCommitted = gpphase.PhaseBackwardEvidenceCommitted
	GraphProjectionPhaseDeploymentMapping         = gpphase.PhaseDeploymentMapping
	GraphProjectionPhaseWorkloadMaterialization   = gpphase.PhaseWorkloadMaterialization
	GraphProjectionPhaseCrossSourceAnchorReady    = gpphase.PhaseCrossSourceAnchorReady
)

// GraphProjectionPhaseKey identifies one bounded graph-write readiness slice.
type GraphProjectionPhaseKey = gpphase.PhaseKey

// GraphProjectionPhaseState captures one durable readiness publication.
type GraphProjectionPhaseState struct {
	Key         GraphProjectionPhaseKey
	Phase       GraphProjectionPhase
	CommittedAt time.Time
	UpdatedAt   time.Time
}

// GraphProjectionReadinessLookup reports whether a bounded readiness slice
// has reached the requested phase. It returns (ready, found).
type GraphProjectionReadinessLookup = gpphase.ReadinessLookup

// GraphProjectionReadinessPrefetch resolves readiness for a bounded set of
// keys and returns an in-memory lookup closure for the current cycle.
type GraphProjectionReadinessPrefetch = gpphase.ReadinessPrefetch

// GraphProjectionPhasePublisher persists graph-readiness publications.
type GraphProjectionPhasePublisher interface {
	PublishGraphProjectionPhases(context.Context, []GraphProjectionPhaseState) error
}

// EndpointPresenceRow records that one endpoint node uid is committed in the
// canonical graph, keyed by its bounded keyspace. It is the uid-exact,
// cross-scope readiness primitive (issue #1380, ADR #1314 §6/§8): a presence row
// proves the specific node X is committed, which the same-scope/same-generation
// graph_projection_phase_state gate cannot express. CommittedAt is the node
// materializer's commit instant; an empty value defers to the store's clock.
type EndpointPresenceRow struct {
	Keyspace GraphProjectionKeyspace
	UID      string
	ScopeID  string
	// RepoID and SourceGeneration are written only by the symbol→runtime presence
	// publishers (#2842) so stale rows can be retracted per repo when a generation
	// re-materializes — the synthesized uid is a hash (#2844) and no longer carries
	// the repo_id. They are blank for the uid-exact #1380 presence rows, which are
	// retracted by scope/node lifecycle instead. Both are NUL-free (a repo_id and a
	// generation id contain no 0x00), so they are safe in the Postgres text columns.
	RepoID           string
	SourceGeneration string
	CommittedAt      time.Time
}

// EndpointPresenceWriter records and retracts endpoint-node presence. The
// CloudResource and KubernetesWorkload node materializers call Upsert with one
// row per committed node uid (idempotent: re-upserting the same (keyspace, uid)
// converges on one row), and RetractScope removes a scope's presence rows so a
// node retract removes its presence. Implementations MUST be safe under
// concurrent materializer workers (the upsert is ON CONFLICT idempotent); the
// contract forbids reducing workers or batch size to dodge a race.
type EndpointPresenceWriter interface {
	Upsert(ctx context.Context, rows []EndpointPresenceRow) error
	RetractScope(ctx context.Context, scopeIDs []string) error
	// RetractStaleRepoGenerations removes a keyspace's presence rows for the given
	// repos whose source_generation differs from generationID (#2842), so a repo's
	// removed or re-pathed endpoints/workloads stop being reported present once the
	// repo re-materializes. It is race-free under concurrent materializer workers:
	// it only deletes rows from OTHER generations, never the current generation's
	// rows that a sibling intent may have just upserted, and deleting an
	// already-removed older row is idempotent. A blank generationID or empty repo
	// set is a no-op.
	RetractStaleRepoGenerations(ctx context.Context, keyspace GraphProjectionKeyspace, scopeID, generationID string, repoIDs []string) error
}

// EndpointPresenceLookup answers the uid-exact cross-scope readiness question
// for the secrets/IAM graph projection gate (issue #1380). MissingUIDs returns
// the subset of uids that have no presence row for the keyspace, computed with
// ONE bounded query (WHERE keyspace=$1 AND uid = ANY($2)) and an in-memory
// set-difference — never an N+1 per-uid probe, which the §performance contract
// forbids. An empty input yields an empty result and no query.
type EndpointPresenceLookup interface {
	MissingUIDs(ctx context.Context, keyspace GraphProjectionKeyspace, uids []string) ([]string, error)
}
