// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// SignInPolicyMutationHandler serves the admin sign-in policy write route
// (epic #4962, issue #4968). Every request requires all-scope admin
// authentication, writes strictly within the caller's own tenant (derived
// from AuthContext, never a request body), and emits a governance audit
// event for both the allowed and denied outcome — including a guardrail
// rejection, so an operator can see every attempt to enable require_sso
// without a proven provider or SSO admin proof, not just successful changes.
type SignInPolicyMutationHandler struct {
	Store SignInPolicyMutationStore
	Audit GovernanceAuditAppender
	// Instruments records the require_sso guardrail decision
	// (eshu_dp_auth_sign_in_policy_guardrail_total). Optional: nil-safe, a
	// handler built without it simply skips the counter increment.
	Instruments *telemetry.Instruments
	Now         func() time.Time
}

// Mount registers the admin sign-in policy mutation route.
func (h *SignInPolicyMutationHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("PATCH /api/v0/auth/admin/sign-in-policy", h.handleUpdate)
}

func (h *SignInPolicyMutationHandler) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func (h *SignInPolicyMutationHandler) storeReady(w http.ResponseWriter, r *http.Request) bool {
	if h == nil || h.Store == nil {
		if h != nil {
			h.audit(r, governanceaudit.DecisionDenied, "sign_in_policy_store_unavailable", "")
		}
		WriteError(w, http.StatusServiceUnavailable, "sign-in policy store is unavailable")
		return false
	}
	return true
}

func (h *SignInPolicyMutationHandler) adminScope(w http.ResponseWriter, r *http.Request) (tenantID string, ok bool) {
	auth, found := AuthContextFromContext(r.Context())
	auth = normalizeAuthContext(auth)
	if !found || !auth.AllScopes {
		h.audit(r, governanceaudit.DecisionDenied, "admin_scope_required", "")
		WriteError(w, http.StatusForbidden, "all-scope admin authentication is required")
		return "", false
	}
	if auth.TenantID == "" {
		h.audit(r, governanceaudit.DecisionDenied, "admin_tenant_required", "")
		WriteError(w, http.StatusForbidden, "admin tenant scope is required")
		return "", false
	}
	return auth.TenantID, true
}

func (h *SignInPolicyMutationHandler) requirePermission(w http.ResponseWriter, r *http.Request) bool {
	if authContextAllowsPermissionFeature(r.Context(), permissionFeatureIdentityAdmin) {
		return true
	}
	h.audit(r, governanceaudit.DecisionDenied, "permission_catalog_denied", "")
	writePermissionDeniedEnvelope(w, "identity_admin.sign_in_policy_update")
	return false
}

// signInPolicyUpdateRequestBody is the JSON body for the admin sign-in policy
// PATCH route. Every field is optional; an absent field leaves the current
// value unchanged.
type signInPolicyUpdateRequestBody struct {
	RequireSSO             *bool `json:"require_sso,omitempty"`
	AllowLocalUserCreation *bool `json:"allow_local_user_creation,omitempty"`
	RequireMFAForAllUsers  *bool `json:"require_mfa_for_all_users,omitempty"`
	IdleTimeoutSeconds     *int  `json:"idle_timeout_seconds,omitempty"`
	AbsoluteTimeoutSeconds *int  `json:"absolute_timeout_seconds,omitempty"`
}

// signInPolicyMinNonZeroTimeoutSeconds is the smallest accepted non-zero
// idle_timeout_seconds/absolute_timeout_seconds value on the admin PATCH
// route. Zero is exempt (it means "use the process default"); any other
// value below this floor is rejected rather than silently persisted, since
// storage otherwise clamps out-of-range values only at read time
// (resolveSessionTimeouts), letting an absurd or negative write sit
// unnoticed until the next session is issued.
const signInPolicyMinNonZeroTimeoutSeconds = 60

// validateSignInPolicyTimeouts rejects a PATCH body's idle/absolute timeout
// fields that would make a nonsensical or unusable session timeout: negative
// values, a non-zero value below signInPolicyMinNonZeroTimeoutSeconds, or an
// absolute timeout shorter than the idle timeout when both are set to
// non-zero overrides in the same request. It intentionally does not compare
// against a value already stored for the tenant (a partial update that only
// sets one field cannot see the other without an extra read) — that
// cross-request comparison is validateSignInPolicyMergedTimeouts's job.
func validateSignInPolicyTimeouts(body signInPolicyUpdateRequestBody) error {
	if body.IdleTimeoutSeconds != nil {
		if err := validateSignInPolicyTimeoutSeconds(*body.IdleTimeoutSeconds); err != nil {
			return fmt.Errorf("idle_timeout_seconds %w", err)
		}
	}
	if body.AbsoluteTimeoutSeconds != nil {
		if err := validateSignInPolicyTimeoutSeconds(*body.AbsoluteTimeoutSeconds); err != nil {
			return fmt.Errorf("absolute_timeout_seconds %w", err)
		}
	}
	if body.IdleTimeoutSeconds != nil && body.AbsoluteTimeoutSeconds != nil &&
		signInPolicyAbsoluteBeforeIdle(*body.IdleTimeoutSeconds, *body.AbsoluteTimeoutSeconds) {
		return errors.New("absolute_timeout_seconds must not be less than idle_timeout_seconds")
	}
	return nil
}

