package query

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// AdminIdentityMutationHandler serves tenant-scoped admin write endpoints for the
// console admin UX: invitation revoke, role-assignment grant/revoke, and IdP
// group->role mapping create/delete.
//
// Every route requires all-scope admin authentication and writes strictly within
// the caller's own tenant/workspace, derived from AuthContext (never from a
// request body). Every allowed and denied mutation emits a governance audit
// event. No route accepts or returns a secret, invite code, credential handle,
// or raw external group name. Writes are idempotent under retry via active-row
// conflict keys; CSRF protection for browser-session callers is enforced by the
// auth middleware ahead of these handlers (POST/DELETE are unsafe methods).
type AdminIdentityMutationHandler struct {
	Store AdminIdentityMutationStore
	Audit GovernanceAuditAppender
}

// Mount registers the admin identity mutation routes.
func (h *AdminIdentityMutationHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/auth/local/invitations/{invite_id}/revoke", h.handleRevokeInvitation)
	mux.HandleFunc("POST /api/v0/auth/admin/role-assignments", h.handleGrantRoleAssignment)
	mux.HandleFunc("POST /api/v0/auth/admin/role-assignments/revoke", h.handleRevokeRoleAssignment)
	mux.HandleFunc("POST /api/v0/auth/admin/idp-group-mappings", h.handleCreateIdPGroupMapping)
	mux.HandleFunc("DELETE /api/v0/auth/admin/idp-group-mappings/{mapping_ref}", h.handleDeleteIdPGroupMapping)
}

// storeReady reports whether the mutation store is wired, writing 503 when not.
func (h *AdminIdentityMutationHandler) storeReady(w http.ResponseWriter) bool {
	if h == nil || h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "admin identity mutation store is unavailable")
		return false
	}
	return true
}

// adminScope resolves the all-scope admin caller's tenant/workspace, writing the
// appropriate error and returning ok=false when the caller is not an all-scope
// admin or carries no tenant. A denied caller is audited so the read side can
// surface denied attempts. It mirrors AdminIdentityReadHandler.adminScope, with
// an additional denied-decision audit event for the write surface.
func (h *AdminIdentityMutationHandler) adminScope(
	w http.ResponseWriter,
	r *http.Request,
	eventType governanceaudit.EventType,
) (tenantID, workspaceID string, ok bool) {
	auth, found := AuthContextFromContext(r.Context())
	auth = normalizeAuthContext(auth)
	if !found || !auth.AllScopes {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "admin_scope_required", "")
		WriteError(w, http.StatusForbidden, "all-scope admin authentication is required")
		return "", "", false
	}
	if auth.TenantID == "" {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "admin_tenant_required", "")
		WriteError(w, http.StatusForbidden, "admin tenant scope is required")
		return "", "", false
	}
	return auth.TenantID, auth.WorkspaceID, true
}

// resolveWorkspace narrows the caller's AuthContext workspace by an optional
// request workspace_id. A blank request workspace keeps the AuthContext
// workspace; a non-blank request workspace must match it, so a tenant admin can
// never mutate another workspace by passing a different id. Returns ok=false on
// mismatch.
func resolveWorkspace(authWorkspaceID, requestWorkspaceID string) (string, bool) {
	requestWorkspaceID = strings.TrimSpace(requestWorkspaceID)
	if requestWorkspaceID == "" {
		return authWorkspaceID, true
	}
	if requestWorkspaceID != authWorkspaceID {
		return "", false
	}
	return requestWorkspaceID, true
}

