// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/admin/audit"
	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// audit emits one governance audit event for a mutation decision, deriving
// the actor class and actor id hash from the request's queryauth.AuthContext. It is a
// no-op when no appender is wired.
func (h *MutationHandler) audit(
	r *http.Request,
	eventType governanceaudit.EventType,
	decision governanceaudit.Decision,
	reasonCode string,
	actorIDHash string,
) {
	if h == nil || h.Audit == nil {
		return
	}
	auth, _ := queryauth.AuthContextFromContext(r.Context())
	auth = queryauth.NormalizeAuthContext(auth)
	actorClass := audit.ActorClassForAuth(auth)
	if actorIDHash == "" {
		actorIDHash = auth.SubjectIDHash
	}
	if actorIDHash == "" && actorClass == governanceaudit.ActorClassSharedToken {
		actorIDHash = audit.SharedActorIDHash
	}
	event := governanceaudit.Event{
		Type:               eventType,
		ActorClass:         actorClass,
		ActorIDHash:        actorIDHash,
		ScopeClass:         governanceaudit.ScopeClassAdmin,
		Decision:           decision,
		ReasonCode:         strings.TrimSpace(reasonCode),
		CorrelationID:      audit.SafeCorrelationID(audit.CorrelationID(r)),
		PolicyRevisionHash: auth.PolicyRevisionHash,
		OccurredAt:         time.Now().UTC(),
		TenantID:           auth.TenantID,
		WorkspaceID:        auth.WorkspaceID,
	}
	if err := h.Audit.Append(r.Context(), []governanceaudit.Event{event}); err != nil {
		slog.ErrorContext(
			r.Context(), "governance audit append failed",
			"err", err,
			"event_type", string(eventType),
			"decision", string(decision),
			"reason_code", reasonCode,
		)
	}
}

func providerConfigWriteResponse(result WriteResult) map[string]any {
	return map[string]any{
		"provider_config_id": result.ProviderConfigID,
		"revision_id":        result.RevisionID,
		"status":             result.Status,
		"changed":            result.Changed,
	}
}

// providerConfigWriteErrorReason maps a store error to a governance audit
// reason code.
func providerConfigWriteErrorReason(err error) string {
	switch {
	case errors.Is(err, ErrDuplicateKey):
		return "provider_config_duplicate_key"
	case errors.Is(err, ErrKeyringUnavailable):
		return "provider_config_keyring_unavailable"
	case errors.Is(err, ErrRevisionNotFound):
		return "provider_config_revision_not_found"
	case errors.Is(err, ErrKindMismatch):
		return "provider_config_kind_mismatch"
	case errors.Is(err, ErrRevisionChanged):
		return "provider_config_revision_changed"
	case errors.Is(err, ErrManagedByEnvironment):
		return "provider_config_managed_by_environment"
	default:
		return "provider_config_write_failed"
	}
}

// writeProviderConfigWriteError maps a store error to an HTTP response. It
// never includes the underlying error text in the response body (only in the
// server log via the caller's slog.ErrorContext, which callers should add for
// unmapped errors) — see individual handlers.
func writeProviderConfigWriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrDuplicateKey):
		querycontract.WriteError(w, http.StatusConflict, "a provider config already exists for this tenant, kind, and identity key")
	case errors.Is(err, ErrKeyringUnavailable):
		querycontract.WriteError(w, http.StatusServiceUnavailable, "provider secret encryption is not configured on this deployment")
	case errors.Is(err, ErrRevisionNotFound):
		querycontract.WriteError(w, http.StatusNotFound, "revision not found")
	case errors.Is(err, ErrKindMismatch):
		querycontract.WriteError(w, http.StatusBadRequest, "provider_kind does not match the existing provider config")
	case errors.Is(err, ErrRevisionChanged):
		querycontract.WriteError(w, http.StatusConflict, "the provider config's active revision changed since it was tested; run test-connection again and retry enable")
	case errors.Is(err, ErrManagedByEnvironment):
		querycontract.WriteError(w, http.StatusBadRequest, "managed by environment; edit in your IaC, not here")
	default:
		slog.Error("admin provider config write failed", "err", err)
		querycontract.WriteError(w, http.StatusInternalServerError, "failed to write provider config")
	}
}
