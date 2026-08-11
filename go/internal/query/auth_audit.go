// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

const governanceAuditAppendTimeout = 500 * time.Millisecond

func recordReadAuthorizationDenied(r *http.Request, audit GovernanceAuditAppender) {
	recordReadAuthorizationDeniedWithReason(r, audit, "authentication_required")
}

func recordReadAuthorizationDeniedWithReason(
	r *http.Request,
	audit GovernanceAuditAppender,
	reasonCode string,
) {
	if audit == nil {
		return
	}
	event := governanceaudit.Event{
		Type:          governanceaudit.EventTypeReadAuthorization,
		ActorClass:    governanceaudit.ActorClassAnonymous,
		ScopeClass:    governanceaudit.ScopeClassAdmin,
		Decision:      governanceaudit.DecisionDenied,
		ReasonCode:    strings.TrimSpace(reasonCode),
		CorrelationID: safeAuditCorrelationID(documentationCorrelationID(r)),
		OccurredAt:    time.Now().UTC(),
	}
	if event.ReasonCode == "" {
		event.ReasonCode = "authentication_required"
	}
	ctx, cancel := context.WithTimeout(r.Context(), governanceAuditAppendTimeout)
	defer cancel()
	_ = audit.Append(ctx, []governanceaudit.Event{event})
}

// bearerDenialOutcomeCarrier is implemented by a credential-resolution error
// that knows which bounded outcome caused the denial (internal/oidcbearer's
// denial error is the production implementation). It is declared here as a
// structural interface rather than importing the resolver package, so the query
// package keeps owning its read surfaces without depending on a specific
// identity provider implementation.
type bearerDenialOutcomeCarrier interface {
	DenialOutcome() string
}

// auditableBearerDenialReasons is the closed set of denial outcomes allowed into
// governance_audit_events.reason_code. reason_code is an enum operators filter
// and group by, so a resolver must not widen it at will: an unrecognized value
// would make an existing "show me every denial of kind X" query silently
// incomplete. Anything outside this set audits as authentication_required.
var auditableBearerDenialReasons = map[string]struct{}{
	"expired":                      {},
	"wrong_audience":               {},
	"unknown_issuer":               {},
	"bad_signature":                {},
	"malformed":                    {},
	"no_grants":                    {},
	"jwks_fetch_failure":           {},
	"grant_resolution_unavailable": {},
}

// unavailableBearerDenialReasons are the outcomes that mean a dependency could
// not answer, rather than a verdict about the credential. They record
// DecisionUnavailable, matching what the interactive OIDC and GitHub login
// paths already do for the same conditions.
//
// The difference matters during an incident. DecisionDenied plus no_grants
// asserts that a subject is not entitled to anything; if a grant store or an
// IdP's key endpoint is down, that claim is false for every user at once, and
// an operator querying the audit trail would see a mass entitlement failure
// instead of an outage.
var unavailableBearerDenialReasons = map[string]struct{}{
	"grant_resolution_unavailable": {},
	"jwks_fetch_failure":           {},
}

// bearerDenialReasonCode returns the bounded reason code for a credential
// resolution failure, or the empty string when the error carries no recognized
// outcome. The caller falls back to the generic reason in that case.
func bearerDenialReasonCode(err error) string {
	var carrier bearerDenialOutcomeCarrier
	if !errors.As(err, &carrier) {
		return ""
	}
	outcome := strings.TrimSpace(carrier.DenialOutcome())
	if _, ok := auditableBearerDenialReasons[outcome]; !ok {
		return ""
	}
	return outcome
}

// recordBearerResolutionDenied records a credential-resolution denial with the
// resolver's specific outcome when it reports one, so the audit trail can tell
// an expired token from a wrong audience from a bad signature (#5567). A
// resolver that reports nothing recognized keeps the previous generic reason,
// leaving those paths byte-identical.
func recordBearerResolutionDenied(r *http.Request, audit GovernanceAuditAppender, err error) {
	reason := bearerDenialReasonCode(err)
	if reason == "" {
		recordReadAuthorizationDenied(r, audit)
		return
	}
	if _, unavailable := unavailableBearerDenialReasons[reason]; unavailable {
		recordReadAuthorizationUnavailable(r, audit, reason)
		return
	}
	recordReadAuthorizationDeniedWithReason(r, audit, reason)
}

