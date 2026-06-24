// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

const (
	localIdentityAPITokenClassPersonal         = "personal"
	localIdentityAPITokenClassServicePrincipal = "service_principal"
)

// LocalIdentityAPITokenCreate stores one hash-only generated API token.
type LocalIdentityAPITokenCreate struct {
	TokenID            string
	TokenHash          string
	TokenClass         string
	TenantID           string
	WorkspaceID        string
	UserID             string
	ServicePrincipalID string
	DisplayHandleHash  string
	PolicyRevisionHash string
	IssuedAt           time.Time
	ExpiresAt          time.Time
}

// LocalIdentityAPITokenRevoke revokes one active generated API token.
type LocalIdentityAPITokenRevoke struct {
	TokenID     string
	TenantID    string
	WorkspaceID string
	RevokedAt   time.Time
}

// LocalIdentityAPITokenRotate atomically replaces one generated API token.
type LocalIdentityAPITokenRotate struct {
	OldTokenID      string
	NewTokenID      string
	NewTokenHash    string
	TenantID        string
	WorkspaceID     string
	RotatedAt       time.Time
	NewTokenExpires time.Time
}

type localIdentityAPITokenCreateRequest struct {
	TokenClass         string    `json:"token_class"`
	TenantID           string    `json:"tenant_id"`
	WorkspaceID        string    `json:"workspace_id"`
	UserID             string    `json:"user_id"`
	ServicePrincipalID string    `json:"service_principal_id"`
	DisplayLabel       string    `json:"display_label"`
	ExpiresAt          time.Time `json:"expires_at"`
}

type localIdentityAPITokenRevokeRequest struct {
	TenantID    string `json:"tenant_id"`
	WorkspaceID string `json:"workspace_id"`
	ReasonCode  string `json:"reason_code"`
}

