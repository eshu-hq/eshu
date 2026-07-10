// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// GetSignInPolicy reads one tenant's sign-in policy without locking. A tenant
// with no row yet returns defaultSignInPolicy(tenantID), not an error.
func (s *IdentitySubjectStore) GetSignInPolicy(ctx context.Context, tenantID string) (SignInPolicy, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return SignInPolicy{}, errors.New("sign-in policy: tenant_id is required")
	}
	if s.db == nil {
		return SignInPolicy{}, errors.New("identity subject store database is required")
	}
	rows, err := s.db.QueryContext(ctx, selectSignInPolicyQuery, tenantID)
	if err != nil {
		return SignInPolicy{}, fmt.Errorf("get sign-in policy: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return defaultSignInPolicy(tenantID), rows.Err()
	}
	policy, err := scanSignInPolicyRow(rows)
	if err != nil {
		return SignInPolicy{}, fmt.Errorf("get sign-in policy: %w", err)
	}
	policy.TenantID = tenantID
	return policy, rows.Err()
}

// UpsertSignInPolicy applies a partial update to one tenant's sign-in policy
// inside a row-locked transaction, so two concurrent admin writes for the
// same tenant serialize instead of one silently clobbering the other.
//
// Guardrail: when the resulting RequireSSO is true, the same transaction
// checks (a) at least one provider config with Status="active" — proof of a
// passing connection test, since Status only reaches "active" via a
// synchronous test at enable time — and (b) SSOAdminVerifiedAt is set — proof
// at least one admin has completed one SSO sign-in for this tenant. Either
// check failing rejects the whole update with the matching sentinel error and
// the transaction is rolled back, so no partial policy change (e.g. an
// MFA-only edit bundled with an unproven require_sso=true) is ever
// half-applied. Provider-active-count is read via a plain (non-locking)
// SELECT rather than FOR SHARE against identity_provider_configs: this is an
// admin-rate-limited configuration action, not a hot path, and the residual
// TOCTOU window against a concurrent provider disable is harmless because
// break-glass local admin sign-in (go/internal/query LocalIdentityHandler)
// is unconditionally reachable regardless of RequireSSO — the guardrail
// exists to make an intentional enable safe, not to fully serialize against
// every unrelated concurrent write.
//
// Session revoke (issue #5002): whenever the RESULTING policy has
// RequireSSO=true, the same transaction bulk-revokes every active
// subject_class='local_user' browser session for the tenant (see
// revokeLocalBrowserSessionsForTenantQuery in browser_sessions_schema.go),
// so a password-authenticated session issued before a require_sso flip can
// never be resolved after it. This runs unconditionally on the resulting
// value, not the prior one, so it is idempotent and needs no "was it false
// before" branch. subject_class='break_glass' sessions are never touched
// (break-glass must stay reachable under lockdown); subject_class=
// 'external_oidc_user' sessions are unaffected because SSO already satisfies
// the policy being enabled.
func (s *IdentitySubjectStore) UpsertSignInPolicy(
	ctx context.Context,
	tenantID string,
	update SignInPolicyUpdate,
) (SignInPolicy, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return SignInPolicy{}, errors.New("sign-in policy: tenant_id is required")
	}
	update.PolicyRevisionHash = strings.TrimSpace(update.PolicyRevisionHash)
	if update.PolicyRevisionHash == "" {
		return SignInPolicy{}, errors.New("sign-in policy: policy_revision_hash is required")
	}
	now := update.Now
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	tx, err := s.beginLocalIdentityTx(ctx)
	if err != nil {
		return SignInPolicy{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, ensureSignInPolicyRowQuery, tenantID); err != nil {
		return SignInPolicy{}, fmt.Errorf("ensure sign-in policy row: %w", err)
	}
	rows, err := tx.QueryContext(ctx, selectSignInPolicyForUpdateQuery, tenantID)
	if err != nil {
		return SignInPolicy{}, fmt.Errorf("lock sign-in policy row: %w", err)
	}
	if !rows.Next() {
		_ = rows.Close()
		return SignInPolicy{}, errors.New("sign-in policy: locked row not found after ensure")
	}
	current, err := scanSignInPolicyRow(rows)
	closeErr := rows.Close()
	if err != nil {
		return SignInPolicy{}, fmt.Errorf("lock sign-in policy row: %w", err)
	}
	if closeErr != nil {
		return SignInPolicy{}, fmt.Errorf("lock sign-in policy row: %w", closeErr)
	}
	current.TenantID = tenantID

	next := current
	if update.RequireSSO != nil {
		next.RequireSSO = *update.RequireSSO
	}
	if update.AllowLocalUserCreation != nil {
		next.AllowLocalUserCreation = *update.AllowLocalUserCreation
	}
	if update.RequireMFAForAllUsers != nil {
		next.RequireMFAForAllUsers = *update.RequireMFAForAllUsers
	}
	if update.IdleTimeoutSeconds != nil {
		next.IdleTimeoutSeconds = *update.IdleTimeoutSeconds
	}
	if update.AbsoluteTimeoutSeconds != nil {
		next.AbsoluteTimeoutSeconds = *update.AbsoluteTimeoutSeconds
	}

	if next.RequireSSO {
		activeProviders, err := countActiveProviderConfigs(ctx, tx, tenantID)
		if err != nil {
			return SignInPolicy{}, err
		}
		if activeProviders == 0 {
			return SignInPolicy{}, ErrSignInPolicyGuardrailNoProvenProvider
		}
		if current.SSOAdminVerifiedAt.IsZero() {
			return SignInPolicy{}, ErrSignInPolicyGuardrailNoSSOAdminProof
		}
	}

	next.PolicyRevisionHash = update.PolicyRevisionHash
	next.UpdatedAt = now

	if _, err := tx.ExecContext(
		ctx,
		upsertSignInPolicyRowQuery,
		tenantID,
		next.RequireSSO,
		next.AllowLocalUserCreation,
		next.RequireMFAForAllUsers,
		nullableInt(next.IdleTimeoutSeconds),
		nullableInt(next.AbsoluteTimeoutSeconds),
		next.PolicyRevisionHash,
		next.UpdatedAt,
	); err != nil {
		return SignInPolicy{}, fmt.Errorf("upsert sign-in policy: %w", err)
	}

	if next.RequireSSO {
		if _, err := tx.ExecContext(
			ctx,
			revokeLocalBrowserSessionsForTenantQuery,
			tenantID,
			next.UpdatedAt,
		); err != nil {
			return SignInPolicy{}, fmt.Errorf("revoke local browser sessions on require_sso flip: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return SignInPolicy{}, fmt.Errorf("commit sign-in policy update: %w", err)
	}
	committed = true
	// SSOAdminVerifiedAt/SSOAdminVerifiedProviderConfigID are carried forward
	// from current (this update never changes them; only
	// RecordSSOAdminVerification does).
	next.SSOAdminVerifiedAt = current.SSOAdminVerifiedAt
	next.SSOAdminVerifiedProviderConfigID = current.SSOAdminVerifiedProviderConfigID
	return next, nil
}

// RecordSSOAdminVerification records that an admin completed one SSO sign-in
// for the tenant, satisfying half of the require_sso guardrail. It is
// sticky (first call wins; see recordSSOAdminVerificationQuery) and safe to
// call on every admin SSO login. Best-effort by design: callers (the
// browser-session creation adapter) must not fail session issuance if this
// write fails — see its call site's error-logging comment.
func (s *IdentitySubjectStore) RecordSSOAdminVerification(
	ctx context.Context,
	tenantID string,
	providerConfigID string,
	at time.Time,
) error {
	tenantID = strings.TrimSpace(tenantID)
	providerConfigID = strings.TrimSpace(providerConfigID)
	if tenantID == "" || providerConfigID == "" {
		return errors.New("sign-in policy: tenant_id and provider_config_id are required")
	}
	if s.db == nil {
		return errors.New("identity subject store database is required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	if _, err := s.db.ExecContext(ctx, recordSSOAdminVerificationQuery, tenantID, at, providerConfigID); err != nil {
		return fmt.Errorf("record sso admin verification: %w", err)
	}
	return nil
}

func countActiveProviderConfigs(ctx context.Context, db ExecQueryer, tenantID string) (int64, error) {
	rows, err := db.QueryContext(ctx, countActiveProviderConfigsQuery, tenantID)
	if err != nil {
		return 0, fmt.Errorf("count active provider configs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, errors.New("count active provider configs returned no rows")
	}
	var count int64
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("count active provider configs: %w", err)
	}
	return count, rows.Err()
}

// signInPolicyRequiresMFAForUsers reads require_mfa_for_all_users for one
// tenant within the caller's transaction (or any ExecQueryer). Absence of a
// row means false (the default), matching defaultSignInPolicy.
func signInPolicyRequiresMFAForUsers(ctx context.Context, db ExecQueryer, tenantID string) (bool, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return false, nil
	}
	rows, err := db.QueryContext(ctx, selectSignInPolicyRequireMFAQuery, tenantID)
	if err != nil {
		return false, fmt.Errorf("read sign-in policy mfa requirement: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, rows.Err()
	}
	var requireMFA bool
	if err := rows.Scan(&requireMFA); err != nil {
		return false, fmt.Errorf("read sign-in policy mfa requirement: %w", err)
	}
	return requireMFA, rows.Err()
}

// scanSignInPolicyRow scans one identity_sign_in_policies row. Callers set
// TenantID afterward (the query never selects it back).
func scanSignInPolicyRow(rows Rows) (SignInPolicy, error) {
	var (
		policy           SignInPolicy
		idle, absolute   sql.NullInt64
		verifiedAt       sql.NullTime
		verifiedProvider sql.NullString
	)
	if err := rows.Scan(
		&policy.RequireSSO,
		&policy.AllowLocalUserCreation,
		&policy.RequireMFAForAllUsers,
		&idle,
		&absolute,
		&verifiedAt,
		&verifiedProvider,
		&policy.PolicyRevisionHash,
		&policy.UpdatedAt,
	); err != nil {
		return SignInPolicy{}, err
	}
	if idle.Valid {
		policy.IdleTimeoutSeconds = int(idle.Int64)
	}
	if absolute.Valid {
		policy.AbsoluteTimeoutSeconds = int(absolute.Int64)
	}
	if verifiedAt.Valid {
		policy.SSOAdminVerifiedAt = verifiedAt.Time
	}
	if verifiedProvider.Valid {
		policy.SSOAdminVerifiedProviderConfigID = verifiedProvider.String
	}
	return policy, nil
}

// nullableInt maps a zero value ("unset"/"use process default") to SQL NULL,
// matching SignInPolicy's zero-means-default convention for the timeout
// fields.
func nullableInt(value int) sql.NullInt64 {
	if value == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(value), Valid: true}
}