func (h *AdminIdentityMutationHandler) handleRevokeInvitation(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	// EventTypeRoleGrantChange covers invitation revocation: no
	// invitation-specific event type exists in the governance audit catalog.
	const eventType = governanceaudit.EventTypeRoleGrantChange
	tenantID, workspaceID, ok := h.adminScope(w, r, eventType)
	if !ok {
		return
	}
	inviteID := strings.TrimSpace(PathParam(r, "invite_id"))
	if inviteID == "" {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "invitation_id_required", "")
		WriteError(w, http.StatusBadRequest, "invite_id is required")
		return
	}
	result, err := h.Store.RevokeAdminInvitation(r.Context(), AdminInvitationRevokeRequest{
		InviteID:    inviteID,
		TenantID:    tenantID,
		WorkspaceID: workspaceID,
		RevokedAt:   time.Now().UTC(),
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "admin revoke invitation failed", "err", err)
		h.audit(r, eventType, governanceaudit.DecisionDenied, "invitation_revoke_failed", "")
		WriteError(w, http.StatusInternalServerError, "failed to revoke invitation")
		return
	}
	if !result.Found {
		// No invitation with this id in the caller's tenant/workspace. Report
		// not-found rather than fabricating a revoked row.
		h.audit(r, eventType, governanceaudit.DecisionDenied, "invitation_not_found", "")
		WriteError(w, http.StatusNotFound, "invitation not found")
		return
	}
	reason := "invitation_revoked"
	if !result.Revoked {
		// Already revoked/accepted/expired: idempotent no-op.
		reason = "invitation_revoke_noop"
	}
	h.audit(r, eventType, governanceaudit.DecisionAllowed, reason, "")
	WriteJSON(w, http.StatusOK, map[string]any{
		"invite_id": inviteID,
		"status":    result.Status,
		"revoked":   result.Revoked,
	})
}

func (h *AdminIdentityMutationHandler) handleGrantRoleAssignment(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	const eventType = governanceaudit.EventTypeRoleGrantChange
	tenantID, authWorkspaceID, ok := h.adminScope(w, r, eventType)
	if !ok {
		return
	}
	var req adminRoleAssignmentRequest
	if err := ReadJSON(r, &req); err != nil {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "role_assignment_invalid_request", "")
		WriteError(w, http.StatusBadRequest, "invalid role assignment request")
		return
	}
	userID := strings.TrimSpace(req.UserID)
	roleID := strings.TrimSpace(req.RoleID)
	if userID == "" || roleID == "" {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "role_assignment_missing_fields", "")
		WriteError(w, http.StatusBadRequest, "user_id and role_id are required")
		return
	}
	workspaceID, wsOK := resolveWorkspace(authWorkspaceID, req.WorkspaceID)
	if !wsOK {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "role_assignment_workspace_mismatch", "")
		WriteError(w, http.StatusForbidden, "workspace_id must match the caller's workspace")
		return
	}
	result, err := h.Store.GrantAdminRoleAssignment(r.Context(), AdminRoleAssignmentGrantRequest{
		TenantID:           tenantID,
		WorkspaceID:        workspaceID,
		UserID:             userID,
		RoleID:             roleID,
		AssignmentSource:   "admin",
		PolicyRevisionHash: localIdentityPolicyRevision(tenantID, workspaceID),
		EffectiveAt:        time.Now().UTC(),
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "admin grant role assignment failed", "err", err)
		h.audit(r, eventType, governanceaudit.DecisionDenied, "role_assignment_grant_failed", "")
		WriteError(w, http.StatusInternalServerError, "failed to grant role assignment")
		return
	}
	if !result.RoleValid {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "role_assignment_unknown_role", "")
		WriteError(w, http.StatusBadRequest, "role does not exist or is not active in the tenant")
		return
	}
	if !result.UserValid {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "role_assignment_unknown_user", "")
		WriteError(w, http.StatusBadRequest, "user does not have an active tenant membership")
		return
	}
	h.audit(r, eventType, governanceaudit.DecisionAllowed, "role_assignment_granted", "")
	// changed reflects a fresh row insertion; reactivation of a previously revoked
	// assignment reports changed=false. Read status for the effective assignment state.
	WriteJSON(w, http.StatusOK, map[string]any{
		"user_id": userID,
		"role_id": roleID,
		"status":  result.Status,
		"changed": result.Changed,
	})
}

