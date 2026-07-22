// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// SSOLoginDeniedError wraps an SSO login sentinel — ErrGitHubLoginDenied,
// ErrGitHubLoginUnavailable, ErrOIDCLoginDenied, or ErrOIDCLoginUnavailable —
// with the specific classification that produced it (e.g. "org_not_allowed",
// "no_grants"). The githublogin and oidclogin CompleteXLogin implementations
// already compute this classification internally (issue #5601: it used to be
// logged via slog and then discarded); wrapping it here lets the query-layer
// callback handler record WHY a login was denied as a governance-audit
// ReasonCode instead of a generic catch-all, without changing the HTTP
// status mapping in writeGitHubLoginError/writeOIDCLoginError — errors.Is
// against the wrapped Sentinel keeps matching through Unwrap exactly as it
// did before this type existed.
type SSOLoginDeniedError struct {
	// Sentinel is the bare provider sentinel this error wraps
	// (ErrGitHubLoginDenied, ErrGitHubLoginUnavailable, ErrOIDCLoginDenied,
	// or ErrOIDCLoginUnavailable).
	Sentinel error
	// Reason is a stable, low-cardinality classification safe to use
	// verbatim as a governanceaudit.Event.ReasonCode: lowercase ASCII
	// letters, digits, and underscore, at most 64 characters (see
	// governanceaudit.validReasonCode).
	Reason string
}

// Error implements the error interface.
func (e *SSOLoginDeniedError) Error() string {
	if e.Sentinel == nil {
		return e.Reason
	}
	if e.Reason == "" {
		return e.Sentinel.Error()
	}
	return e.Sentinel.Error() + ": " + e.Reason
}

// Unwrap exposes the wrapped sentinel so errors.Is(err, ErrGitHubLoginDenied)
// (or the OIDC/Unavailable equivalents) keeps matching unchanged.
func (e *SSOLoginDeniedError) Unwrap() error { return e.Sentinel }

// SSOLoginDenialReason extracts the classification an SSOLoginDeniedError
// carries via errors.As. Returns fallback when err does not wrap one (or
// wraps one with an empty Reason), so callers always get a valid non-empty
// reason code to audit.
func SSOLoginDenialReason(err error, fallback string) string {
	var denied *SSOLoginDeniedError
	if errors.As(err, &denied) && denied.Reason != "" {
		return denied.Reason
	}
	return fallback
}

// recordSSOLoginAuthentication appends one identity_authentication
// governance-audit event for an SSO (GitHub, OIDC, or SAML) callback
// outcome, mirroring LocalIdentityHandler.auditLocalIdentity's shape (issue
// #5601). This is the ONLY durable trace of who authenticated via SSO and
// when: SSO deliberately creates no local identity row (see
// LocalIdentityHandler.handleCreateInvitation's doc comment on invitations
// being the local-only account-creation path), and the browser_sessions row
// issued on success eventually expires — after that, without this event,
// there is no evidence anyone signed in via SSO at all.
//
// subjectIDHash is the hashed external subject — never a raw subject, email,
// or token. It is only ever non-empty on a successful callback: every
// CompleteGitHubLogin/CompleteOIDCLogin denial branch (state_invalid,
// org_not_allowed, no_grants, ...) returns before a verified identity is
// resolved, so no subject hash exists yet to attach to a denied attempt.
//
// tenantID and workspaceID scope the event to the tenant/workspace the
// session was issued into, mirroring AuthContext.TenantID/WorkspaceID (issue
// #5601 follow-up: without this, GovernanceAuditStore.List/SummaryForTenant
// exclude NULL-tenant rows for a tenant admin, making every SSO login
// invisible to the tenant that must audit it). They are only ever non-empty
// on a successful callback for the same reason subjectIDHash is: every
// denial branch returns before identity/tenant resolution completes, so
// there is no tenant/workspace yet to attach to a denied attempt.
func recordSSOLoginAuthentication(
	r *http.Request,
	audit GovernanceAuditAppender,
	now time.Time,
	decision governanceaudit.Decision,
	reasonCode string,
	subjectIDHash string,
	tenantID string,
	workspaceID string,
) {
	if audit == nil {
		return
	}
	actorClass := governanceaudit.ActorClassAnonymous
	if subjectIDHash != "" {
		actorClass = governanceaudit.ActorClassOperator
	}
	event := governanceaudit.Event{
		Type:        governanceaudit.EventTypeIdentityAuthentication,
		ActorClass:  actorClass,
		ActorIDHash: subjectIDHash,
		ScopeClass:  governanceaudit.ScopeClassAdmin,
		Decision:    decision,
		ReasonCode:  reasonCode,
		OccurredAt:  now,
		TenantID:    tenantID,
		WorkspaceID: workspaceID,
	}
	_ = audit.Append(r.Context(), []governanceaudit.Event{event})
}

