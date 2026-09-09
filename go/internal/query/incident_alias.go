// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // S2 root alias shim for #6060: type aliases and thin forwarders for the moved incident family must live in package query so handler wiring, cmd constructors, and staying callers compile unchanged.

import (
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	"github.com/eshu-hq/eshu/go/internal/query/incident/store"
	"github.com/eshu-hq/eshu/go/internal/query/service"
)

// incident_alias.go is the root alias shim for the incident-context handler
// family (#6060, lane B S2). The read-model types, the capability and limit
// constants, and the response assembly moved to incident/model/; the
// Postgres stores move to incident/store/ and the query text to
// incident/sql/; the HTTP surface moves to incident/. Names the rest of the
// program still spells `query.X` (APIRouter wiring, cmd/api and
// cmd/mcp-server constructors, staying callers and tests) alias here so the
// move touches no caller outside the family.
//
// One home per symbol: nothing here implements behavior, it only aliases or
// forwards to the canonical home. New code must import the incident leaves
// directly.

// incidentContextCapability is the query capability gating incident-context
// reads. Its home is incident/model/; this alias keeps the staying contract
// matrix, handler, store, and tests spelling the package-local name
// unchanged. See #6060.
const incidentContextCapability = model.Capability

// incidentContextDefaultLimit bounds an incident-context read when the caller
// passes no limit. Its home is incident/model/; see incidentContextCapability.
const incidentContextDefaultLimit = model.DefaultLimit

// incidentContextMaxLimit is the largest incident-context limit a caller may
// request. Its home is incident/model/; see incidentContextCapability.
const incidentContextMaxLimit = model.MaxLimit

// IncidentContextStore reads bounded incident context evidence. Its home is
// incident/model/; this alias keeps staying callers spelling
// query.IncidentContextStore unchanged. See #6060.
type IncidentContextStore = model.IncidentContextStore

// IncidentRepositoryAuthorizer resolves the durable owning repositories an
// incident correlates to. Its home is incident/model/; see IncidentContextStore.
type IncidentRepositoryAuthorizer = model.IncidentRepositoryAuthorizer

// IncidentContextFilter bounds one incident-context read. Its home is
// incident/model/; see IncidentContextStore.
type IncidentContextFilter = model.IncidentContextFilter

// IncidentContextQuery is the normalized query echoed in the response. Its
// home is incident/model/; see IncidentContextStore.
type IncidentContextQuery = model.IncidentContextQuery

// IncidentTruthLabel classifies one incident-context evidence edge. Its home
// is incident/model/; see IncidentContextStore.
type IncidentTruthLabel = model.IncidentTruthLabel

// Incident truth labels. Their home is incident/model/. See #6060.
const (
	IncidentTruthExact            = model.IncidentTruthExact
	IncidentTruthDerived          = model.IncidentTruthDerived
	IncidentTruthFallback         = model.IncidentTruthFallback
	IncidentTruthDrifted          = model.IncidentTruthDrifted
	IncidentTruthAmbiguous        = model.IncidentTruthAmbiguous
	IncidentTruthUnresolved       = model.IncidentTruthUnresolved
	IncidentTruthStale            = model.IncidentTruthStale
	IncidentTruthRejected         = model.IncidentTruthRejected
	IncidentTruthPermissionHidden = model.IncidentTruthPermissionHidden
	IncidentTruthMissing          = model.IncidentTruthMissing
)

// IncidentEvidenceSlot names one position in the incident evidence path. Its
// home is incident/model/; see IncidentContextStore.
type IncidentEvidenceSlot = model.IncidentEvidenceSlot

// Incident evidence slots. Their home is incident/model/. See #6060.
const (
	IncidentSlotIncident        = model.IncidentSlotIncident
	IncidentSlotService         = model.IncidentSlotService
	IncidentSlotIntendedRouting = model.IncidentSlotIntendedRouting
	IncidentSlotAppliedRouting  = model.IncidentSlotAppliedRouting
	IncidentSlotLiveRouting     = model.IncidentSlotLiveRouting
	IncidentSlotDeployable      = model.IncidentSlotDeployable
	IncidentSlotRuntimeArtifact = model.IncidentSlotRuntimeArtifact
	IncidentSlotImage           = model.IncidentSlotImage
	IncidentSlotBuildDeploy     = model.IncidentSlotBuildDeploy
	IncidentSlotCommit          = model.IncidentSlotCommit
	IncidentSlotPullRequest     = model.IncidentSlotPullRequest
	IncidentSlotWorkItem        = model.IncidentSlotWorkItem
)

// IncidentContextSnapshot is the store-owned evidence packet before response
// defaults and missing slots are applied. Its home is incident/model/; see
// IncidentContextStore.
type IncidentContextSnapshot = model.IncidentContextSnapshot

// IncidentContextResponse is the public API/MCP response for one incident.
// Its home is incident/model/; see IncidentContextStore.
type IncidentContextResponse = model.IncidentContextResponse

// IncidentContextIncident is the provider-reported incident anchor. Its home
// is incident/model/; see IncidentContextStore.
type IncidentContextIncident = model.IncidentContextIncident

