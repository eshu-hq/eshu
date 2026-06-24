// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
	"time"
)

type localIdentityBootstrapRequest struct {
	TenantID            string   `json:"tenant_id"`
	WorkspaceID         string   `json:"workspace_id"`
	LoginID             string   `json:"login_id"`
	ProfileHandle       string   `json:"profile_handle"`
	Password            string   `json:"password"`
	MFAFactorKind       string   `json:"mfa_factor_kind"`
	MFACredentialHandle string   `json:"mfa_credential_handle"`
	RecoveryCodes       []string `json:"recovery_codes"`
}

type localIdentityLoginRequest struct {
	LoginID      string `json:"login_id"`
	Password     string `json:"password"`
	RecoveryCode string `json:"recovery_code"`
}

type localIdentityInvitationRequest struct {
	TenantID      string    `json:"tenant_id"`
	WorkspaceID   string    `json:"workspace_id"`
	InviteCode    string    `json:"invite_code"`
	InviteeHandle string    `json:"invitee_handle"`
	RoleID        string    `json:"role_id"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type localIdentityAcceptInvitationRequest struct {
	InviteCode          string   `json:"invite_code"`
	LoginID             string   `json:"login_id"`
	ProfileHandle       string   `json:"profile_handle"`
	Password            string   `json:"password"`
	MFAFactorKind       string   `json:"mfa_factor_kind"`
	MFACredentialHandle string   `json:"mfa_credential_handle"`
	RecoveryCodes       []string `json:"recovery_codes"`
}

type localIdentityPasswordResetRequest struct {
	Password string `json:"password"`
}

type localIdentityMFAResetRequest struct {
	MFAFactorKind       string   `json:"mfa_factor_kind"`
	MFACredentialHandle string   `json:"mfa_credential_handle"`
	RecoveryCodes       []string `json:"recovery_codes"`
}

type localIdentityBreakGlassRequest struct {
	TenantID       string    `json:"tenant_id"`
	WorkspaceID    string    `json:"workspace_id"`
	SubjectID      string    `json:"subject_id"`
	BreakGlassCode string    `json:"break_glass_code"`
	ReasonCode     string    `json:"reason_code"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type localIdentityBreakGlassSessionRequest struct {
	BreakGlassCode string `json:"break_glass_code"`
}

func localIdentityDefault(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func localIdentityExpiry(explicit time.Time, now time.Time, duration time.Duration) time.Time {
	if !explicit.IsZero() {
		return explicit.UTC()
	}
	return now.Add(duration).UTC()
}

func localIdentityPolicyRevision(tenantID string, workspaceID string) string {
	return localIdentityHash(strings.TrimSpace(tenantID) + ":" + strings.TrimSpace(workspaceID))
}

func localIdentityOptionalID(h *LocalIdentityHandler, enabled bool) string {
	if !enabled {
		return ""
	}
	return h.newID()
}

func authTenantID(r *http.Request) string {
	auth, _ := AuthContextFromContext(r.Context())
	return normalizeAuthContext(auth).TenantID
}

func authWorkspaceID(r *http.Request) string {
	auth, _ := AuthContextFromContext(r.Context())
	return normalizeAuthContext(auth).WorkspaceID
}

func authSubjectIDHash(r *http.Request) string {
	auth, _ := AuthContextFromContext(r.Context())
	return normalizeAuthContext(auth).SubjectIDHash
}
