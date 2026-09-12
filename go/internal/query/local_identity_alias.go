// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B5 root alias shim for #6642: type aliases and thin forwarders for the moved local-identity family must live in package query so handler wiring, cmd constructors, the setup family, and staying callers compile unchanged.

import (
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/local"
)

// local_identity_alias.go is the root alias shim for the local-identity
// handler family (#6642, modelled on freshness_alias.go and
// secrets_iam_alias.go). Its production files moved to local/. Names the
// rest of the program still spells `query.X` (handler.go's struct field,
// cmd/api's wiring, the setup family's staying files, sign_in_policy_mutations.go,
// and staying root tests) alias here so the move touches no caller outside
// the family.
//
// One home per symbol: nothing here implements behavior, it only aliases or
// forwards to the canonical home in local/. New code must import local
// directly.

// LocalIdentityHandler is the local-identity handler family type (production
// login, bootstrap, invitations, break-glass, API tokens, TOTP). Its home is
// local/ (IdentityHandler); this alias keeps handler.go's
// `LocalIdentity *LocalIdentityHandler` field and cmd/api's wiring spelling
// query.LocalIdentityHandler unchanged.
type LocalIdentityHandler = local.IdentityHandler

// LocalIdentityStore is the query-layer port for hash-only local identity
// persistence. Its home is local/ (IdentityStore); cmd/api's
// postgresLocalIdentityAdapter keeps implementing it as query.LocalIdentityStore.
type LocalIdentityStore = local.IdentityStore

// LocalIdentityProfileLister extends LocalIdentityStore with the list, MFA
// status, and TOTP enrollment operations ProfileHandler and
// SetupHandler need. Its home is local/ (IdentityProfileLister);
// profile_handler.go's `LocalIdentityStore LocalIdentityProfileLister` field
// keeps this spelling unchanged.
type LocalIdentityProfileLister = local.IdentityProfileLister

// LocalIdentityAuthContext is the query-layer auth context returned by
// storage. Its home is local/ (IdentityAuthContext); setup_mfa_handler.go's
// completed-wizard session build keeps spelling it query.LocalIdentityAuthContext.
type LocalIdentityAuthContext = local.IdentityAuthContext

// LocalIdentityAuthStatus is the bounded result of local credential
// validation. Its home is local/ (IdentityAuthStatus).
type LocalIdentityAuthStatus = local.IdentityAuthStatus

// LocalIdentityAuthenticationAttempt carries a local credential proof. Its
// home is local/ (IdentityAuthenticationAttempt); cmd/api's
// postgresLocalIdentityAdapter keeps this spelling.
type LocalIdentityAuthenticationAttempt = local.IdentityAuthenticationAttempt

// LocalIdentityAuthenticationResult is the hash-safe local login result. Its
// home is local/ (IdentityAuthenticationResult); cmd/api's
// postgresLocalIdentityAdapter keeps this spelling.
type LocalIdentityAuthenticationResult = local.IdentityAuthenticationResult

// LocalIdentityBootstrapRecord contains hash-only first-owner setup state.
// Its home is local/ (IdentityBootstrapRecord); cmd/api's
// postgresLocalIdentityAdapter keeps this spelling.
type LocalIdentityBootstrapRecord = local.IdentityBootstrapRecord

// LocalIdentityBreakGlassAttempt resolves one time-boxed recovery proof. Its
// home is local/ (IdentityBreakGlassAttempt).
type LocalIdentityBreakGlassAttempt = local.IdentityBreakGlassAttempt

// LocalIdentityBreakGlassWindow stores one time-boxed recovery window. Its
// home is local/ (IdentityBreakGlassWindow).
type LocalIdentityBreakGlassWindow = local.IdentityBreakGlassWindow

// LocalIdentityDisableUser disables a user and revokes active local
// sessions. Its home is local/ (IdentityDisableUser).
type LocalIdentityDisableUser = local.IdentityDisableUser

// LocalIdentityInvitationAcceptance creates a user from a live invitation.
// Its home is local/ (IdentityInvitationAcceptance).
type LocalIdentityInvitationAcceptance = local.IdentityInvitationAcceptance

// LocalIdentityInvitationRecord stores one hash-only local signup
// invitation. Its home is local/ (IdentityInvitationRecord).
type LocalIdentityInvitationRecord = local.IdentityInvitationRecord

// LocalIdentityMFAReset replaces active MFA factors and recovery hashes.
// Its home is local/ (IdentityMFAReset).
type LocalIdentityMFAReset = local.IdentityMFAReset

// LocalIdentityMFAStatus is the safe-to-expose MFA state for one identity.
// Its home is local/ (IdentityMFAStatus); cmd/api's
// postgresLocalIdentityAdapter keeps this spelling.
type LocalIdentityMFAStatus = local.IdentityMFAStatus

// LocalIdentityPasswordReset rotates one user's local password hash. Its
// home is local/ (IdentityPasswordReset); setup_types.go's
// `RotateSetupPassword(ctx, reset LocalIdentityPasswordReset)` and
// setup_handler.go keep this spelling.
type LocalIdentityPasswordReset = local.IdentityPasswordReset

// LocalIdentityPasswordRotation is a self-service credential rotation. Its
// home is local/ (IdentityPasswordRotation).
type LocalIdentityPasswordRotation = local.IdentityPasswordRotation