// signInPolicyAbsoluteBeforeIdle reports the one invalid session-timeout
// ordering this route ever rejects: a non-zero absolute timeout shorter than
// a non-zero idle timeout. Zero means "use the process default" on both
// fields and never participates in the comparison, matching
// resolveSessionTimeouts's zero-means-default convention. Shared by
// validateSignInPolicyTimeouts (same-request pair) and
// validateSignInPolicyMergedTimeouts (stored+incoming pair) so both checks
// enforce the identical rule.
func signInPolicyAbsoluteBeforeIdle(idleSeconds, absoluteSeconds int) bool {
	return idleSeconds > 0 && absoluteSeconds > 0 && absoluteSeconds < idleSeconds
}

// mergedSignInPolicyTimeoutSeconds resolves the idle/absolute timeout pair
// that would be persisted once body is applied on top of the tenant's
// currently stored policy: the incoming value where the PATCH body sets the
// field, the stored value otherwise.
func mergedSignInPolicyTimeoutSeconds(body signInPolicyUpdateRequestBody, stored SignInPolicy) (idleSeconds, absoluteSeconds int) {
	idleSeconds = stored.IdleTimeoutSeconds
	if body.IdleTimeoutSeconds != nil {
		idleSeconds = *body.IdleTimeoutSeconds
	}
	absoluteSeconds = stored.AbsoluteTimeoutSeconds
	if body.AbsoluteTimeoutSeconds != nil {
		absoluteSeconds = *body.AbsoluteTimeoutSeconds
	}
	return idleSeconds, absoluteSeconds
}

// validateSignInPolicyMergedTimeouts closes issue #5002 part 2: a stored
// absolute_timeout_seconds < idle_timeout_seconds produced across two
// separate PATCHes (idle=3600 in one request, absolute=1800 in a later one)
// is invisible to validateSignInPolicyTimeouts, which only sees one request
// body at a time. This merges body onto stored (mergedSignInPolicyTimeoutSeconds)
// and applies the same absolute>=idle rule to the pair that would actually be
// persisted. A no-op when neither timeout field is present in the body, so
// callers only pay for a store read when a timeout field is actually being
// changed.
func validateSignInPolicyMergedTimeouts(body signInPolicyUpdateRequestBody, stored SignInPolicy) error {
	if body.IdleTimeoutSeconds == nil && body.AbsoluteTimeoutSeconds == nil {
		return nil
	}
	idleSeconds, absoluteSeconds := mergedSignInPolicyTimeoutSeconds(body, stored)
	if signInPolicyAbsoluteBeforeIdle(idleSeconds, absoluteSeconds) {
		return errors.New("absolute_timeout_seconds must not be less than idle_timeout_seconds (comparing against the currently stored value)")
	}
	return nil
}

// validateSignInPolicyTimeoutSeconds validates one idle/absolute timeout
// field value: 0 ("use the process default") and any value at or above
// signInPolicyMinNonZeroTimeoutSeconds are valid; negative values and a
// non-zero value below the floor are rejected.
func validateSignInPolicyTimeoutSeconds(seconds int) error {
	if seconds < 0 {
		return errors.New("must not be negative")
	}
	if seconds > 0 && seconds < signInPolicyMinNonZeroTimeoutSeconds {
		return fmt.Errorf("must be 0 or at least %d seconds", signInPolicyMinNonZeroTimeoutSeconds)
	}
	return nil
}

