// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // S1 root alias shim for #6060: type aliases and thin forwarders for the moved admin family must live in package query so handler wiring, cmd constructors, and staying callers compile unchanged.

import (
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	"github.com/eshu-hq/eshu/go/internal/query/admin/identity"
	"github.com/eshu-hq/eshu/go/internal/query/admin/provider/config"
	"github.com/eshu-hq/eshu/go/internal/query/admin/store"
)

// admin_alias.go is the root alias shim for the admin handler family
// (#6060, lane B S1). Handler, Store, the work-item/decision row and filter
// models, and the replay-safety set moved to admin/; the tenant identity
// reads/mutations moved to admin/identity/; the provider-config
// reads/mutations moved to admin/provider/config/; the Postgres store moved
// to admin/store/; the shared audit/permission glue moved to admin/audit/.
// Names the rest of the program still spells `query.X` (APIRouter wiring,
// cmd/api and cmd/mcp-server constructors, staying callers and tests) alias
// here so the move touches no caller outside the family.
//
// One home per symbol: nothing here implements behavior, it only aliases or
// forwards to the canonical home. New code must import the admin leaves
// directly.

// AdminHandler is the admin-handler family type. Its home is admin/; this
// alias keeps the APIRouter wiring and the cmd/api constructors spelling
// query.AdminHandler unchanged. See #6060.
type AdminHandler = admin.Handler

// AdminStore is the admin Postgres port. Its home is admin/ (implemented in
// admin/store/); this alias keeps the cmd/api and cmd/mcp-server wiring
// spelling query.AdminStore unchanged. See #6060.
type AdminStore = admin.Store

// AdminDeadLetterListHandler mounts only the bounded dead-letter read
// surface. Its home is admin/; this alias keeps the cmd/mcp-server wiring
// spelling query.AdminDeadLetterListHandler unchanged. See #6060.
type AdminDeadLetterListHandler = admin.DeadLetterListHandler

// AdminInputInvalidFactListHandler mounts only the bounded
// reducer_input_invalid_facts read surface. Its home is admin/; this alias
// keeps the cmd/mcp-server wiring spelling
// query.AdminInputInvalidFactListHandler unchanged. See #6060.
type AdminInputInvalidFactListHandler = admin.InputInvalidFactListHandler

// AdminWorkItem is an admin-friendly view of a fact_work_items row. Its home
// is admin/; this alias keeps staying callers spelling query.AdminWorkItem
// unchanged. See #6060.
type AdminWorkItem = admin.WorkItem

// AdminDeadLetterWorkItem is a bounded operator-facing view of one durable
// fact_work_items dead-letter row. Its home is admin/. See #6060.
type AdminDeadLetterWorkItem = admin.DeadLetterWorkItem

// AdminReducerInputInvalidFact is a bounded operator-facing view of one
// durable reducer_input_invalid_facts row. Its home is admin/. See #6060.
type AdminReducerInputInvalidFact = admin.InputInvalidFact

// AdminReplayEvent is an admin-friendly view of a fact_replay_events row.
// Its home is admin/. See #6060.
type AdminReplayEvent = admin.ReplayEvent

// AdminBackfillRequest is an admin-friendly view of a fact_backfill_requests
// row. Its home is admin/. See #6060.
type AdminBackfillRequest = admin.BackfillRequest

// AdminDecisionRow is a query-layer view of a projection decision row. Its
// home is admin/. See #6060.
type AdminDecisionRow = admin.DecisionRow

// AdminEvidenceRow is a query-layer view of a projection decision evidence
// row. Its home is admin/. See #6060.
type AdminEvidenceRow = admin.EvidenceRow

// ReplayIdempotencyClaim is the outcome of attempting to claim a replay
// idempotency key. Its home is admin/ (implemented in admin/store/). See
// #6060.
type ReplayIdempotencyClaim = admin.ReplayIdempotencyClaim

// WorkItemFilter constrains admin work-item queries. Its home is admin/.
// See #6060.
type WorkItemFilter = admin.WorkItemFilter

// DeadLetterListFilter constrains bounded dead-letter read queries. Its home
// is admin/. See #6060.
type DeadLetterListFilter = admin.DeadLetterListFilter

// DeadLetterFilter constrains admin dead-letter operations. Its home is
// admin/. See #6060.
type DeadLetterFilter = admin.DeadLetterFilter

// ReplayWorkItemFilter constrains admin replay operations. Its home is
// admin/. See #6060.
type ReplayWorkItemFilter = admin.ReplayWorkItemFilter

// BackfillInput captures the parameters for a backfill request. Its home is
// admin/. See #6060.
type BackfillInput = admin.BackfillInput

// ReplayEventFilter constrains replay-event audit queries. Its home is
// admin/. See #6060.
type ReplayEventFilter = admin.ReplayEventFilter

// DecisionQueryFilter constrains projection-decision queries. Its home is
// admin/. See #6060.
type DecisionQueryFilter = admin.DecisionQueryFilter

// InputInvalidFactListFilter constrains bounded reducer_input_invalid_facts
// read queries. Its home is admin/. See #6060.
type InputInvalidFactListFilter = admin.InputInvalidFactListFilter

