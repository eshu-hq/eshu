// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	auditstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/governance/audit"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// coreAdminIdentityReadAdapter translates the admin read contract into the
// Postgres identity subject store, mapping metadata-only item sets.
type coreAdminIdentityReadAdapter struct {
	store *pgstatus.IdentitySubjectStore
}

func newCoreAdminIdentityReadAdapter(
	rawDB *sql.DB,
	instruments *telemetry.Instruments,
) *coreAdminIdentityReadAdapter {
	if rawDB == nil {
		return nil
	}
	return &coreAdminIdentityReadAdapter{
		store: pgstatus.NewIdentitySubjectStore(coreInstrumentedDB(rawDB, instruments, "identity_subjects")),
	}
}

// ListAdminInvitations returns invitations in the tenant/workspace.
func (a *coreAdminIdentityReadAdapter) ListAdminInvitations(
	ctx context.Context,
	tenantID, workspaceID string,
) ([]query.AdminInvitationListItem, error) {
	items, err := a.store.ListAdminInvitations(ctx, tenantID, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]query.AdminInvitationListItem, 0, len(items))
	for _, item := range items {
		out = append(out, query.AdminInvitationListItem{
			InviteID:    item.InviteID,
			RoleID:      item.RoleID,
			Status:      item.Status,
			ExpiresAt:   item.ExpiresAt,
			AcceptedAt:  item.AcceptedAt,
			RevokedAt:   item.RevokedAt,
			CreatedAt:   item.CreatedAt,
			UpdatedAt:   item.UpdatedAt,
			TenantID:    item.TenantID,
			WorkspaceID: item.WorkspaceID,
		})
	}
	return out, nil
}

// ListAdminRoleAssignments returns membership-role assignments.
func (a *coreAdminIdentityReadAdapter) ListAdminRoleAssignments(
	ctx context.Context,
	tenantID, workspaceID, userID string,
) ([]query.AdminRoleAssignmentListItem, error) {
	items, err := a.store.ListAdminRoleAssignments(ctx, tenantID, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	out := make([]query.AdminRoleAssignmentListItem, 0, len(items))
	for _, item := range items {
		out = append(out, query.AdminRoleAssignmentListItem{
			UserID:           item.UserID,
			RoleID:           item.RoleID,
			AssignmentSource: item.AssignmentSource,
			Status:           item.Status,
			EffectiveAt:      item.EffectiveAt,
			ExpiresAt:        item.ExpiresAt,
			TenantID:         item.TenantID,
			WorkspaceID:      item.WorkspaceID,
		})
	}
	return out, nil
}

// ListAdminRoles returns the tenant's roles and the grants each confers.
func (a *coreAdminIdentityReadAdapter) ListAdminRoles(
	ctx context.Context,
	tenantID string,
) ([]query.AdminRoleListItem, bool, error) {
	items, grantsTruncated, err := a.store.ListAdminRoles(ctx, tenantID)
	if err != nil {
		return nil, false, err
	}
	out := make([]query.AdminRoleListItem, 0, len(items))
	for _, item := range items {
		grants := make([]query.AdminRoleGrantListItem, 0, len(item.Grants))
		for _, grant := range item.Grants {
			grants = append(grants, query.AdminRoleGrantListItem{
				GrantID:    grant.GrantID,
				Action:     grant.Action,
				Feature:    grant.Feature,
				DataClass:  grant.DataClass,
				ScopeClass: grant.ScopeClass,
				Status:     grant.Status,
			})
		}
		out = append(out, query.AdminRoleListItem{
			RoleID:  item.RoleID,
			Status:  item.Status,
			BuiltIn: item.BuiltIn,
			Grants:  grants,
		})
	}
	return out, grantsTruncated, nil
}

// ListAdminIdPProviders returns the tenant's configured identity providers.
func (a *coreAdminIdentityReadAdapter) ListAdminIdPProviders(
	ctx context.Context,
	tenantID string,
) ([]query.AdminIdPProviderListItem, error) {
	items, err := a.store.ListAdminIdPProviders(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]query.AdminIdPProviderListItem, 0, len(items))
	for _, item := range items {
		out = append(out, query.AdminIdPProviderListItem{
			ProviderConfigID: item.ProviderConfigID,
			ProviderKind:     item.ProviderKind,
			Status:           item.Status,
		})
	}
	return out, nil
}

// ListAdminIdPGroupMappings returns the tenant/workspace group→role mappings.
func (a *coreAdminIdentityReadAdapter) ListAdminIdPGroupMappings(
	ctx context.Context,
	tenantID, workspaceID, afterRef string,
) ([]query.AdminIdPGroupMappingListItem, error) {
	items, err := a.store.ListAdminIdPGroupMappings(ctx, tenantID, workspaceID, afterRef)
	if err != nil {
		return nil, err
	}
	out := make([]query.AdminIdPGroupMappingListItem, 0, len(items))
	for _, item := range items {
		out = append(out, query.AdminIdPGroupMappingListItem{
			MappingRef:       item.MappingRef,
			ProviderConfigID: item.ProviderConfigID,
			RoleID:           item.RoleID,
			Status:           item.Status,
			EffectiveAt:      item.EffectiveAt,
			ExpiresAt:        item.ExpiresAt,
			TenantID:         item.TenantID,
			WorkspaceID:      item.WorkspaceID,
		})
	}
	return out, nil
}

