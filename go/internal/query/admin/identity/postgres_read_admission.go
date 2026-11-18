// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import (
	"context"

	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// NewAdminIdentityReadAdmission binds the core-owned Postgres identity
// subject store behind the versioned #9669 D3 admission contract, so the
// API handler depends on the pin instead of a direct store handle. The
// caller owns the store (including any instrumentation wrapping); this adapter only
// translates the metadata-only item sets between the two type sets. A
// nil store yields a nil admission, and the handler answers 503 through
// its existing storeReady gate — never a confident empty page.
//
// D-wave consumer: D3 (admin/identity/recovery reads, per the arbiter
// D-wave ruling on #9669). This constructor lives outside go/cmd/api
// on purpose: the cmd-local adapter deletes at removal, while this one
// stays as the canonical core-owned construction the satellite records
// and the removal-time wiring adopts.
//
// Rollback: revert this file and the ReadHandler field swap. The pin
// itself is untouched, so rollback moves no traffic and changes no data.
func NewAdminIdentityReadAdmission(store *pgstatus.IdentitySubjectStore) AdminIdentityReadAdmission {
	if store == nil {
		return nil
	}
	return &postgresAdminIdentityReadAdapter{store: store}
}

// postgresAdminIdentityReadAdapter translates the Postgres
// IdentitySubjectStore reads into the query-layer admin read contract.
// Every method is a bounded tenant-scoped metadata-only list: no secret,
// hash, invite code, credential handle, or external group hash crosses
// this boundary.
type postgresAdminIdentityReadAdapter struct {
	store *pgstatus.IdentitySubjectStore
}

// The adapter satisfies the D3 pin with the minimal shape: no wider
// store surface leaks through the admission.
var _ AdminIdentityReadAdmission = (*postgresAdminIdentityReadAdapter)(nil)

func (a *postgresAdminIdentityReadAdapter) ListAdminInvitations(
	ctx context.Context,
	tenantID, workspaceID string,
) ([]InvitationListItem, error) {
	items, err := a.store.ListAdminInvitations(ctx, tenantID, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]InvitationListItem, 0, len(items))
	for _, item := range items {
		out = append(out, InvitationListItem{
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

func (a *postgresAdminIdentityReadAdapter) ListAdminRoleAssignments(
	ctx context.Context,
	tenantID, workspaceID, userID string,
) ([]RoleAssignmentListItem, error) {
	items, err := a.store.ListAdminRoleAssignments(ctx, tenantID, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	out := make([]RoleAssignmentListItem, 0, len(items))
	for _, item := range items {
		out = append(out, RoleAssignmentListItem{
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

func (a *postgresAdminIdentityReadAdapter) ListAdminRoles(
	ctx context.Context,
	tenantID string,
) ([]RoleListItem, bool, error) {
	items, grantsTruncated, err := a.store.ListAdminRoles(ctx, tenantID)
	if err != nil {
		return nil, false, err
	}
	out := make([]RoleListItem, 0, len(items))
	for _, item := range items {
		grants := make([]RoleGrantListItem, 0, len(item.Grants))
		for _, grant := range item.Grants {
			grants = append(grants, RoleGrantListItem{
				GrantID:    grant.GrantID,
				Action:     grant.Action,
				Feature:    grant.Feature,
				DataClass:  grant.DataClass,
				ScopeClass: grant.ScopeClass,
				Status:     grant.Status,
			})
		}
		out = append(out, RoleListItem{
			RoleID:  item.RoleID,
			Status:  item.Status,
			BuiltIn: item.BuiltIn,
			Grants:  grants,
		})
	}
	return out, grantsTruncated, nil
}

func (a *postgresAdminIdentityReadAdapter) ListAdminIdPProviders(
	ctx context.Context,
	tenantID string,
) ([]IdPProviderListItem, error) {
	items, err := a.store.ListAdminIdPProviders(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]IdPProviderListItem, 0, len(items))
	for _, item := range items {
		out = append(out, IdPProviderListItem{
			ProviderConfigID: item.ProviderConfigID,
			ProviderKind:     item.ProviderKind,
			Status:           item.Status,
		})
	}
	return out, nil
}

func (a *postgresAdminIdentityReadAdapter) ListAdminIdPGroupMappings(
	ctx context.Context,
	tenantID, workspaceID, afterRef string,
) ([]IdPGroupMappingListItem, error) {
	items, err := a.store.ListAdminIdPGroupMappings(ctx, tenantID, workspaceID, afterRef)
	if err != nil {
		return nil, err
	}
	out := make([]IdPGroupMappingListItem, 0, len(items))
	for _, item := range items {
		out = append(out, IdPGroupMappingListItem{
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

func (a *postgresAdminIdentityReadAdapter) ListAdminAPITokens(
	ctx context.Context,
	tenantID, workspaceID string,
) ([]APITokenListItem, error) {
	items, err := a.store.ListAdminAPITokens(ctx, tenantID, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]APITokenListItem, 0, len(items))
	for _, item := range items {
		out = append(out, APITokenListItem{
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