// RecoveryService is the subset of recovery.Handler used by admin endpoints.
// Its home is admin/. See #6060.
type RecoveryService = admin.RecoveryService

// ReindexRequester is the subset of the reindex surface used by admin
// routes. Its home is admin/. See #6060.
type ReindexRequester = admin.ReindexRequester

// NewPostgresAdminStore constructs an AdminStore backed by Postgres. Its
// home is admin/store/; this forwarder keeps the cmd/api and cmd/mcp-server
// wiring calling query.NewPostgresAdminStore unchanged. See #6060.
func NewPostgresAdminStore(db *sql.DB) AdminStore {
	return store.NewStore(db)
}

// AdminIdentityReadHandler serves tenant-scoped admin read endpoints. Its
// home is admin/identity/; this alias keeps the APIRouter wiring and the
// cmd/api constructors spelling query.AdminIdentityReadHandler unchanged.
// See #6060.
type AdminIdentityReadHandler = identity.ReadHandler

// AdminIdentityMutationHandler serves tenant-scoped admin write endpoints.
// Its home is admin/identity/; this alias keeps the APIRouter wiring and
// the cmd/api constructors spelling query.AdminIdentityMutationHandler
// unchanged. See #6060.
type AdminIdentityMutationHandler = identity.MutationHandler

// AdminInvitationListItem is the metadata-only invitation view returned to a
// tenant admin. Its home is admin/identity/. See #6060.
type AdminInvitationListItem = identity.InvitationListItem

// AdminRoleAssignmentListItem is the metadata-only membership-role
// assignment view returned to a tenant admin. Its home is admin/identity/.
// See #6060.
type AdminRoleAssignmentListItem = identity.RoleAssignmentListItem

// AdminRoleGrantListItem is one capability grant attached to a role. Its
// home is admin/identity/. See #6060.
type AdminRoleGrantListItem = identity.RoleGrantListItem

// AdminRoleListItem is a tenant role plus the grants it confers. Its home is
// admin/identity/. See #6060.
type AdminRoleListItem = identity.RoleListItem

// AdminIdPProviderListItem is the metadata-only identity-provider view
// returned to a tenant admin. Its home is admin/identity/. See #6060.
type AdminIdPProviderListItem = identity.IdPProviderListItem

// AdminIdPGroupMappingListItem is the metadata-only group->role mapping view
// returned to a tenant admin. Its home is admin/identity/. See #6060.
type AdminIdPGroupMappingListItem = identity.IdPGroupMappingListItem

// AdminAPITokenListItem is the metadata-only generated-token view returned
// to a tenant admin. Its home is admin/identity/. See #6060.
type AdminAPITokenListItem = identity.APITokenListItem

// AdminAuditQuery bounds an admin audit-event read. Its home is
// admin/identity/. See #6060.
type AdminAuditQuery = identity.AuditQuery

// AdminIdentityReadStore is the read surface the admin console backend uses
// for tenant-scoped identity metadata. Its home is admin/identity/; this
// alias keeps the cmd/api adapters implementing
// query.AdminIdentityReadStore unchanged. See #6060.
type AdminIdentityReadStore = identity.ReadStore

// AdminGovernanceAuditReader is the read surface for tenant-admin audit
// links. Its home is admin/identity/. See #6060.
type AdminGovernanceAuditReader = identity.GovernanceAuditReader

// AdminIdentityMutationStore is the write surface the admin console backend
// uses for tenant-scoped identity mutations. Its home is admin/identity/;
// this alias keeps the cmd/api adapters implementing
// query.AdminIdentityMutationStore unchanged. See #6060.
type AdminIdentityMutationStore = identity.MutationStore

// AdminInvitationRevokeRequest revokes one invitation in the caller's
// tenant/workspace. Its home is admin/identity/. See #6060.
type AdminInvitationRevokeRequest = identity.InvitationRevokeRequest

// AdminInvitationRevokeResult reports the terminal state of a revoke
// attempt. Its home is admin/identity/. See #6060.
type AdminInvitationRevokeResult = identity.InvitationRevokeResult

// AdminRoleAssignmentGrantRequest grants a membership-role row for a user.
// Its home is admin/identity/. See #6060.
type AdminRoleAssignmentGrantRequest = identity.RoleAssignmentGrantRequest

// AdminRoleAssignmentMutationResult reports whether an active assignment row
// was affected. Its home is admin/identity/. See #6060.
type AdminRoleAssignmentMutationResult = identity.RoleAssignmentMutationResult

// AdminRoleAssignmentRevokeRequest tombstones a membership-role row. Its
// home is admin/identity/. See #6060.
type AdminRoleAssignmentRevokeRequest = identity.RoleAssignmentRevokeRequest

// AdminIdPGroupMappingCreateRequest creates an external group->role mapping.
// Its home is admin/identity/. See #6060.
type AdminIdPGroupMappingCreateRequest = identity.IdPGroupMappingCreateRequest

