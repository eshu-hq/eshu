// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
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
	event := governanceaudit.Event{
		Type:               governanceaudit.EventTypeReadAuthorization,
		ActorClass:         governanceaudit.ActorClassScopedToken,
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