// LocalIdentitySessionResponse is returned after successful local login and
// after the setup wizard's final step. Its home is local/
// (IdentitySessionResponse); setup_types.go's SetupCompleteResponse.Auth
// field type keeps this spelling indirectly through
// queryauth.BrowserSessionAuthResponse (unaffected by this move).
type LocalIdentitySessionResponse = local.IdentitySessionResponse

// LocalIdentityAPITokenCreate stores one hash-only generated API token. Its
// home is local/ (IdentityAPITokenCreate).
type LocalIdentityAPITokenCreate = local.IdentityAPITokenCreate

// LocalIdentityAPITokenListItem is the metadata-only view of one API token
// safe to return to the owning subject. Its home is local/
// (IdentityAPITokenListItem); cmd/api's postgresLocalIdentityAdapter keeps
// this spelling.
type LocalIdentityAPITokenListItem = local.IdentityAPITokenListItem

// LocalIdentityAPITokenRevoke revokes one active generated API token. Its
// home is local/ (IdentityAPITokenRevoke).
type LocalIdentityAPITokenRevoke = local.IdentityAPITokenRevoke

// LocalIdentityAPITokenRotate atomically replaces one generated API token.
// Its home is local/ (IdentityAPITokenRotate).
type LocalIdentityAPITokenRotate = local.IdentityAPITokenRotate

// LocalIdentityTOTPEnrollmentBegin starts TOTP enrollment for one user. Its
// home is local/ (IdentityTOTPEnrollmentBegin); cmd/api's
// postgresLocalIdentityAdapter keeps this spelling.
type LocalIdentityTOTPEnrollmentBegin = local.IdentityTOTPEnrollmentBegin

// LocalIdentityTOTPEnrollmentConfirm verifies the first submitted TOTP code
// against a pending enrollment. Its home is local/
// (IdentityTOTPEnrollmentConfirm); cmd/api's postgresLocalIdentityAdapter
// keeps this spelling.
type LocalIdentityTOTPEnrollmentConfirm = local.IdentityTOTPEnrollmentConfirm

// Local identity auth-status value aliases preserve every pre-move root
// spelling of the closed status enumeration. cmd/api's
// postgresLocalIdentityAdapter and staying root tests
// (identity_handler_test.go, session_timeout_policy_test.go) keep these
// spellings.
const (
	LocalIdentityAuthAuthenticated      = local.IdentityAuthAuthenticated
	LocalIdentityAuthInvalid            = local.IdentityAuthInvalid
	LocalIdentityAuthMFARequired        = local.IdentityAuthMFARequired
	LocalIdentityAuthLocked             = local.IdentityAuthLocked
	LocalIdentityAuthDisabled           = local.IdentityAuthDisabled
	LocalIdentityAuthMustChangePassword = local.IdentityAuthMustChangePassword
)

// ErrLocalIdentityAPITokenNotFound reports that a self-service revoke or
// rotate matched no active token the caller owns (issue #5164). Its home is
// local/ (ErrIdentityAPITokenNotFound); cmd/api's postgresLocalIdentityAdapter
// keeps returning this exact sentinel so callers using errors.Is against the
// pre-move spelling still match.
var ErrLocalIdentityAPITokenNotFound = local.ErrIdentityAPITokenNotFound

// IdentityHash is the exported hash-only identity-field convention
// (go/cmd/api/seed_initial_admin.go, go/cmd/api/seed_initial_admin_helpers.go,
// and go/internal/cli/admin/credential.go all call query.IdentityHash). Its
// home is local/ (IdentityHash).
func IdentityHash(value string) string {
	return local.IdentityHash(value)
}

// localIdentityHash forwards to local.IdentityHash. It stays unexported and
// under its pre-move name because setup_handler.go and
// setup_handler_helpers.go (the staying setup family, moving in a later
// #6642 lane) call it as a bare package-private identifier.
func localIdentityHash(value string) string {
	return local.IdentityHash(value)
}

// localIdentityHashes forwards to local.IdentityHashes. It stays unexported
// and under its pre-move name because setup_mfa_handler.go calls it as a
// bare package-private identifier.
func localIdentityHashes(values []string) []string {
	return local.IdentityHashes(values)
}

// issueLocalSessionCookies forwards to local.IssueSessionCookies. It stays
// unexported and under its pre-move name because setup_mfa_handler.go (the
// wizard's final step, sharing the same session-issuance body as production
// login) calls it as a bare package-private identifier. Its return type
// local.SessionIssued must be exported at the leaf (naming rule 4 did not
// otherwise require exporting it) purely so this forwarder's own signature
// can name it -- see local/helpers.go's SessionIssued doc comment.
func issueLocalSessionCookies(
	w http.ResponseWriter,
	r *http.Request,
	sessions BrowserSessionStore,
	newSecret func() (string, error),
	now time.Time,
	idleTimeout time.Duration,
	absoluteTimeout time.Duration,
	cookieSecure CookieSecureMode,
	auth LocalIdentityAuthContext,
) (local.SessionIssued, bool) {
	return local.IssueSessionCookies(w, r, sessions, newSecret, now, idleTimeout, absoluteTimeout, cookieSecure, auth)
}

// localIdentityPolicyRevision forwards to local.PolicyRevision. It stays
// unexported and under its pre-move name because sign_in_policy_mutations.go
// calls it as a bare package-private identifier.
func localIdentityPolicyRevision(tenantID string, workspaceID string) string {
	return local.PolicyRevision(tenantID, workspaceID)
}