// AdminIdPGroupMappingCreateResult reports the outcome of a mapping create.
// Its home is admin/identity/. See #6060.
type AdminIdPGroupMappingCreateResult = identity.IdPGroupMappingCreateResult

// AdminIdPGroupMappingDeleteRequest tombstones one external group->role
// mapping. Its home is admin/identity/. See #6060.
type AdminIdPGroupMappingDeleteRequest = identity.IdPGroupMappingDeleteRequest

// AdminIdPGroupMappingDeleteResult reports whether a mapping was tombstoned.
// Its home is admin/identity/. See #6060.
type AdminIdPGroupMappingDeleteResult = identity.IdPGroupMappingDeleteResult

// AdminProviderConfigReadHandler serves the DB-backed identity
// provider-config read endpoints. Its home is admin/provider/config/; this
// alias keeps the APIRouter wiring and the cmd/api constructors spelling
// query.AdminProviderConfigReadHandler unchanged. See #6060.
type AdminProviderConfigReadHandler = config.ReadHandler

// AdminProviderConfigMutationHandler serves the DB-backed identity
// provider-config write endpoints. Its home is admin/provider/config/; this
// alias keeps the APIRouter wiring and the cmd/api constructors spelling
// query.AdminProviderConfigMutationHandler unchanged. See #6060.
type AdminProviderConfigMutationHandler = config.MutationHandler

// AdminProviderConfigDetail is the metadata-only admin view returned by GET
// routes. Its home is admin/provider/config/. See #6060.
type AdminProviderConfigDetail = config.Detail

// AdminProviderConfigRevisionItem is one row of a provider config's revision
// history. Its home is admin/provider/config/. See #6060.
type AdminProviderConfigRevisionItem = config.RevisionItem

// AdminProviderConfigCreateRequest is the store-facing create request. Its
// home is admin/provider/config/. See #6060.
type AdminProviderConfigCreateRequest = config.CreateRequest

// AdminProviderConfigUpdateRequest is the store-facing update request. Its
// home is admin/provider/config/. See #6060.
type AdminProviderConfigUpdateRequest = config.UpdateRequest

// AdminProviderConfigRevertRequest is the store-facing revert request. Its
// home is admin/provider/config/. See #6060.
type AdminProviderConfigRevertRequest = config.RevertRequest

// AdminProviderConfigWriteResult is returned by every provider-config
// mutation. Its home is admin/provider/config/. See #6060.
type AdminProviderConfigWriteResult = config.WriteResult

// AdminProviderConfigConnectionTestResult reports a test-connection outcome.
// Its home is admin/provider/config/. See #6060.
type AdminProviderConfigConnectionTestResult = config.ConnectionTestResult

// AdminProviderConfigMutationStore is the write surface the provider-config
// admin handler uses. Its home is admin/provider/config/; this alias keeps
// the cmd/api adapters implementing query.AdminProviderConfigMutationStore
// unchanged. See #6060.
type AdminProviderConfigMutationStore = config.MutationStore

// AdminProviderConfigReadStore is the read surface the provider-config admin
// handler uses. Its home is admin/provider/config/; this alias keeps the
// cmd/api adapters implementing query.AdminProviderConfigReadStore
// unchanged. See #6060.
type AdminProviderConfigReadStore = config.ReadStore

// ProviderConfigConnectionTester runs the bounded, safe portion of a
// provider's connection test. Its home is admin/provider/config/; this alias
// keeps the cmd/api wiring spelling query.ProviderConfigConnectionTester
// unchanged. See #6060.
type ProviderConfigConnectionTester = config.ConnectionTester

// ErrAdminProviderConfigDuplicateKey mirrors
// postgres.ErrProviderConfigDuplicateKey. Its home is
// admin/provider/config/. See #6060.
var ErrAdminProviderConfigDuplicateKey = config.ErrDuplicateKey

// ErrAdminProviderConfigKeyringUnavailable mirrors
// postgres.ErrProviderSecretKeyringUnavailable. Its home is
// admin/provider/config/. See #6060.
var ErrAdminProviderConfigKeyringUnavailable = config.ErrKeyringUnavailable

// ErrAdminProviderConfigRevisionNotFound mirrors
// postgres.ErrProviderConfigRevisionNotFound. Its home is
// admin/provider/config/. See #6060.
var ErrAdminProviderConfigRevisionNotFound = config.ErrRevisionNotFound

// ErrAdminProviderConfigKindMismatch mirrors
// postgres.ErrProviderConfigKindMismatch. Its home is
// admin/provider/config/. See #6060.
var ErrAdminProviderConfigKindMismatch = config.ErrKindMismatch

// ErrAdminProviderConfigRevisionChanged mirrors
// postgres.ErrProviderConfigRevisionChanged. Its home is
// admin/provider/config/. See #6060.
var ErrAdminProviderConfigRevisionChanged = config.ErrRevisionChanged

// ErrAdminProviderConfigManagedByEnvironment is returned by
// AdminProviderConfigMutationStore implementations for env-managed
// providers. Its home is admin/provider/config/. See #6060.
var ErrAdminProviderConfigManagedByEnvironment = config.ErrManagedByEnvironment