// IncidentContextReference is a bounded provider reference. Its home is
// incident/model/; see IncidentContextStore.
type IncidentContextReference = model.IncidentContextReference

// IncidentContextTimelineEvent is one provider-reported incident event. Its
// home is incident/model/; see IncidentContextStore.
type IncidentContextTimelineEvent = model.IncidentContextTimelineEvent

// IncidentContextChangeCandidate is a related or time-window candidate
// change. Its home is incident/model/; see IncidentContextStore.
type IncidentContextChangeCandidate = model.IncidentContextChangeCandidate

// IncidentContextLink is a sanitized provider link. Its home is
// incident/model/; see IncidentContextStore.
type IncidentContextLink = model.IncidentContextLink

// IncidentContextEvidenceEdge explains one slot in the evidence path. Its
// home is incident/model/; see IncidentContextStore.
type IncidentContextEvidenceEdge = model.IncidentContextEvidenceEdge

// IncidentContextEvidenceRef points to one source or reducer fact behind an
// edge. Its home is incident/model/; see IncidentContextStore.
type IncidentContextEvidenceRef = model.IncidentContextEvidenceRef

// IncidentContextEvidenceCandidate describes an ambiguous evidence
// candidate. Its home is incident/model/; see IncidentContextStore.
type IncidentContextEvidenceCandidate = model.IncidentContextEvidenceCandidate

// IncidentMissingEvidence names evidence that was not present for the
// incident. Its home is incident/model/; see IncidentContextStore.
type IncidentMissingEvidence = model.IncidentMissingEvidence

// IncidentContextIncidentCandidate identifies an ambiguous incident anchor.
// Its home is incident/model/; see IncidentContextStore.
type IncidentContextIncidentCandidate = model.IncidentContextIncidentCandidate

// BuildIncidentContextResponse applies the public incident-context contract
// to a store snapshot. Its home is incident/model/; this forwarder keeps
// staying callers spelling the package-local name unchanged. See #6060.
func BuildIncidentContextResponse(snapshot IncidentContextSnapshot) IncidentContextResponse {
	return model.BuildIncidentContextResponse(snapshot)
}

// ErrIncidentContextNotFound reports a missing incident anchor. Its home is
// incident/model/; this alias keeps the staying handler and scope files
// spelling query.ErrIncidentContextNotFound unchanged. See #6060.
var ErrIncidentContextNotFound = model.ErrIncidentContextNotFound

// IncidentContextAmbiguousError reports multiple active incident anchors.
// Its home is incident/model/; this alias keeps the staying handler spelling
// query.IncidentContextAmbiguousError unchanged. See #6060.
type IncidentContextAmbiguousError = model.IncidentContextAmbiguousError

// normalizeIncidentContextFilter trims and defaults one incident-context
// filter. Its home is incident/model/ (NormalizeFilter); this wrapper keeps
// the staying handler spelling the package-local name until that file moves
// to incident/ in the same lane. See #6060.
func normalizeIncidentContextFilter(filter IncidentContextFilter) IncidentContextFilter {
	return model.NormalizeFilter(filter)
}

// PostgresIncidentContextStore reads active PagerDuty incident source facts.
// Its home is incident/store/; this alias keeps the cmd/api and
// cmd/mcp-server wiring spelling query.PostgresIncidentContextStore
// unchanged. See #6060.
type PostgresIncidentContextStore = store.PostgresIncidentContextStore

// NewPostgresIncidentContextStore creates the Postgres incident-context
// store. Its home is incident/store/ (NewStore); this forwarder builds the
// service-catalog, CI/CD run correlation, and container image identity
// sub-stores the runtime evidence reads through, keeping the cmd/api and
// cmd/mcp-server wiring calling query.NewPostgresIncidentContextStore
// unchanged. See #6060.
func NewPostgresIncidentContextStore(db *sql.DB) PostgresIncidentContextStore {
	return store.NewStore(db).
		WithCatalog(service.NewPostgresServiceCatalogCorrelationStore(db)).
		WithCICD(NewPostgresCICDRunCorrelationStore(db)).
		WithImages(NewPostgresContainerImageIdentityStore(db))
}

// PostgresIncidentRepositoryAuthorizer resolves an incident's durable owning
// repositories from the reducer-owned correlation edge. Its home is
// incident/store/; this alias keeps the cmd/api and cmd/mcp-server wiring
// spelling query.PostgresIncidentRepositoryAuthorizer unchanged. See #6060.
type PostgresIncidentRepositoryAuthorizer = store.PostgresIncidentRepositoryAuthorizer

// NewPostgresIncidentRepositoryAuthorizer creates the Postgres incident
// repository authorizer over the shared fact store. Its home is
// incident/store/; this forwarder keeps the cmd/api and cmd/mcp-server
// wiring calling query.NewPostgresIncidentRepositoryAuthorizer unchanged.
// See #6060.
func NewPostgresIncidentRepositoryAuthorizer(db *sql.DB) PostgresIncidentRepositoryAuthorizer {
	return store.PostgresIncidentRepositoryAuthorizer{DB: db}
}