// ListAdminAPITokens returns every user's generated tokens in the tenant.
func (a *coreAdminIdentityReadAdapter) ListAdminAPITokens(
	ctx context.Context,
	tenantID, workspaceID string,
) ([]query.AdminAPITokenListItem, error) {
	items, err := a.store.ListAdminAPITokens(ctx, tenantID, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]query.AdminAPITokenListItem, 0, len(items))
	for _, item := range items {
		out = append(out, query.AdminAPITokenListItem{
			TokenID:            item.TokenID,
			TokenClass:         item.TokenClass,
			UserID:             item.UserID,
			ServicePrincipalID: item.ServicePrincipalID,
			Status:             item.Status,
			DisplayLabel:       item.DisplayLabel,
			IssuedAt:           item.IssuedAt,
			ExpiresAt:          item.ExpiresAt,
			RevokedAt:          item.RevokedAt,
			TenantID:           item.TenantID,
			WorkspaceID:        item.WorkspaceID,
		})
	}
	return out, nil
}

// coreAdminGovernanceAuditReader adapts the Postgres governance audit store's
// authorized List and aggregate query.Summary into the tenant-admin audit read
// contract.
type coreAdminGovernanceAuditReader struct {
	store   auditstore.GovernanceAuditReader
	summary query.GovernanceAuditSummaryReader
}

func newCoreAdminGovernanceAuditReaderWithReadStore(
	reader db.Queryer,
	instruments *telemetry.Instruments,
	summary query.GovernanceAuditSummaryReader,
	logger *slog.Logger,
) *coreAdminGovernanceAuditReader {
	if reader == nil {
		return nil
	}
	return &coreAdminGovernanceAuditReader{
		store:   newCoreGovernanceAuditReader(reader, instruments, logger),
		summary: summary,
	}
}

// ListAuditEvents lists authorized audit events for the admin surface.
func (a *coreAdminGovernanceAuditReader) ListAuditEvents(
	ctx context.Context,
	q query.AdminAuditQuery,
) ([]governanceaudit.Event, error) {
	return a.store.List(ctx, auditstore.GovernanceAuditQuery{
		OperatorAuthorized: q.OperatorAuthorized,
		EventType:          governanceaudit.EventType(q.EventType),
		Decision:           governanceaudit.Decision(q.Decision),
		ReasonCode:         q.ReasonCode,
		OccurredAfter:      q.OccurredAfter,
		OccurredBefore:     q.OccurredBefore,
		Limit:              q.Limit,
		OrderDesc:          q.OrderDesc,
		TenantID:           q.TenantID,
	})
}

// SummarizeAuditEvents aggregates audit counts, preferring the cached
// summary reader when wired.
func (a *coreAdminGovernanceAuditReader) SummarizeAuditEvents(
	ctx context.Context,
) (governanceaudit.Summary, error) {
	if a.summary != nil {
		return a.summary.Summary(ctx)
	}
	return a.store.Summary(ctx)
}

// SummarizeAuditEventsForTenant aggregates audit counts for one tenant,
// bypassing the cache (which is not keyed by tenant).
func (a *coreAdminGovernanceAuditReader) SummarizeAuditEventsForTenant(
	ctx context.Context,
	tenantID string,
) (governanceaudit.Summary, error) {
	return a.store.SummaryForTenant(ctx, tenantID)
}

// coreAdminIdentityMutationAdapter translates the admin mutation contract
// into the Postgres identity subject store. The raw external group name
// never crosses this boundary; only its precomputed hash does.
type coreAdminIdentityMutationAdapter struct {
	store *pgstatus.IdentitySubjectStore
}

var _ query.AdminIdentityAdmission = (*coreAdminIdentityMutationAdapter)(nil)

// newCoreAdminIdentityMutationHandler wires the tenant-scoped admin identity
// mutations through the versioned admission contract.
func newCoreAdminIdentityMutationHandler(
	store query.AdminIdentityAdmission,
	governanceAudit query.GovernanceAuditSummaryReader,
) *query.AdminIdentityMutationHandler {
	handler := &query.AdminIdentityMutationHandler{
		Audit: coreRecoveryAuditAppender(governanceAudit),
	}
	if store = unwrapNilAdmissionPort(store); store != nil {
		handler.Store = store
	}
	return handler
}

// newCorePostgresAdminIdentityMutationAdapter builds the mutation adapter,
// or nil without a database.
func newCorePostgresAdminIdentityMutationAdapter(
	rawDB *sql.DB,
	instruments *telemetry.Instruments,
) *coreAdminIdentityMutationAdapter {
	if rawDB == nil {
		return nil
	}
	return &coreAdminIdentityMutationAdapter{
		store: pgstatus.NewIdentitySubjectStore(coreInstrumentedDB(rawDB, instruments, "identity_subjects")),
	}
}