func (h *AdminIdentityMutationHandler) handleRevokeRoleAssignment(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	const eventType = governanceaudit.EventTypeRoleGrantChange
	tenantID, authWorkspaceID, ok := h.adminScope(w, r, eventType)
	if !ok {
		return
	}
	var req adminRoleAssignmentRequest
	if err := ReadJSON(r, &req); err != nil {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "role_assignment_invalid_request", "")
		WriteError(w, http.StatusBadRequest, "invalid role assignment request")
		return
	}
	userID := strings.TrimSpace(req.UserID)
	roleID := strings.TrimSpace(req.RoleID)
	if userID == "" || roleID == "" {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "role_assignment_missing_fields", "")
		WriteError(w, http.StatusBadRequest, "user_id and role_id are required")
		return
	}
	workspaceID, wsOK := resolveWorkspace(authWorkspaceID, req.WorkspaceID)
	if !wsOK {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "role_assignment_workspace_mismatch", "")
		WriteError(w, http.StatusForbidden, "workspace_id must match the caller's workspace")
		return
	}
	result, err := h.Store.RevokeAdminRoleAssignment(r.Context(), AdminRoleAssignmentRevokeRequest{
		TenantID:    tenantID,
		WorkspaceID: workspaceID,
		UserID:      userID,
		RoleID:      roleID,
		RevokedAt:   time.Now().UTC(),
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "admin revoke role assignment failed", "err", err)
		h.audit(r, eventType, governanceaudit.DecisionDenied, "role_assignment_revoke_failed", "")
		WriteError(w, http.StatusInternalServerError, "failed to revoke role assignment")
		return
	}
	reason := "role_assignment_revoked"
	if !result.Changed {
		reason = "role_assignment_revoke_noop"
	}
	h.audit(r, eventType, governanceaudit.DecisionAllowed, reason, "")
	WriteJSON(w, http.StatusOK, map[string]any{
		"user_id": userID,
		"role_id": roleID,
		"status":  result.Status,
		"changed": result.Changed,
	})
}

func (h *AdminIdentityMutationHandler) handleCreateIdPGroupMapping(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	const eventType = governanceaudit.EventTypeIDPConfigChange
	tenantID, authWorkspaceID, ok := h.adminScope(w, r, eventType)
	if !ok {
		return
	}
	var req adminIdPGroupMappingRequest
	if err := ReadJSON(r, &req); err != nil {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "idp_group_mapping_invalid_request", "")
		WriteError(w, http.StatusBadRequest, "invalid idp group mapping request")
		return
	}
	providerConfigID := strings.TrimSpace(req.ProviderConfigID)
	roleID := strings.TrimSpace(req.RoleID)
	externalGroup := strings.TrimSpace(req.ExternalGroup)
	if providerConfigID == "" || roleID == "" || externalGroup == "" {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "idp_group_mapping_missing_fields", "")
		WriteError(w, http.StatusBadRequest, "provider_config_id, external_group, and role_id are required")
		return
	}
	workspaceID, wsOK := resolveWorkspace(authWorkspaceID, req.WorkspaceID)
	if !wsOK {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "idp_group_mapping_workspace_mismatch", "")
		WriteError(w, http.StatusForbidden, "workspace_id must match the caller's workspace")
		return
	}
	// Hash the raw external group name with localIdentityHash, which is
	// byte-identical to oidclogin.SHA256Hash (the function the OIDC login path
	// uses to hash claim groups before reading identity_provider_group_role_mappings):
	// both return "sha256:"+hex(sha256(TrimSpace(value))) for a non-empty value,
	// and externalGroup is non-empty here. The in-package helper is used to avoid
	// importing oidclogin, which imports this package (an import cycle); the hash
	// the mapping is stored under therefore matches what login looks up. The raw
	// group name never reaches the store, response, or audit event.
	result, err := h.Store.CreateAdminIdPGroupMapping(r.Context(), AdminIdPGroupMappingCreateRequest{
		ProviderConfigID:   providerConfigID,
		ExternalGroupHash:  localIdentityHash(externalGroup),
		TenantID:           tenantID,
		WorkspaceID:        workspaceID,
		RoleID:             roleID,
		MappingSource:      "admin",
		PolicyRevisionHash: localIdentityPolicyRevision(tenantID, workspaceID),
		EffectiveAt:        time.Now().UTC(),
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "admin create idp group mapping failed", "err", err)
		h.audit(r, eventType, governanceaudit.DecisionDenied, "idp_group_mapping_create_failed", "")
		WriteError(w, http.StatusInternalServerError, "failed to create idp group mapping")
		return
	}
	if !result.ProviderValid {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "idp_group_mapping_unknown_provider", "")
		WriteError(w, http.StatusBadRequest, "provider config does not exist or is not active in the tenant")
		return
	}
	if !result.RoleValid {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "idp_group_mapping_unknown_role", "")
		WriteError(w, http.StatusBadRequest, "role does not exist or is not active in the tenant")
		return
	}
	h.audit(r, eventType, governanceaudit.DecisionAllowed, "idp_group_mapping_created", "")
	// created reflects a fresh row insertion; reactivation of a previously
	// revoked mapping reports created=false. Read status for the effective state.
	WriteJSON(w, http.StatusOK, map[string]any{
		"mapping_ref":        result.MappingRef,
		"provider_config_id": providerConfigID,
		"role_id":            roleID,
		"status":             result.Status,
		"created":            result.Created,
	})
}

