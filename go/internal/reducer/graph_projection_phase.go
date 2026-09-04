// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
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

// GraphProjectionPhaseState captures one durable readiness publication. It is
// aliased from [gpphase.PhaseState] so the family subpackages that publish a
// readiness milestone (issue #6061) and every existing reducer-root caller name
// the same struct.
type GraphProjectionPhaseState = gpphase.PhaseState

// GraphProjectionReadinessLookup reports whether a bounded readiness slice
// has reached the requested phase. It returns (ready, found).
type GraphProjectionReadinessLookup = gpphase.ReadinessLookup

// GraphProjectionReadinessPrefetch resolves readiness for a bounded set of
// keys and returns an in-memory lookup closure for the current cycle.
type GraphProjectionReadinessPrefetch = gpphase.ReadinessPrefetch

// GraphProjectionPhasePublisher persists graph-readiness publications. It is
// aliased from [gpphase.PhasePublisher] so a family subpackage can accept the
// same publisher the reducer root wires.
type GraphProjectionPhasePublisher = gpphase.PhasePublisher

// EndpointPresenceRow records that one endpoint node uid is committed in the
// canonical graph, keyed by its bounded keyspace. It is aliased from
// [gpphase.EndpointPresenceRow] (issue #6061) so the family subpackages that
// write endpoint presence and every existing reducer-root caller name the
// same struct; see [gpphase.EndpointPresenceRow] for the uid-exact,
// cross-scope readiness primitive it captures (issue #1380, ADR #1314 §6/§8).
type EndpointPresenceRow = gpphase.EndpointPresenceRow

// EndpointPresenceWriter records and retracts endpoint-node presence. It is
// aliased from [gpphase.EndpointPresenceWriter] (issue #6061) so a family
// subpackage can accept the same writer the reducer root wires; see
// [gpphase.EndpointPresenceWriter] for the CloudResource / KubernetesWorkload
// node-materializer contract it implements.
type EndpointPresenceWriter = gpphase.EndpointPresenceWriter

// EndpointPresenceLookup answers the uid-exact cross-scope readiness question
// for the secrets/IAM graph projection gate (issue #1380). It moved to
// [gpphase] in issue #6061 so the secretsiam family can name it without
// importing this package; see [gpphase.EndpointPresenceLookup] for the bounded
// query contract MissingUIDs must honour.
type EndpointPresenceLookup = gpphase.EndpointPresenceLookup
