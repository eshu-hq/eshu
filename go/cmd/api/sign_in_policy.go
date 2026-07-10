// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// postgresSignInPolicyAdapter implements query.SignInPolicyReadStore and
// query.SignInPolicyMutationStore over storage/postgres.IdentitySubjectStore
// (epic #4962, issue #4968), mapping between the query-layer and postgres-
// layer SignInPolicy/SignInPolicyUpdate types the same way
// postgresLocalIdentityAdapter and providerConfigReadAdapter already do for
// their sibling identity surfaces.
type postgresSignInPolicyAdapter struct {
	store *pgstatus.IdentitySubjectStore
}

func newPostgresSignInPolicyAdapter(db *sql.DB, instruments *telemetry.Instruments) *postgresSignInPolicyAdapter {
	if db == nil {
		return nil
	}
	signInPolicyDB := pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})
	if instruments != nil {
		signInPolicyDB = &pgstatus.InstrumentedDB{
			Inner:       signInPolicyDB,
			Tracer:      otel.Tracer(telemetry.DefaultSignalName),
			Instruments: instruments,
			StoreName:   "identity_sign_in_policy",
		}
	}
	return &postgresSignInPolicyAdapter{store: pgstatus.NewIdentitySubjectStore(signInPolicyDB)}
}

// newSignInPolicyReadHandler wires the sign-in policy read routes (public +
// admin). Nil-safe: a nil database yields a handler whose store is nil, so
// each route returns 503 rather than panicking, matching
// newAdminProviderConfigReadHandler's convention.
func newSignInPolicyReadHandler(db *sql.DB, instruments *telemetry.Instruments) *query.SignInPolicyReadHandler {
	handler := &query.SignInPolicyReadHandler{}
	if store := newPostgresSignInPolicyAdapter(db, instruments); store != nil {
		handler.Store = store
	}
	return handler
}

// newSignInPolicyMutationHandler wires the admin sign-in policy write route.
func newSignInPolicyMutationHandler(
	db *sql.DB,
	instruments *telemetry.Instruments,
	governanceAudit query.GovernanceAuditSummaryReader,
) *query.SignInPolicyMutationHandler {
	handler := &query.SignInPolicyMutationHandler{
		Audit:       adminRecoveryAuditAppender(governanceAudit),
		Instruments: instruments,
	}
	if store := newPostgresSignInPolicyAdapter(db, instruments); store != nil {
		handler.Store = store
	}
	return handler
}

func (a *postgresSignInPolicyAdapter) GetSignInPolicy(ctx context.Context, tenantID string) (query.SignInPolicy, error) {
	policy, err := a.store.GetSignInPolicy(ctx, tenantID)
	if err != nil {
		return query.SignInPolicy{}, err
	}
	return signInPolicyFromPostgres(policy), nil
}

func (a *postgresSignInPolicyAdapter) UpsertSignInPolicy(
	ctx context.Context,
	tenantID string,
	update query.SignInPolicyUpdateRequest,
	policyRevisionHash string,
	now time.Time,
) (query.SignInPolicy, error) {
	policy, err := a.store.UpsertSignInPolicy(ctx, tenantID, pgstatus.SignInPolicyUpdate{
		RequireSSO:             update.RequireSSO,
		AllowLocalUserCreation: update.AllowLocalUserCreation,
		RequireMFAForAllUsers:  update.RequireMFAForAllUsers,
		IdleTimeoutSeconds:     update.IdleTimeoutSeconds,
		AbsoluteTimeoutSeconds: update.AbsoluteTimeoutSeconds,
		PolicyRevisionHash:     policyRevisionHash,
		Now:                    now,
	})
	if err != nil {
		return query.SignInPolicy{}, mapSignInPolicyGuardrailError(err)
	}
	return signInPolicyFromPostgres(policy), nil
}

// mapSignInPolicyGuardrailError translates the postgres-layer guardrail
// sentinels to their query-layer equivalents (errors.Is-compatible against
// the query package's own sentinels) so go/internal/query never imports
// storage/postgres directly, matching writeProviderConfigWriteError's
// layering for the sibling provider-config surface.
func mapSignInPolicyGuardrailError(err error) error {
	switch {
	case errors.Is(err, pgstatus.ErrSignInPolicyGuardrailNoProvenProvider):
		return query.ErrSignInPolicyGuardrailNoProvenProvider
	case errors.Is(err, pgstatus.ErrSignInPolicyGuardrailNoSSOAdminProof):
		return query.ErrSignInPolicyGuardrailNoSSOAdminProof
	case errors.Is(err, pgstatus.ErrSignInPolicyTimeoutOrdering):
		return query.ErrSignInPolicyTimeoutOrdering
	default:
		return err
	}
}

func signInPolicyFromPostgres(policy pgstatus.SignInPolicy) query.SignInPolicy {
	return query.SignInPolicy{
		TenantID:                         policy.TenantID,
		RequireSSO:                       policy.RequireSSO,
		AllowLocalUserCreation:           policy.AllowLocalUserCreation,
		RequireMFAForAllUsers:            policy.RequireMFAForAllUsers,
		IdleTimeoutSeconds:               policy.IdleTimeoutSeconds,
		AbsoluteTimeoutSeconds:           policy.AbsoluteTimeoutSeconds,
		SSOAdminVerifiedAt:               policy.SSOAdminVerifiedAt,
		SSOAdminVerifiedProviderConfigID: policy.SSOAdminVerifiedProviderConfigID,
		PolicyRevisionHash:               policy.PolicyRevisionHash,
		UpdatedAt:                        policy.UpdatedAt,
	}
}