func (h *SignInPolicyMutationHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w, r) {
		return
	}
	if !h.requirePermission(w, r) {
		return
	}
	tenantID, ok := h.adminScope(w, r)
	if !ok {
		return
	}
	var body signInPolicyUpdateRequestBody
	if err := ReadJSON(r, &body); err != nil {
		h.audit(r, governanceaudit.DecisionDenied, "sign_in_policy_invalid_request", "")
		WriteError(w, http.StatusBadRequest, "invalid sign-in policy request")
		return
	}
	if err := validateSignInPolicyTimeouts(body); err != nil {
		h.audit(r, governanceaudit.DecisionDenied, "sign_in_policy_invalid_timeout", "")
		WriteError(w, http.StatusBadRequest, "invalid sign-in policy request: "+err.Error())
		return
	}
	if body.IdleTimeoutSeconds != nil || body.AbsoluteTimeoutSeconds != nil {
		stored, err := h.Store.GetSignInPolicy(r.Context(), tenantID)
		if err != nil {
			slog.ErrorContext(r.Context(), "sign-in policy merged-timeout read failed", "err", err)
			h.audit(r, governanceaudit.DecisionDenied, "sign_in_policy_read_failed", "")
			WriteError(w, http.StatusInternalServerError, "failed to read sign-in policy")
			return
		}
		if err := validateSignInPolicyMergedTimeouts(body, stored); err != nil {
			h.audit(r, governanceaudit.DecisionDenied, "sign_in_policy_invalid_timeout", "")
			WriteError(w, http.StatusBadRequest, "invalid sign-in policy request: "+err.Error())
			return
		}
	}
	update := SignInPolicyUpdateRequest(body)
	policyRevisionHash := localIdentityPolicyRevision(tenantID, "")
	now := h.now()

	policy, err := h.Store.UpsertSignInPolicy(r.Context(), tenantID, update, policyRevisionHash, now)
	if err != nil {
		h.handleUpdateError(w, r, body.RequireSSO != nil && *body.RequireSSO, err)
		return
	}
	if body.RequireSSO != nil && *body.RequireSSO {
		h.recordGuardrailOutcome(r.Context(), "allowed")
	}
	h.audit(r, governanceaudit.DecisionAllowed, "sign_in_policy_updated", "")
	WriteJSON(w, http.StatusOK, signInPolicyDetailJSON(policy))
}

// handleUpdateError maps a store error to the right HTTP status, distinguishing
// a require_sso guardrail rejection (400, with a message the console surfaces
// directly to the admin) from any other failure (500).
func (h *SignInPolicyMutationHandler) handleUpdateError(w http.ResponseWriter, r *http.Request, requestedRequireSSO bool, err error) {
	switch {
	case errors.Is(err, ErrSignInPolicyGuardrailNoProvenProvider):
		if requestedRequireSSO {
			h.recordGuardrailOutcome(r.Context(), "denied_no_provider")
		}
		h.audit(r, governanceaudit.DecisionDenied, "sign_in_policy_guardrail_no_provider", "")
		WriteError(w, http.StatusBadRequest, "require_sso cannot be enabled: no provider config has a passing connection test")
	case errors.Is(err, ErrSignInPolicyGuardrailNoSSOAdminProof):
		if requestedRequireSSO {
			h.recordGuardrailOutcome(r.Context(), "denied_no_sso_proof")
		}
		h.audit(r, governanceaudit.DecisionDenied, "sign_in_policy_guardrail_no_sso_proof", "")
		WriteError(w, http.StatusBadRequest, "require_sso cannot be enabled: no admin has signed in via SSO yet")
	default:
		slog.ErrorContext(r.Context(), "admin sign-in policy update failed", "err", err)
		h.audit(r, governanceaudit.DecisionDenied, "sign_in_policy_update_failed", "")
		WriteError(w, http.StatusInternalServerError, "failed to update sign-in policy")
	}
}

// recordGuardrailOutcome increments AuthSignInPolicyGuardrailTotal, the OTEL
// signal on the require_sso enforcement path (epic #4962, issue #4968). A
// nil h.Instruments (handler built without telemetry wiring, e.g. in a unit
// test) is a no-op.
func (h *SignInPolicyMutationHandler) recordGuardrailOutcome(ctx context.Context, decision string) {
	if h == nil || h.Instruments == nil || h.Instruments.AuthSignInPolicyGuardrailTotal == nil {
		return
	}
	h.Instruments.AuthSignInPolicyGuardrailTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionDecision, decision),
	))
}

func (h *SignInPolicyMutationHandler) audit(
	r *http.Request,
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
		Type:               governanceaudit.EventTypeIDPConfigChange,
		ActorClass:         actorClass,
		ActorIDHash:        actorIDHash,
		ScopeClass:         governanceaudit.ScopeClassTenant,
		Decision:           decision,
		ReasonCode:         reasonCode,
		CorrelationID:      safeAuditCorrelationID(documentationCorrelationID(r)),
		PolicyRevisionHash: auth.PolicyRevisionHash,
		OccurredAt:         h.now(),
		TenantID:           auth.TenantID,
		WorkspaceID:        auth.WorkspaceID,
	}
	if err := h.Audit.Append(r.Context(), []governanceaudit.Event{event}); err != nil {
		slog.ErrorContext(
			r.Context(), "governance audit append failed",
			"err", err,
			"event_type", string(event.Type),
			"decision", string(decision),
			"reason_code", reasonCode,
		)
	}
}
