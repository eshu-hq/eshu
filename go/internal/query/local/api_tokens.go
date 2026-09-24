// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package local

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	localIdentityAPITokenClassPersonal         = "personal"
	localIdentityAPITokenClassServicePrincipal = "service_principal"
)

// IdentityAPITokenCreate stores one hash-only generated API token.
type IdentityAPITokenCreate struct {
	TokenID            string
	TokenHash          string
	TokenClass         string
	TenantID           string
	WorkspaceID        string
	UserID             string
	ServicePrincipalID string
	DisplayHandleHash  string
	// DisplayLabel is the real, non-secret operator-facing label persisted as
	// plaintext (issue #3708), distinct from DisplayHandleHash.
	DisplayLabel       string
	PolicyRevisionHash string
	IssuedAt           time.Time
	ExpiresAt          time.Time
}

// IdentityAPITokenRevoke revokes one active generated API token.
type IdentityAPITokenRevoke struct {
	TokenID     string
	TenantID    string
	WorkspaceID string
	RevokedAt   time.Time
	// OwnerSubjectIDHash, when non-empty, restricts the revoke to a token the
	// named subject owns (self-service revoke, issue #5164). The store adds an
	// atomic ownership predicate so a caller can never revoke a token they do
	// not own; a non-matching token affects zero rows and the store reports it
	// as not-found. An empty value keeps the existing unrestricted all-scope
	// admin revoke.
	OwnerSubjectIDHash string
}

// IdentityAPITokenRotate atomically replaces one generated API token.
type IdentityAPITokenRotate struct {
	OldTokenID      string
	NewTokenID      string
	NewTokenHash    string
	TenantID        string
	WorkspaceID     string
	RotatedAt       time.Time
	NewTokenExpires time.Time
	// OwnerSubjectIDHash, when non-empty, restricts the rotation to a token the
	// named subject owns (self-service rotate, issue #5164). The store adds an
	// atomic ownership predicate to the replacement insert so a caller can
	// never rotate a token they do not own; a non-matching token affects zero
	// rows and the store reports it as not-found. An empty value keeps the
	// existing unrestricted all-scope admin rotate.
	OwnerSubjectIDHash string
}

// ErrIdentityAPITokenNotFound reports that a self-service revoke or rotate
// matched no active token the caller owns (issue #5164). Handlers translate it
// into a non-disclosing 404 so a caller cannot probe another subject's token
// existence by observing a different error.
var ErrIdentityAPITokenNotFound = errors.New("local identity api token not found")

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

func (h *IdentityHandler) mountAPITokenRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/auth/local/api-tokens", h.handleListAPITokens)
	mux.HandleFunc("POST /api/v0/auth/local/api-tokens", h.handleCreateAPIToken)
	mux.HandleFunc("POST /api/v0/auth/local/api-tokens/{token_id}/revoke", h.handleRevokeAPIToken)
	mux.HandleFunc("POST /api/v0/auth/local/api-tokens/{token_id}/rotate", h.handleRotateAPIToken)
}

