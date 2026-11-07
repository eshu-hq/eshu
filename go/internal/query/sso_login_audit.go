// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
)

// SSOLoginDeniedError lives in auth (D13, #9669); this alias keeps the
// callback handlers, the login services, and every errors.Is/errors.As
// caller matching the identical wrapped sentinel.
type SSOLoginDeniedError = auth.SSOLoginDeniedError

// SSOLoginDenialReason forwards to the auth-owned extractor so audit
// callers keep one stable entry point.
func SSOLoginDenialReason(err error, fallback string) string {
	return auth.SSOLoginDenialReason(err, fallback)
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