// auditGitHubSSOLogin classifies a GitHub CompleteGitHubLogin outcome and
// records the resulting identity_authentication event. err == nil records
// the successful outcome ("sso_login_authenticated") with subjectIDHash.
// Every non-nil error is audited, including an error that is neither
// ErrGitHubLoginUnavailable nor ErrGitHubLoginDenied — for example
// ErrGitHubLoginInvalidRequest returned by CompleteGitHubLogin itself (the
// provider config was deleted between login-start and callback) or an
// unwrapped connector-factory error — via the default case below, so the
// "audit every callback outcome" guarantee (issue #5601) holds for every
// outcome CompleteGitHubLogin can return. A callback that never reaches
// CompleteGitHubLogin at all (h.ready returns false: the service or session
// store is unavailable) is not audited, mirroring LocalIdentityHandler not
// auditing a ReadJSON parse failure before Store.AuthenticateLocalIdentity is
// ever called — this function is only ever invoked with the error
// CompleteGitHubLogin returned.
func auditGitHubSSOLogin(
	r *http.Request,
	audit GovernanceAuditAppender,
	now time.Time,
	err error,
	subjectIDHash string,
	tenantID string,
	workspaceID string,
) {
	if err == nil {
		recordSSOLoginAuthentication(r, audit, now, governanceaudit.DecisionAllowed, "sso_login_authenticated", subjectIDHash, tenantID, workspaceID)
		return
	}
	switch {
	case errors.Is(err, ErrGitHubLoginUnavailable):
		reason := SSOLoginDenialReason(err, "sso_login_unavailable")
		recordSSOLoginAuthentication(r, audit, now, governanceaudit.DecisionUnavailable, reason, subjectIDHash, tenantID, workspaceID)
	case errors.Is(err, ErrGitHubLoginDenied):
		reason := SSOLoginDenialReason(err, "sso_login_denied")
		recordSSOLoginAuthentication(r, audit, now, governanceaudit.DecisionDenied, reason, subjectIDHash, tenantID, workspaceID)
	default:
		reason := SSOLoginDenialReason(err, "sso_login_error")
		recordSSOLoginAuthentication(r, audit, now, governanceaudit.DecisionDenied, reason, subjectIDHash, tenantID, workspaceID)
	}
}

// auditOIDCSSOLogin is auditGitHubSSOLogin's OIDC mirror, classifying
// against ErrOIDCLoginUnavailable/ErrOIDCLoginDenied instead, with the same
// default-case guarantee that every non-nil error CompleteOIDCLogin returns
// is audited.
func auditOIDCSSOLogin(
	r *http.Request,
	audit GovernanceAuditAppender,
	now time.Time,
	err error,
	subjectIDHash string,
	tenantID string,
	workspaceID string,
) {
	if err == nil {
		recordSSOLoginAuthentication(r, audit, now, governanceaudit.DecisionAllowed, "sso_login_authenticated", subjectIDHash, tenantID, workspaceID)
		return
	}
	switch {
	case errors.Is(err, ErrOIDCLoginUnavailable):
		reason := SSOLoginDenialReason(err, "sso_login_unavailable")
		recordSSOLoginAuthentication(r, audit, now, governanceaudit.DecisionUnavailable, reason, subjectIDHash, tenantID, workspaceID)
	case errors.Is(err, ErrOIDCLoginDenied):
		reason := SSOLoginDenialReason(err, "sso_login_denied")
		recordSSOLoginAuthentication(r, audit, now, governanceaudit.DecisionDenied, reason, subjectIDHash, tenantID, workspaceID)
	default:
		reason := SSOLoginDenialReason(err, "sso_login_error")
		recordSSOLoginAuthentication(r, audit, now, governanceaudit.DecisionDenied, reason, subjectIDHash, tenantID, workspaceID)
	}
}