func (h *IdentityHandler) handleListAPITokens(w http.ResponseWriter, r *http.Request) {
	authCtx, ok := auth.AuthContextFromContext(r.Context())
	if !ok {
		querycontract.WriteUnauthorized(w, r)
		return
	}
	authCtx = auth.NormalizeAuthContext(authCtx)
	if authCtx.SubjectIDHash == "" {
		querycontract.WriteUnauthorized(w, r)
		return
	}
	if h.Store == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "identity store unavailable")
		return
	}
	now := h.now()
	items, err := h.Store.ListAPITokensBySubject(r.Context(), authCtx.SubjectIDHash, now)
	if err != nil {
		slog.ErrorContext(r.Context(), "list api tokens by subject failed", "err", err)
		querycontract.WriteError(w, http.StatusInternalServerError, "failed to list api tokens")
		return
	}
	// display_label (issue #3708) is the real, non-secret operator-facing
	// label persisted alongside display_handle_hash. Unlike the hash, it is
	// safe to render verbatim, so it is included here when set.
	type tokenJSON struct {
		TokenID      string     `json:"token_id"`
		TokenClass   string     `json:"token_class,omitempty"`
		DisplayLabel string     `json:"display_label,omitempty"`
		IssuedAt     time.Time  `json:"issued_at"`
		ExpiresAt    *time.Time `json:"expires_at,omitempty"`
		RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	}
	out := make([]tokenJSON, 0, len(items))
	for _, item := range items {
		t := tokenJSON{
			TokenID:      item.TokenID,
			TokenClass:   item.TokenClass,
			DisplayLabel: item.DisplayLabel,
			IssuedAt:     item.IssuedAt,
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
	querycontract.WriteJSON(w, http.StatusOK, map[string]any{"tokens": out})
}

func (h *IdentityHandler) handleCreateAPIToken(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	authCtx, ok := h.authorizeTokenMutation(w, r)
	if !ok {
		return
	}
	if !h.requirePermissionFeature(
		w,
		r,
		governanceaudit.EventTypeTokenLifecycle,
		"tokens.create",
		auth.PermissionFeatureTokens,
	) {
		return
	}
	var req localIdentityAPITokenCreateRequest
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, "invalid local identity api token request")
		return
	}
	if authCtx.AllScopes {
		// All-scope admin path (unchanged, issue #5164 preserves it): a blank
		// user_id on a personal token resolves to the admin's own identity, and
		// an explicit user_id mints a token for another user.
		if err := h.resolveSelfServiceAPITokenUserID(r, &req); err != nil {
			slog.ErrorContext(r.Context(), "resolve self-service api token user id failed", "err", err)
			querycontract.WriteError(w, http.StatusInternalServerError, "failed to create local identity api token")
			return
		}
	} else if !h.enforceSelfServiceTokenCreateScope(w, r, &req, authCtx) {
		// Non-admin self-service: the request is constrained to the caller's
		// own personal token, or already denied with 403/500.
		return
	}
	now := h.now()
	tokenID, apiToken := h.newID(), ""
	var err error
	apiToken, err = h.newSecret()
	if tokenID == "" || err != nil || apiToken == "" {
		querycontract.WriteError(w, http.StatusInternalServerError, "failed to create local identity api token")
		return
	}
	record := h.buildAPITokenCreateRecord(r, req, tokenID, apiToken, now)
	if err := h.Store.CreateLocalIdentityAPIToken(r.Context(), record); err != nil {
		h.auditLocalIdentity(r, governanceaudit.EventTypeTokenLifecycle, governanceaudit.DecisionDenied, "api_token_issue_failed", "")
		querycontract.WriteError(w, http.StatusBadRequest, "failed to create local identity api token")
		return
	}
	h.auditLocalIdentity(r, governanceaudit.EventTypeTokenLifecycle, governanceaudit.DecisionAllowed, "api_token_issued", "")
	querycontract.WriteJSON(w, http.StatusCreated, localIdentityAPITokenResponse{
		Status:     "created",
		TokenID:    record.TokenID,
		TokenClass: record.TokenClass,
		APIToken:   apiToken,
		IssuedAt:   record.IssuedAt,
		ExpiresAt:  record.ExpiresAt,
	})
}

func (h *IdentityHandler) handleRevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	authCtx, ok := h.authorizeTokenMutation(w, r)
	if !ok {
		return
	}
	if !h.requirePermissionFeature(
		w,
		r,
		governanceaudit.EventTypeTokenLifecycle,
		"tokens.revoke",
		auth.PermissionFeatureTokens,
	) {
		return
	}
	var req localIdentityAPITokenRevokeRequest
	if err := readOptionalAPITokenRevokeRequest(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, "invalid local identity api token revoke request")
		return
	}
	tenantID, workspaceID := localIdentityAPITokenScope(req.TenantID, req.WorkspaceID, authCtx)
	revoke := IdentityAPITokenRevoke{
		TokenID:            querycontract.PathParam(r, "token_id"),
		TenantID:           tenantID,
		WorkspaceID:        workspaceID,
		RevokedAt:          h.now(),
		OwnerSubjectIDHash: selfServiceTokenOwner(authCtx),
	}
	if err := h.Store.RevokeLocalIdentityAPIToken(r.Context(), revoke); err != nil {
		h.auditLocalIdentity(r, governanceaudit.EventTypeTokenLifecycle, governanceaudit.DecisionDenied, "api_token_revoke_failed", "")
		if h.writeSelfServiceTokenNotFound(w, revoke.OwnerSubjectIDHash, err) {
			return
		}
		querycontract.WriteError(w, http.StatusBadRequest, "failed to revoke local identity api token")
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

func (h *IdentityHandler) handleRotateAPIToken(w http.ResponseWriter, r *http.Request) {
	if !h.ready(w) {
		return
	}
	authCtx, ok := h.authorizeTokenMutation(w, r)
	if !ok {
		return
	}
	if !h.requirePermissionFeature(
		w,
		r,
		governanceaudit.EventTypeTokenLifecycle,
		"tokens.rotate",
		auth.PermissionFeatureTokens,
	) {
		return
	}
	var req localIdentityAPITokenRotateRequest
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, "invalid local identity api token rotation request")
		return
	}
	now := h.now()
	tokenID, apiToken := h.newID(), ""
	var err error
	apiToken, err = h.newSecret()
	if tokenID == "" || err != nil || apiToken == "" {
		querycontract.WriteError(w, http.StatusInternalServerError, "failed to rotate local identity api token")
		return
	}
	tenantID, workspaceID := localIdentityAPITokenScope(req.TenantID, req.WorkspaceID, authCtx)
	rotation := IdentityAPITokenRotate{
		OldTokenID:         querycontract.PathParam(r, "token_id"),
		NewTokenID:         tokenID,
		NewTokenHash:       IdentityHash(apiToken),
		TenantID:           tenantID,
		WorkspaceID:        workspaceID,
		RotatedAt:          now,
		NewTokenExpires:    req.ExpiresAt.UTC(),
		OwnerSubjectIDHash: selfServiceTokenOwner(authCtx),
	}
	if err := h.Store.RotateLocalIdentityAPIToken(r.Context(), rotation); err != nil {
		h.auditLocalIdentity(r, governanceaudit.EventTypeTokenLifecycle, governanceaudit.DecisionDenied, "api_token_rotate_failed", "")
		if h.writeSelfServiceTokenNotFound(w, rotation.OwnerSubjectIDHash, err) {
			return
		}
		querycontract.WriteError(w, http.StatusBadRequest, "failed to rotate local identity api token")
		return
	}
	h.auditLocalIdentity(r, governanceaudit.EventTypeTokenLifecycle, governanceaudit.DecisionAllowed, "api_token_rotated", "")
	querycontract.WriteJSON(w, http.StatusCreated, localIdentityAPITokenResponse{
		Status:    "rotated",
		TokenID:   tokenID,
		APIToken:  apiToken,
		IssuedAt:  now,
		ExpiresAt: rotation.NewTokenExpires,
	})
}

