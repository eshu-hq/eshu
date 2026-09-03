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

// scopedRouteDenialSignal records that the auth middleware refused THIS
// request on route admission -- scopedBearerRouteDenialReason or
// browserSessionRouteDenialReason returned a reason code and
// scopedRouteDeniedResponse wrote the 403 -- rather than on credential
// resolution. It is a pointer carried on the request context so a mutation
// inside the middleware is visible to the caller that installed it after
// ServeHTTP returns, even though context values themselves are immutable. It
// is the same shape go/internal/mcp's denyClassification uses, and for the
// same reason: the observer sits outside the handler that knows the answer.
type scopedRouteDenialSignal struct{ denied bool }

type scopedRouteDenialSignalCtxKey struct{}

// WithScopedRouteDenialSignal derives a context carrying a fresh route-denial
// signal and returns it with the predicate that reads it back. The predicate
// reports whether the auth middleware refused the request on route admission;
// it is false for an admitted request and for a 401 that never got as far as
// route admission.
//
// It exists for one caller: go/internal/mcp's authenticatedTransportHandler,
// which labels eshu_dp_mcp_transport_auth_denied_total and, without this,
// classifies every unmarked 401/403 as reason="unauthenticated". An all-scope
// bearer refused on GET /sse or POST /mcp/message under hosted_multi_tenant is
// an authorization refusal, not a failed authentication, and an operator
// paging on an authentication-failure spike must not be woken by a governance
// mode working exactly as configured.
//
// The dependency has to run this way round. go/internal/mcp imports
// go/internal/query, never the reverse, so query cannot mark a sentinel that
// mcp defines; mcp installs the sentinel query defines, and query marks it.
// A response header would be the other option and is worse: it would put an
// internal admission fact on the wire for every refused MCP client to read.
//
// Installing this is optional. cmd/api does not, and the mark below is a
// no-op with no signal on the context, so the refusal path costs one type
// assertion on a request that is already being refused.
//
// The predicate must be called from the same goroutine that served the
// request, after ServeHTTP returns. Both the write and the read happen on the
// request goroutine, so there is no cross-goroutine visibility question to
// answer.
func WithScopedRouteDenialSignal(ctx context.Context) (context.Context, func() bool) {
	signal := &scopedRouteDenialSignal{}
	return context.WithValue(ctx, scopedRouteDenialSignalCtxKey{}, signal),
		func() bool { return signal.denied }
}

// markScopedRouteDenied sets the signal WithScopedRouteDenialSignal installed,
// if any. scopedRouteDeniedResponse calls it, which is the single choke point
// every route-admission refusal goes through -- the scoped-bearer branch and
// the browser-session branch of authMiddlewareWithRoutePolicy both write their
// 403 there -- so the signal cannot drift out of step with the response the
// caller actually received.
func markScopedRouteDenied(ctx context.Context) {
	if signal, ok := ctx.Value(scopedRouteDenialSignalCtxKey{}).(*scopedRouteDenialSignal); ok {
		signal.denied = true
	}
}

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

// recordScopedRouteAuthorizationDeniedWithReason is the only scoped-route
// denial recorder, mirroring recordReadAuthorizationDeniedWithReason above.
// Both admission paths use it because, since #6450, a cookie caller is refused
// for two genuinely different causes and an operator has to be able to tell
// them apart; the scoped-bearer path joined them when #6450's residual item 1
// closed and scopedBearerRouteDenialReason started producing both codes too.
// See the reason-code constants in auth_browser_session_route_policy.go. It
// replaced a fixed-code wrapper, recordScopedRouteAuthorizationDenied, which
// lost its last caller in that change. A blank or whitespace-only code falls
// back to scopedRouteDeniedUnspecifiedReason rather than emitting an event
// NormalizeEvent would reject: the durable GovernanceAuditStore.Append
// normalizes all-or-nothing and returns before any INSERT, and the async
// appender's per-event fallback (#5170) isolates the bad event from its batch
// siblings, so the cost of emitting one would be a single lost event rather
// than its whole batch. The fallback is distinct from both real codes on
// purpose, so a caller that passes a blank code is visible in the audit
// instead of being mislabelled as one of them.
//
// The event carries the caller's tenant and workspace, matching
// recordScopedReadAuthorized below: a tenant admin's governance-audit read is
// filtered by tenant_id, so a denial recorded without one is a denial only the
// shared operator can ever see.
func recordScopedRouteAuthorizationDeniedWithReason(
	r *http.Request,
	audit GovernanceAuditAppender,
	auth AuthContext,
	reasonCode string,
) {
	if audit == nil {
		return
	}
	// The closed governanceaudit.ActorClass enum (governanceaudit/audit.go)
	// has no browser-session member, so a cookie-session denial is stamped
	// scoped_token on purpose. Read it as "identity-resolved caller", not
	// "bearer token". Do not "correct" it to ActorClassOperator: that member
	// means a human operator carrying no direct identifier, and this helper is
	// shared with the scoped-bearer denial path in
	// authMiddlewareWithRoutePolicy, where scoped_token is literally right --
	// including for scoped_route_all_scope_grant_required, which since #6450's
	// residual item 1 closed is emitted for an all-scope bearer as well as an
	// all-scope cookie session. Widening the enum with a browser-session
	// member is tracked in #6459.
	actorClass := governanceaudit.ActorClassScopedToken
	if auth.SubjectIDHash == "" {
		actorClass = governanceaudit.ActorClassAnonymous
	}
	reasonCode = strings.TrimSpace(reasonCode)
	if reasonCode == "" {
		reasonCode = scopedRouteDeniedUnspecifiedReason
	}
	event := governanceaudit.Event{
		Type:               governanceaudit.EventTypeReadAuthorization,
		ActorClass:         actorClass,
		ActorIDHash:        auth.SubjectIDHash,
		ScopeClass:         governanceaudit.ScopeClassAdmin,
		Decision:           governanceaudit.DecisionDenied,
		ReasonCode:         reasonCode,
		CorrelationID:      safeAuditCorrelationID(documentationCorrelationID(r)),
		PolicyRevisionHash: auth.PolicyRevisionHash,
		OccurredAt:         time.Now().UTC(),
		TenantID:           auth.TenantID,
		WorkspaceID:        auth.WorkspaceID,
	}
	ctx, cancel := context.WithTimeout(r.Context(), governanceAuditAppendTimeout)
	defer cancel()
	_ = audit.Append(ctx, []governanceaudit.Event{event})
}

// recordScopedReadAuthorized records the F-9 (#5170) allowed-read
// governance-audit event for a resolver-success scoped-token or OIDC-bearer
// MCP/API read, mirroring the ALLOWED counterpart of
// recordScopedRouteAuthorizationDeniedWithReason above. It is a sibling of the denial
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
	// Mirror the denial helper's empty-hash guard exactly:
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
