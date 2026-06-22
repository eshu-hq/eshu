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
