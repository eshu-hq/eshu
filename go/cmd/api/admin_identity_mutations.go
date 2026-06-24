// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"

	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// newAdminIdentityMutationHandler wires the tenant-scoped admin identity
// mutation endpoints over Postgres. The handler is nil-safe: a nil database
// yields a handler whose store is nil, so each route returns 503 rather than
// panicking. The audit appender is shared with the read/recovery paths.
func newAdminIdentityMutationHandler(
	db *sql.DB,
	instruments *telemetry.Instruments,
	governanceAudit query.GovernanceAuditSummaryReader,
) *query.AdminIdentityMutationHandler {
	handler := &query.AdminIdentityMutationHandler{
		Audit: adminRecoveryAuditAppender(governanceAudit),
	}
	if store := newPostgresAdminIdentityMutationAdapter(db, instruments); store != nil {
		handler.Store = store
	}
	return handler
}

// postgresAdminIdentityMutationAdapter translates the query-layer admin mutation
// contract into the Postgres IdentitySubjectStore, mapping each request/result
// between the two type sets. The raw external group name never crosses this
// boundary; only its precomputed hash does.
type postgresAdminIdentityMutationAdapter struct {
	store *pgstatus.IdentitySubjectStore
}

func newPostgresAdminIdentityMutationAdapter(
	db *sql.DB,
	instruments *telemetry.Instruments,
) *postgresAdminIdentityMutationAdapter {
	if db == nil {
		return nil
	}
	identityDB := pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})
	if instruments != nil {
		identityDB = &pgstatus.InstrumentedDB{
			Inner:       identityDB,
			Tracer:      otel.Tracer("eshu-api"),
			Instruments: instruments,
			StoreName:   "identity_subjects",
		}
	}
	return &postgresAdminIdentityMutationAdapter{store: pgstatus.NewIdentitySubjectStore(identityDB)}
}

func (a *postgresAdminIdentityMutationAdapter) RevokeAdminInvitation(
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

func (a *postgresAdminIdentityMutationAdapter) GrantAdminRoleAssignment(
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

func (a *postgresAdminIdentityMutationAdapter) RevokeAdminRoleAssignment(
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

func (a *postgresAdminIdentityMutationAdapter) CreateAdminIdPGroupMapping(
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

func (a *postgresAdminIdentityMutationAdapter) DeleteAdminIdPGroupMapping(
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
