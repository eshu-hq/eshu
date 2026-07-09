// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// audit emits one governance audit event for a mutation decision, deriving
// the actor class and actor id hash from the request's AuthContext. It is a
// no-op when no appender is wired.
func (h *AdminProviderConfigMutationHandler) audit(
	r *http.Request,
	eventType governanceaudit.EventType,
	decision governanceaudit.Decision,
	reasonCode string,
	actorIDHash string,
) {
	if h == nil || h.Audit == nil {
		return
	}
	auth, _ := AuthContextFromContext(r.Context())
	auth = normalizeAuthContext(auth)
	actorClass := localIdentityActorClass(auth)
	if actorIDHash == "" {
		actorIDHash = auth.SubjectIDHash
	}
	if actorIDHash == "" && actorClass == governanceaudit.ActorClassSharedToken {
		actorIDHash = sharedAdminActorIDHash
	}
	event := governanceaudit.Event{
		Type:               eventType,
		ActorClass:         actorClass,
		ActorIDHash:        actorIDHash,
		ScopeClass:         governanceaudit.ScopeClassAdmin,
		Decision:           decision,
		ReasonCode:         strings.TrimSpace(reasonCode),
		CorrelationID:      safeAuditCorrelationID(documentationCorrelationID(r)),
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

func providerConfigWriteResponse(result AdminProviderConfigWriteResult) map[string]any {
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
	case errors.Is(err, ErrAdminProviderConfigDuplicateKey):
		return "provider_config_duplicate_key"
	case errors.Is(err, ErrAdminProviderConfigKeyringUnavailable):
		return "provider_config_keyring_unavailable"
	case errors.Is(err, ErrAdminProviderConfigRevisionNotFound):
		return "provider_config_revision_not_found"
	case errors.Is(err, ErrAdminProviderConfigKindMismatch):
		return "provider_config_kind_mismatch"
	case errors.Is(err, ErrAdminProviderConfigRevisionChanged):
		return "provider_config_revision_changed"
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
	case errors.Is(err, ErrAdminProviderConfigDuplicateKey):
		WriteError(w, http.StatusConflict, "a provider config already exists for this tenant, kind, and identity key")
	case errors.Is(err, ErrAdminProviderConfigKeyringUnavailable):
		WriteError(w, http.StatusServiceUnavailable, "provider secret encryption is not configured on this deployment")
	case errors.Is(err, ErrAdminProviderConfigRevisionNotFound):
		WriteError(w, http.StatusNotFound, "revision not found")
	case errors.Is(err, ErrAdminProviderConfigKindMismatch):
		WriteError(w, http.StatusBadRequest, "provider_kind does not match the existing provider config")
	case errors.Is(err, ErrAdminProviderConfigRevisionChanged):
		WriteError(w, http.StatusConflict, "the provider config's active revision changed since it was tested; run test-connection again and retry enable")
	default:
		slog.Error("admin provider config write failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "failed to write provider config")
	}
}
