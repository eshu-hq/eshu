// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"

	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// newAdminIdentityReadHandler wires the tenant-scoped admin identity read
// endpoints over Postgres. The handler is nil-safe: a nil database yields a
// handler whose store and audit reader are nil, so each route returns 503
// rather than panicking.
func newAdminIdentityReadHandler(
	db *sql.DB,
	instruments *telemetry.Instruments,
	governanceAudit query.GovernanceAuditSummaryReader,
) *query.AdminIdentityReadHandler {
	handler := &query.AdminIdentityReadHandler{}
	if store := newPostgresAdminIdentityReadAdapter(db, instruments); store != nil {
		handler.Store = store
	}
	if reader := newAdminGovernanceAuditReader(db, instruments, governanceAudit); reader != nil {
		handler.Audit = reader
	}
	return handler
}

// postgresAdminIdentityReadAdapter translates the query-layer admin read
// contract into the Postgres IdentitySubjectStore, mapping each metadata-only
// item set between the two type sets.
type postgresAdminIdentityReadAdapter struct {
	store *pgstatus.IdentitySubjectStore
}

func newPostgresAdminIdentityReadAdapter(
	db *sql.DB,
	instruments *telemetry.Instruments,
) *postgresAdminIdentityReadAdapter {
	if db == nil {
		return nil
	}
	identityDB := pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})
	if instruments != nil {
		identityDB = &pgstatus.InstrumentedDB{
			Inner:       identityDB,
			Tracer:      otel.Tracer(telemetry.DefaultSignalName),
			Instruments: instruments,
			StoreName:   "identity_subjects",
		}
	}
	return &postgresAdminIdentityReadAdapter{store: pgstatus.NewIdentitySubjectStore(identityDB)}
}

func (a *postgresAdminIdentityReadAdapter) ListAdminInvitations(
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

func (a *postgresAdminIdentityReadAdapter) ListAdminRoleAssignments(
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

func (a *postgresAdminIdentityReadAdapter) ListAdminRoles(
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

func (a *postgresAdminIdentityReadAdapter) ListAdminIdPProviders(
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

func (a *postgresAdminIdentityReadAdapter) ListAdminIdPGroupMappings(
	ctx context.Context,
	tenantID, workspaceID string,
) ([]query.AdminIdPGroupMappingListItem, error) {
	items, err := a.store.ListAdminIdPGroupMappings(ctx, tenantID, workspaceID)
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

func (a *postgresAdminIdentityReadAdapter) ListAdminAPITokens(
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

// adminGovernanceAuditReader adapts the Postgres governance audit store's
// authorized List and aggregate Summary into the query-layer admin audit read
// contract. List and Summary share one store; a reader that does not implement
// the detailed List surface disables the events endpoint without failing
// startup.
type adminGovernanceAuditReader struct {
	store   pgstatus.GovernanceAuditStore
	summary query.GovernanceAuditSummaryReader
}

func newAdminGovernanceAuditReader(
	db *sql.DB,
	instruments *telemetry.Instruments,
	summary query.GovernanceAuditSummaryReader,
) *adminGovernanceAuditReader {
	if db == nil {
		return nil
	}
	governanceAuditDB := pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})
	if instruments != nil {
		governanceAuditDB = &pgstatus.InstrumentedDB{
			Inner:       governanceAuditDB,
			Tracer:      otel.Tracer(telemetry.DefaultSignalName),
			Instruments: instruments,
			StoreName:   "governance_audit",
		}
	}
	return &adminGovernanceAuditReader{
		store:   pgstatus.NewGovernanceAuditStore(governanceAuditDB),
		summary: summary,
	}
}

func (a *adminGovernanceAuditReader) ListAuditEvents(
	ctx context.Context,
	q query.AdminAuditQuery,
) ([]governanceaudit.Event, error) {
	return a.store.List(ctx, pgstatus.GovernanceAuditQuery{
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

func (a *adminGovernanceAuditReader) SummarizeAuditEvents(
	ctx context.Context,
) (governanceaudit.Summary, error) {
	if a.summary != nil {
		return a.summary.Summary(ctx)
	}
	return a.store.Summary(ctx)
}

// SummarizeAuditEventsForTenant returns aggregate audit counts scoped to a
// single tenant. It bypasses the in-memory summary cache (a.summary) because
// the cache is not keyed by tenant; a direct DB hit is intentional here and is
// served efficiently by the governance_audit_events_tenant_idx partial index
// added in the #3717 schema migration.
func (a *adminGovernanceAuditReader) SummarizeAuditEventsForTenant(
	ctx context.Context,
	tenantID string,
) (governanceaudit.Summary, error) {
	return a.store.SummaryForTenant(ctx, tenantID)
}