// resolveSelfServiceAPITokenUserID fills req.UserID from the caller's own
// session when a personal-token create request omits it. A browser session
// only ever carries a one-way SubjectIDHash (never the internal user_id
// primary key identity_token_metadata.user_id requires), so the console's
// self-service "create my own token" form structurally cannot supply
// user_id — it has no way to learn it. This mirrors the same resolution
// self-service TOTP enrollment already performs for the identical problem
// (handleBeginTOTPEnrollment in totp.go).
//
// This only ever fills a BLANK user_id. An explicit user_id in the request
// body — the existing all-scope admin flow that mints a token for a
// DIFFERENT target user — always wins outright and this resolution is
// skipped entirely, so admin-driven creation on behalf of another user is
// unaffected. Service-principal tokens are never self-resolved: a service
// principal is not the human caller's own identity, so its token is always
// minted by explicitly naming service_principal_id.
func (h *IdentityHandler) resolveSelfServiceAPITokenUserID(
	r *http.Request,
	req *localIdentityAPITokenCreateRequest,
) error {
	if strings.TrimSpace(req.UserID) != "" {
		return nil
	}
	if localIdentityDefault(req.TokenClass, localIdentityAPITokenClassPersonal) != localIdentityAPITokenClassPersonal {
		return nil
	}
	authCtx, ok := auth.AuthContextFromContext(r.Context())
	authCtx = auth.NormalizeAuthContext(authCtx)
	if !ok || authCtx.SubjectIDHash == "" {
		return nil
	}
	userID, found, err := h.Store.ResolveLocalIdentityUserID(r.Context(), authCtx.SubjectIDHash)
	if err != nil {
		return err
	}
	if !found {
		// Leave UserID blank: the existing validation path in
		// buildAPITokenCreateRecord/CreateLocalIdentityAPIToken already
		// rejects a blank user_id with a clear 400, so this fails the same
		// way it did before this resolution existed.
		return nil
	}
	req.UserID = userID
	return nil
}

func (h *IdentityHandler) buildAPITokenCreateRecord(
	r *http.Request,
	req localIdentityAPITokenCreateRequest,
	tokenID string,
	apiToken string,
	issuedAt time.Time,
) IdentityAPITokenCreate {
	authCtx, _ := auth.AuthContextFromContext(r.Context())
	authCtx = auth.NormalizeAuthContext(authCtx)
	tenantID, workspaceID := localIdentityAPITokenScope(req.TenantID, req.WorkspaceID, authCtx)
	policyRevision := strings.TrimSpace(authCtx.PolicyRevisionHash)
	if policyRevision == "" {
		policyRevision = PolicyRevision(tenantID, workspaceID)
	}
	return IdentityAPITokenCreate{
		TokenID:            tokenID,
		TokenHash:          IdentityHash(apiToken),
		TokenClass:         localIdentityDefault(req.TokenClass, localIdentityAPITokenClassPersonal),
		TenantID:           tenantID,
		WorkspaceID:        workspaceID,
		UserID:             strings.TrimSpace(req.UserID),
		ServicePrincipalID: strings.TrimSpace(req.ServicePrincipalID),
		DisplayHandleHash:  IdentityHash(req.DisplayLabel),
		DisplayLabel:       strings.TrimSpace(req.DisplayLabel),
		PolicyRevisionHash: policyRevision,
		IssuedAt:           issuedAt,
		ExpiresAt:          req.ExpiresAt.UTC(),
	}
}

func localIdentityAPITokenScope(reqTenantID string, reqWorkspaceID string, auth auth.AuthContext) (string, string) {
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
