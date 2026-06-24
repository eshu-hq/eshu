package query

import (
	"net/http"
	"time"
)

// ProfileHandler serves GET /api/v0/auth/profile, returning an aggregated
// read-only view of the caller's identity, active context, MFA state, and
// memberships. It never exposes session hashes, token hashes, credential
// handles, or recovery codes.
type ProfileHandler struct {
	// LocalIdentityStore is the store used for MFA status lookups. It must
	// implement LocalIdentityProfileLister so GetLocalIdentityMFAStatus is
	// available. When nil, MFA status is omitted from the response.
	LocalIdentityStore LocalIdentityProfileLister
	// Now is an optional clock override for testing.
	Now func() time.Time
}

// Mount registers the profile route.
func (h *ProfileHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/auth/profile", h.handleProfile)
}

func (h *ProfileHandler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now().UTC()
}

func (h *ProfileHandler) handleProfile(w http.ResponseWriter, r *http.Request) {
	auth, ok := AuthContextFromContext(r.Context())
	if !ok {
		unauthorizedResponse(w, r)
		return
	}
	auth = normalizeAuthContext(auth)
	if auth.SubjectIDHash == "" {
		unauthorizedResponse(w, r)
		return
	}

	now := h.now()

	// MFA status — safe fields only; never credential handle or hash.
	// Only fetched for sessions with an active tenant context; sessions
	// without a tenant are not associated with a local identity subject.
	type mfaJSON struct {
		HasActiveMFA bool   `json:"has_active_mfa"`
		FactorKind   string `json:"factor_kind,omitempty"`
	}
	var mfa mfaJSON
	if h.LocalIdentityStore != nil && auth.TenantID != "" {
		status, err := h.LocalIdentityStore.GetLocalIdentityMFAStatus(r.Context(), auth.SubjectIDHash, now)
		if err == nil {
			mfa.HasActiveMFA = status.HasActiveMFA
			mfa.FactorKind = status.FactorKind
		}
		// On error leave mfa at zero value — profile is best-effort for MFA
		// visibility and must not block the rest of the response.
	}

	// Memberships: return only the active tenant/workspace from the session.
	// A full per-subject membership query does not exist today; fabricating
	// rows would violate accuracy rules. Active context only.
	type membershipJSON struct {
		TenantID    string `json:"tenant_id"`
		WorkspaceID string `json:"workspace_id"`
	}
	memberships := []membershipJSON{}
	if auth.TenantID != "" && auth.WorkspaceID != "" {
		memberships = append(memberships, membershipJSON{
			TenantID:    auth.TenantID,
			WorkspaceID: auth.WorkspaceID,
		})
	}

	type profileJSON struct {
		ExternalProviderConfigID  string           `json:"external_provider_config_id,omitempty"`
		ActiveTenantID            string           `json:"active_tenant_id,omitempty"`
		ActiveWorkspaceID         string           `json:"active_workspace_id,omitempty"`
		RoleIDs                   []string         `json:"role_ids,omitempty"`
		AllowedPermissionFeatures []string         `json:"allowed_permission_features,omitempty"`
		PermissionCatalogEnforced bool             `json:"permission_catalog_enforced"`
		MFA                       mfaJSON          `json:"mfa"`
		Memberships               []membershipJSON `json:"memberships"`
	}

	resp := profileJSON{
		ExternalProviderConfigID:  auth.ExternalProviderConfigID,
		ActiveTenantID:            auth.TenantID,
		ActiveWorkspaceID:         auth.WorkspaceID,
		RoleIDs:                   append([]string(nil), auth.RoleIDs...),
		AllowedPermissionFeatures: append([]string(nil), auth.AllowedPermissionFeatures...),
		PermissionCatalogEnforced: auth.PermissionCatalogEnforced,
		MFA:                       mfa,
		Memberships:               memberships,
	}

	WriteJSON(w, http.StatusOK, resp)
}