type localIdentityAPITokenRotateRequest struct {
	TenantID    string    `json:"tenant_id"`
	WorkspaceID string    `json:"workspace_id"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type localIdentityAPITokenResponse struct {
	Status     string    `json:"status"`
	TokenID    string    `json:"token_id"`
	TokenClass string    `json:"token_class,omitempty"`
	APIToken   string    `json:"api_token"`
	IssuedAt   time.Time `json:"issued_at"`
	ExpiresAt  time.Time `json:"expires_at,omitempty"`
}

func (h *LocalIdentityHandler) mountAPITokenRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/auth/local/api-tokens", h.handleListAPITokens)
	mux.HandleFunc("POST /api/v0/auth/local/api-tokens", h.handleCreateAPIToken)
	mux.HandleFunc("POST /api/v0/auth/local/api-tokens/{token_id}/revoke", h.handleRevokeAPIToken)
	mux.HandleFunc("POST /api/v0/auth/local/api-tokens/{token_id}/rotate", h.handleRotateAPIToken)
}

func (h *LocalIdentityHandler) handleListAPITokens(w http.ResponseWriter, r *http.Request) {
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
	if h.Store == nil {
		WriteError(w, http.StatusServiceUnavailable, "identity store unavailable")
		return
	}
	now := h.now()
	items, err := h.Store.ListAPITokensBySubject(r.Context(), auth.SubjectIDHash, now)
	if err != nil {
		slog.ErrorContext(r.Context(), "list api tokens by subject failed", "err", err)
		WriteError(w, http.StatusInternalServerError, "failed to list api tokens")
		return
	}
	// display_label is intentionally absent: the only value stored is
	// SHA-256(display_label) which is a hash, not a human label. Rendering it
	// as "Label" is misleading. Issue #3703 tracks persisting a real label.
	type tokenJSON struct {
		TokenID    string     `json:"token_id"`
		TokenClass string     `json:"token_class,omitempty"`
		IssuedAt   time.Time  `json:"issued_at"`
		ExpiresAt  *time.Time `json:"expires_at,omitempty"`
		RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	}
	out := make([]tokenJSON, 0, len(items))
	for _, item := range items {
		t := tokenJSON{
			TokenID:    item.TokenID,
			TokenClass: item.TokenClass,
			IssuedAt:   item.IssuedAt,
		}
		if !item.ExpiresAt.IsZero() {
			v := item.ExpiresAt
			t.ExpiresAt = &v
		}
		if !item.RevokedAt.IsZero() {
			v := item.RevokedAt
			t.RevokedAt = &v
		}
		out = append(out, t)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"tokens": out})
}

func (h *LocalIdentityHandler) handleCreateAPIToken(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) || !h.requireAllScopeAuth(w, r) {
		return
	}
	var req localIdentityAPITokenCreateRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid local identity api token request")
		return
	}
	now := h.now()
	tokenID, apiToken := h.newID(), ""
	var err error
	apiToken, err = h.newSecret()
	if tokenID == "" || err != nil || apiToken == "" {
		WriteError(w, http.StatusInternalServerError, "failed to create local identity api token")
		return
	}
	record := h.buildAPITokenCreateRecord(r, req, tokenID, apiToken, now)
	if err := h.Store.CreateLocalIdentityAPIToken(r.Context(), record); err != nil {
		h.auditLocalIdentity(r, governanceaudit.EventTypeTokenLifecycle, governanceaudit.DecisionDenied, "api_token_issue_failed", "")
		WriteError(w, http.StatusBadRequest, "failed to create local identity api token")
		return
	}
	h.auditLocalIdentity(r, governanceaudit.EventTypeTokenLifecycle, governanceaudit.DecisionAllowed, "api_token_issued", "")
	WriteJSON(w, http.StatusCreated, localIdentityAPITokenResponse{
		Status:     "created",
		TokenID:    record.TokenID,
		TokenClass: record.TokenClass,
		APIToken:   apiToken,
		IssuedAt:   record.IssuedAt,
		ExpiresAt:  record.ExpiresAt,
	})
}

func (h *LocalIdentityHandler) handleRevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) || !h.requireAllScopeAuth(w, r) {
		return
	}
	var req localIdentityAPITokenRevokeRequest
	if err := readOptionalAPITokenRevokeRequest(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid local identity api token revoke request")
		return
	}
	auth, _ := AuthContextFromContext(r.Context())
	auth = normalizeAuthContext(auth)
	tenantID, workspaceID := localIdentityAPITokenScope(req.TenantID, req.WorkspaceID, auth)
	if err := h.Store.RevokeLocalIdentityAPIToken(r.Context(), LocalIdentityAPITokenRevoke{
		TokenID:     PathParam(r, "token_id"),
		TenantID:    tenantID,
		WorkspaceID: workspaceID,
		RevokedAt:   h.now(),
	}); err != nil {
		h.auditLocalIdentity(r, governanceaudit.EventTypeTokenLifecycle, governanceaudit.DecisionDenied, "api_token_revoke_failed", "")
		WriteError(w, http.StatusBadRequest, "failed to revoke local identity api token")
		return
	}
	h.auditLocalIdentity(
		r,
		governanceaudit.EventTypeTokenLifecycle,
		governanceaudit.DecisionAllowed,
		localIdentityDefault(req.ReasonCode, "api_token_revoked"),
		"",
	)
	w.WriteHeader(http.StatusNoContent)
}

func (h *LocalIdentityHandler) handleRotateAPIToken(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) || !h.requireAllScopeAuth(w, r) {
		return
	}
	var req localIdentityAPITokenRotateRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid local identity api token rotation request")
		return
	}
	now := h.now()
	tokenID, apiToken := h.newID(), ""
	var err error
	apiToken, err = h.newSecret()
	if tokenID == "" || err != nil || apiToken == "" {
		WriteError(w, http.StatusInternalServerError, "failed to rotate local identity api token")
		return
	}
	auth, _ := AuthContextFromContext(r.Context())
	auth = normalizeAuthContext(auth)
	tenantID, workspaceID := localIdentityAPITokenScope(req.TenantID, req.WorkspaceID, auth)
	rotation := LocalIdentityAPITokenRotate{
		OldTokenID:      PathParam(r, "token_id"),
		NewTokenID:      tokenID,
		NewTokenHash:    localIdentityHash(apiToken),
		TenantID:        tenantID,
		WorkspaceID:     workspaceID,
		RotatedAt:       now,
		NewTokenExpires: req.ExpiresAt.UTC(),
	}
	if err := h.Store.RotateLocalIdentityAPIToken(r.Context(), rotation); err != nil {
		h.auditLocalIdentity(r, governanceaudit.EventTypeTokenLifecycle, governanceaudit.DecisionDenied, "api_token_rotate_failed", "")
		WriteError(w, http.StatusBadRequest, "failed to rotate local identity api token")
		return
	}
	h.auditLocalIdentity(r, governanceaudit.EventTypeTokenLifecycle, governanceaudit.DecisionAllowed, "api_token_rotated", "")
	WriteJSON(w, http.StatusCreated, localIdentityAPITokenResponse{
		Status:    "rotated",
		TokenID:   tokenID,
		APIToken:  apiToken,
		IssuedAt:  now,
		ExpiresAt: rotation.NewTokenExpires,
	})
}

func (h *LocalIdentityHandler) buildAPITokenCreateRecord(
	r *http.Request,
	req localIdentityAPITokenCreateRequest,
	tokenID string,
	apiToken string,
	issuedAt time.Time,
) LocalIdentityAPITokenCreate {
	auth, _ := AuthContextFromContext(r.Context())
	auth = normalizeAuthContext(auth)
	tenantID, workspaceID := localIdentityAPITokenScope(req.TenantID, req.WorkspaceID, auth)
	policyRevision := strings.TrimSpace(auth.PolicyRevisionHash)
	if policyRevision == "" {
		policyRevision = localIdentityPolicyRevision(tenantID, workspaceID)
	}
	return LocalIdentityAPITokenCreate{
		TokenID:            tokenID,
		TokenHash:          localIdentityHash(apiToken),
		TokenClass:         localIdentityDefault(req.TokenClass, localIdentityAPITokenClassPersonal),
		TenantID:           tenantID,
		WorkspaceID:        workspaceID,
		UserID:             strings.TrimSpace(req.UserID),
		ServicePrincipalID: strings.TrimSpace(req.ServicePrincipalID),
		DisplayHandleHash:  localIdentityHash(req.DisplayLabel),
		PolicyRevisionHash: policyRevision,
		IssuedAt:           issuedAt,
		ExpiresAt:          req.ExpiresAt.UTC(),
	}
}

func localIdentityAPITokenScope(reqTenantID string, reqWorkspaceID string, auth AuthContext) (string, string) {
	authTenantID := strings.TrimSpace(auth.TenantID)
	authWorkspaceID := strings.TrimSpace(auth.WorkspaceID)
	if authTenantID != "" || authWorkspaceID != "" {
		return authTenantID, authWorkspaceID
	}
	return localIdentityDefault(reqTenantID, ""), localIdentityDefault(reqWorkspaceID, "")
}

func readOptionalAPITokenRevokeRequest(r *http.Request, req *localIdentityAPITokenRevokeRequest) error {
	if r == nil || r.Body == nil {
		return nil
	}
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(req); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return nil
}