// RevokeAdminInvitation soft-revokes an invitation (idempotent).
func (a *coreAdminIdentityMutationAdapter) RevokeAdminInvitation(
	ctx context.Context,
	req query.AdminInvitationRevokeRequest,
) (query.AdminInvitationRevokeResult, error) {
	result, err := a.store.RevokeAdminInvitation(ctx, pgstatus.AdminInvitationRevoke{
		InviteID:    req.InviteID,
		TenantID:    req.TenantID,
		WorkspaceID: req.WorkspaceID,
		RevokedAt:   req.RevokedAt,
	})
	if err != nil {
		return query.AdminInvitationRevokeResult{}, err
	}
	return query.AdminInvitationRevokeResult{
		Found:   result.Found,
		Revoked: result.Revoked,
		Status:  result.Status,
	}, nil
}

// GrantAdminRoleAssignment idempotently activates a membership-role row.
func (a *coreAdminIdentityMutationAdapter) GrantAdminRoleAssignment(
	ctx context.Context,
	req query.AdminRoleAssignmentGrantRequest,
) (query.AdminRoleAssignmentMutationResult, error) {
	result, err := a.store.GrantAdminRoleAssignment(ctx, pgstatus.AdminRoleAssignmentGrant{
		TenantID:           req.TenantID,
		WorkspaceID:        req.WorkspaceID,
		UserID:             req.UserID,
		RoleID:             req.RoleID,
		AssignmentSource:   req.AssignmentSource,
		PolicyRevisionHash: req.PolicyRevisionHash,
		EffectiveAt:        req.EffectiveAt,
	})
	if err != nil {
		return query.AdminRoleAssignmentMutationResult{}, err
	}
	return query.AdminRoleAssignmentMutationResult{
		RoleValid: result.RoleValid,
		UserValid: result.UserValid,
		Changed:   result.Changed,
		Status:    result.Status,
	}, nil
}

// RevokeAdminRoleAssignment tombstones a membership-role row (idempotent).
func (a *coreAdminIdentityMutationAdapter) RevokeAdminRoleAssignment(
	ctx context.Context,
	req query.AdminRoleAssignmentRevokeRequest,
) (query.AdminRoleAssignmentMutationResult, error) {
	result, err := a.store.RevokeAdminRoleAssignment(ctx, pgstatus.AdminRoleAssignmentRevoke{
		TenantID:    req.TenantID,
		WorkspaceID: req.WorkspaceID,
		UserID:      req.UserID,
		RoleID:      req.RoleID,
		RevokedAt:   req.RevokedAt,
	})
	if err != nil {
		return query.AdminRoleAssignmentMutationResult{}, err
	}
	return query.AdminRoleAssignmentMutationResult{
		RoleValid: result.RoleValid,
		Changed:   result.Changed,
		Status:    result.Status,
	}, nil
}

// CreateAdminIdPGroupMapping idempotently activates a group→role mapping.
func (a *coreAdminIdentityMutationAdapter) CreateAdminIdPGroupMapping(
	ctx context.Context,
	req query.AdminIdPGroupMappingCreateRequest,
) (query.AdminIdPGroupMappingCreateResult, error) {
	result, err := a.store.CreateAdminIdPGroupMapping(ctx, pgstatus.AdminIdPGroupMappingCreate{
		ProviderConfigID:   req.ProviderConfigID,
		ExternalGroupHash:  req.ExternalGroupHash,
		TenantID:           req.TenantID,
		WorkspaceID:        req.WorkspaceID,
		RoleID:             req.RoleID,
		MappingSource:      req.MappingSource,
		PolicyRevisionHash: req.PolicyRevisionHash,
		EffectiveAt:        req.EffectiveAt,
	})
	if err != nil {
		return query.AdminIdPGroupMappingCreateResult{}, err
	}
	return query.AdminIdPGroupMappingCreateResult{
		ProviderValid: result.ProviderValid,
		RoleValid:     result.RoleValid,
		Created:       result.Created,
		MappingRef:    result.MappingRef,
		Status:        result.Status,
	}, nil
}

// DeleteAdminIdPGroupMapping tombstones one mapping by mapping_ref
// (idempotent).
func (a *coreAdminIdentityMutationAdapter) DeleteAdminIdPGroupMapping(
	ctx context.Context,
	req query.AdminIdPGroupMappingDeleteRequest,
) (query.AdminIdPGroupMappingDeleteResult, error) {
	result, err := a.store.DeleteAdminIdPGroupMapping(ctx, pgstatus.AdminIdPGroupMappingDelete{
		MappingRef:  req.MappingRef,
		TenantID:    req.TenantID,
		WorkspaceID: req.WorkspaceID,
		RevokedAt:   req.RevokedAt,
	})
	if err != nil {
		return query.AdminIdPGroupMappingDeleteResult{}, err
	}
	return query.AdminIdPGroupMappingDeleteResult{
		Found:   result.Found,
		Deleted: result.Deleted,
	}, nil
}