func (h *AdminIdentityMutationHandler) handleDeleteIdPGroupMapping(w http.ResponseWriter, r *http.Request) {
	if !h.storeReady(w) {
		return
	}
	const eventType = governanceaudit.EventTypeIDPConfigChange
	tenantID, workspaceID, ok := h.adminScope(w, r, eventType)
	if !ok {
		return
	}
	mappingRef := strings.TrimSpace(PathParam(r, "mapping_ref"))
	if mappingRef == "" {
		h.audit(r, eventType, governanceaudit.DecisionDenied, "idp_group_mapping_ref_required", "")
		WriteError(w, http.StatusBadRequest, "mapping_ref is required")
		return
	}
	result, err := h.Store.DeleteAdminIdPGroupMapping(r.Context(), AdminIdPGroupMappingDeleteRequest{
		MappingRef:  mappingRef,
		TenantID:    tenantID,
		WorkspaceID: workspaceID,
		RevokedAt:   time.Now().UTC(),
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "admin delete idp group mapping failed", "err", err)
		h.audit(r, eventType, governanceaudit.DecisionDenied, "idp_group_mapping_delete_failed", "")
		WriteError(w, http.StatusInternalServerError, "failed to delete idp group mapping")
		return
	}
	reason := "idp_group_mapping_deleted"
	if !result.Found {
		// No active mapping matched the ref: idempotent no-op (already deleted
		// or never existed in this tenant/workspace).
		reason = "idp_group_mapping_delete_noop"
	}
	h.audit(r, eventType, governanceaudit.DecisionAllowed, reason, "")
	WriteJSON(w, http.StatusOK, map[string]any{
		"mapping_ref": mappingRef,
		"deleted":     result.Deleted,
	})
}

// audit emits one governance audit event for a mutation decision, deriving the
// actor class and actor id hash from the request's AuthContext. It is a no-op
// when no appender is wired.
func (h *AdminIdentityMutationHandler) audit(
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
	if actorIDHash == "" {
		actorIDHash = auth.SubjectIDHash
	}
	event := governanceaudit.Event{
		Type:               eventType,
		ActorClass:         localIdentityActorClass(auth),
		ActorIDHash:        actorIDHash,
		ScopeClass:         governanceaudit.ScopeClassAdmin,
		Decision:           decision,
		ReasonCode:         strings.TrimSpace(reasonCode),
		CorrelationID:      safeAuditCorrelationID(documentationCorrelationID(r)),
		PolicyRevisionHash: auth.PolicyRevisionHash,
		OccurredAt:         time.Now().UTC(),
	}
	// Do not fail the request on audit write failure: governance audit is
	// best-effort for the caller path. Log the error so an operator sees the gap.
	if err := h.Audit.Append(r.Context(), []governanceaudit.Event{event}); err != nil {
		slog.ErrorContext(r.Context(), "governance audit append failed",
			"err", err,
			"event_type", string(eventType),
			"decision", string(decision),
			"reason_code", reasonCode,
		)
	}
}

// adminRoleAssignmentRequest is the JSON body for the grant and revoke
// role-assignment routes. workspace_id is optional; when present it must match
// the caller's workspace.
type adminRoleAssignmentRequest struct {
	UserID      string `json:"user_id"`
	RoleID      string `json:"role_id"`
	WorkspaceID string `json:"workspace_id"`
}

// adminIdPGroupMappingRequest is the JSON body for creating a group->role
// mapping. external_group is the RAW external group name; the handler hashes it
// server-side and never stores or returns the raw value.
type adminIdPGroupMappingRequest struct {
	ProviderConfigID string `json:"provider_config_id"`
	ExternalGroup    string `json:"external_group"`
	RoleID           string `json:"role_id"`
	WorkspaceID      string `json:"workspace_id"`
}