// recordReadAuthorizationUnavailable records a read-authorization event whose
// cause was a dependency that could not answer. It is the same event shape as
// the denial helpers with DecisionUnavailable instead of DecisionDenied, so an
// operator can separate "we turned this credential away" from "we could not
// tell".
func recordReadAuthorizationUnavailable(
	r *http.Request,
	audit GovernanceAuditAppender,
	reasonCode string,
) {
	if audit == nil {
		return
	}
	event := governanceaudit.Event{
		Type:          governanceaudit.EventTypeReadAuthorization,
		ActorClass:    governanceaudit.ActorClassAnonymous,
		ScopeClass:    governanceaudit.ScopeClassAdmin,
		Decision:      governanceaudit.DecisionUnavailable,
		ReasonCode:    strings.TrimSpace(reasonCode),
		CorrelationID: safeAuditCorrelationID(documentationCorrelationID(r)),
		OccurredAt:    time.Now().UTC(),
	}
	ctx, cancel := context.WithTimeout(r.Context(), governanceAuditAppendTimeout)
	defer cancel()
	_ = audit.Append(ctx, []governanceaudit.Event{event})
}

func recordScopedRouteAuthorizationDenied(
	r *http.Request,
	audit GovernanceAuditAppender,
	auth AuthContext,
) {
	if audit == nil {
		return
	}
	actorClass := governanceaudit.ActorClassScopedToken
	if auth.SubjectIDHash == "" {
		actorClass = governanceaudit.ActorClassAnonymous
	}
	event := governanceaudit.Event{
		Type:               governanceaudit.EventTypeReadAuthorization,
		ActorClass:         actorClass,
		ActorIDHash:        auth.SubjectIDHash,
		ScopeClass:         governanceaudit.ScopeClassAdmin,
		Decision:           governanceaudit.DecisionDenied,
		ReasonCode:         "scoped_route_not_enabled",
		CorrelationID:      safeAuditCorrelationID(documentationCorrelationID(r)),
		PolicyRevisionHash: auth.PolicyRevisionHash,
		OccurredAt:         time.Now().UTC(),
	}
	ctx, cancel := context.WithTimeout(r.Context(), governanceAuditAppendTimeout)
	defer cancel()
	_ = audit.Append(ctx, []governanceaudit.Event{event})
}

// recordScopedReadAuthorized records the F-9 (#5170) allowed-read
// governance-audit event for a resolver-success scoped-token or OIDC-bearer
// MCP/API read, mirroring the ALLOWED counterpart of
// recordScopedRouteAuthorizationDenied above. It is a sibling of the denial
// helpers, but deliberately does NOT wrap the append in the
// governanceAuditAppendTimeout context used by the synchronous denial
// helpers: allowedAudit is a governanceauditasync.AsyncAppender in
// production, whose Append call never blocks on the sink (sub-microsecond
// buffered-channel send), so the 500ms request-scoped timeout wrapper the
// denial helpers need for a real synchronous Postgres call would be
// unnecessary overhead here.
//
// allowedAudit is nil for every constructor except the mcp-server transport
// middleware (see cmd/mcp-server/wiring.go), so this is a no-op — byte
// identical to today — everywhere else, including cmd/api and the
// /api/v0/* authedHandler mcp-server also builds.
func recordScopedReadAuthorized(r *http.Request, allowedAudit GovernanceAuditAppender, auth AuthContext) {
	if allowedAudit == nil {
		return
	}
	// Mirror recordScopedRouteAuthorizationDenied's empty-hash guard exactly:
	// a scoped token can resolve ok=true with an empty SubjectIDHash
	// (scopedtoken/registry.go's validOptionalAuditHash accepts empty, and
	// normalizeAuthContext never fills it). ActorClassScopedToken with an
	// empty ActorIDHash fails NormalizeEvent's actor_identity check, and
	// because the durable store Append is all-or-nothing that one invalid
	// event would take its whole flush batch of well-formed allowed-read
	// events down with it in the async appender's drain. Downgrading to
	// anonymous keeps the event valid.
	actorClass := governanceaudit.ActorClassScopedToken
	if auth.SubjectIDHash == "" {
		actorClass = governanceaudit.ActorClassAnonymous
	}
	event := governanceaudit.Event{
		Type:               governanceaudit.EventTypeReadAuthorization,
		ActorClass:         actorClass,
		ActorIDHash:        auth.SubjectIDHash,
		ScopeClass:         governanceaudit.ScopeClassAdmin,
		Decision:           governanceaudit.DecisionAllowed,
		ReasonCode:         "scoped_read_allowed",
		CorrelationID:      safeAuditCorrelationID(documentationCorrelationID(r)),
		PolicyRevisionHash: auth.PolicyRevisionHash,
		OccurredAt:         time.Now().UTC(),
		TenantID:           auth.TenantID,
		WorkspaceID:        auth.WorkspaceID,
	}
	_ = allowedAudit.Append(r.Context(), []governanceaudit.Event{event})
}

func safeAuditCorrelationID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 96 {
		return ""
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') &&
			r != '_' && r != '-' && r != ':' {
			return ""
		}
	}
	return value
}
