// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package local

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/auth/session"
)

// IdentityStore is the query-layer port for hash-only local identity
// persistence. Concrete implementations live outside internal/query.
type IdentityStore interface {
	BootstrapLocalIdentity(context.Context, IdentityBootstrapRecord) error
	AuthenticateLocalIdentity(
		context.Context,
		IdentityAuthenticationAttempt,
	) (IdentityAuthenticationResult, error)
	CreateLocalIdentityInvitation(context.Context, IdentityInvitationRecord) error
	AcceptLocalIdentityInvitation(context.Context, IdentityInvitationAcceptance) error
	ResetLocalIdentityPassword(context.Context, IdentityPasswordReset) error
	RotateLocalIdentityPassword(context.Context, IdentityPasswordRotation) (IdentityAuthenticationResult, error)
	ResetLocalIdentityMFA(context.Context, IdentityMFAReset) error
	DisableLocalIdentityUser(context.Context, IdentityDisableUser) error
	EnableLocalIdentityBreakGlass(context.Context, IdentityBreakGlassWindow) error
	ResolveLocalIdentityBreakGlass(context.Context, IdentityBreakGlassAttempt) (IdentityAuthContext, error)
	CreateLocalIdentityAPIToken(context.Context, IdentityAPITokenCreate) error
	RevokeLocalIdentityAPIToken(context.Context, IdentityAPITokenRevoke) error
	RotateLocalIdentityAPIToken(context.Context, IdentityAPITokenRotate) error
}

// IdentityBootstrapRecord contains hash-only first-owner setup state.
type IdentityBootstrapRecord struct {
	TenantID               string
	WorkspaceID            string
	UserID                 string
	SubjectIDHash          string
	ProfileHandleHash      string
	PasswordHash           string
	PasswordAlgorithm      string
	PasswordParametersHash string
	MFAFactorID            string
	MFAFactorKind          string
	MFACredentialHandle    string
	RecoveryCodeHashes     []string
	PolicyRevisionHash     string
	CreatedAt              time.Time
}

// IdentityInvitationRecord stores one hash-only local signup invitation.
type IdentityInvitationRecord struct {
	InviteID             string
	TenantID             string
	WorkspaceID          string
	InviteCodeHash       string
	InviteeHandleHash    string
	InviterSubjectIDHash string
	RoleID               string
	Status               string
	PolicyRevisionHash   string
	ExpiresAt            time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// IdentityInvitationAcceptance creates a user from a live invitation.
type IdentityInvitationAcceptance struct {
	InviteCodeHash         string
	UserID                 string
	SubjectIDHash          string
	ProfileHandleHash      string
	PasswordHash           string
	PasswordAlgorithm      string
	PasswordParametersHash string
	MFAFactorID            string
	MFAFactorKind          string
	MFACredentialHandle    string
	RecoveryCodeHashes     []string
	AcceptedAt             time.Time
}

// IdentityAuthenticationAttempt carries a local credential proof.
type IdentityAuthenticationAttempt struct {
	SubjectIDHash         string
	Password              string
	MFARecoveryCodeHash   string
	ConsumeRecoveryCodeAt time.Time
	// MFATOTPCode is the raw authenticator-app code submitted at login
	// (issue #4986), checked before MFARecoveryCodeHash when both are set.
	MFATOTPCode string
	Now         time.Time
}

// IdentityAuthStatus is the bounded result of local credential validation.
type IdentityAuthStatus string

const (
	// IdentityAuthAuthenticated means the password and required MFA passed.
	IdentityAuthAuthenticated IdentityAuthStatus = "authenticated"
	// IdentityAuthInvalid means the submitted credential proof failed.
	IdentityAuthInvalid IdentityAuthStatus = "invalid"
	// IdentityAuthMFARequired means the account needs MFA proof before login.
	IdentityAuthMFARequired IdentityAuthStatus = "mfa_required"
	// IdentityAuthLocked means local login is temporarily locked.
	IdentityAuthLocked IdentityAuthStatus = "locked"
	// IdentityAuthDisabled means the local identity is disabled.
	IdentityAuthDisabled IdentityAuthStatus = "disabled"
	// IdentityAuthMustChangePassword means the password and any required
	// MFA proof both passed, but the credential must be rotated (issue #4976)
	// through RotateLocalIdentityPassword before a session is issued.
	IdentityAuthMustChangePassword IdentityAuthStatus = "must_change_password"
)

// IdentityAuthContext is the query-layer auth context returned by storage.
//
// RoleIDs and the permission-catalog fields carry the same enforcement snapshot
// a scoped token for the same roles would carry. They are populated only for
// non-all-scope logins; all-scope (admin) logins keep PermissionCatalogEnforced
// false and remain fail-open.
type IdentityAuthContext struct {
	TenantID                     string
	WorkspaceID                  string
	SubjectIDHash                string
	SubjectClass                 string
	PolicyRevisionHash           string
	AllScopes                    bool
	RoleIDs                      []string
	PermissionCatalogEnforced    bool
	AllowedPermissionFeatures    []string
	AllowedPermissionDataClasses []string
}

// IdentityAuthenticationResult is the hash-safe local login result.
type IdentityAuthenticationResult struct {
	Status        IdentityAuthStatus
	Authenticated bool
	Auth          IdentityAuthContext
	LockedUntil   time.Time
}

// IdentityPasswordReset rotates one user's local password hash.
type IdentityPasswordReset struct {
	UserID                 string
	CredentialID           string
	PasswordHash           string
	PasswordAlgorithm      string
	PasswordParametersHash string
	ResetAt                time.Time
}

// IdentityPasswordRotation is a self-service credential rotation: the
// caller proves the CURRENT password (and MFA recovery-code proof, if the
// account has an active MFA factor) before the new password is accepted and
// a session is issued. Issue #4976's forced-rotation surface: it never
// depends on the caller already holding a session.
type IdentityPasswordRotation struct {
	SubjectIDHash             string
	CurrentPassword           string
	NewPasswordHash           string
	NewPasswordAlgorithm      string
	NewPasswordParametersHash string
	CredentialID              string
	MFARecoveryCodeHash       string
	ConsumeRecoveryCodeAt     time.Time
	// MFATOTPCode is the raw authenticator-app code re-proved at rotation
	// time (issue #4986), checked before MFARecoveryCodeHash when both are
	// set.
	MFATOTPCode string
	Now         time.Time
}

// IdentityMFAReset replaces active MFA factors and recovery hashes.
type IdentityMFAReset struct {
	UserID              string
	MFAFactorID         string
	MFAFactorKind       string
	MFACredentialHandle string
	RecoveryCodeHashes  []string
	ResetAt             time.Time
}

// IdentityDisableUser disables a user and revokes active local sessions.
type IdentityDisableUser struct {
	UserID     string
	DisabledAt time.Time
}

// IdentityBreakGlassWindow stores one time-boxed recovery window.
type IdentityBreakGlassWindow struct {
	RecoveryID         string
	TenantID           string
	WorkspaceID        string
	SubjectIDHash      string
	BreakGlassCodeHash string
	Status             string
	ReasonCode         string
	PolicyRevisionHash string
	EnabledAt          time.Time
	ExpiresAt          time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// IdentityBreakGlassAttempt resolves one time-boxed recovery proof.
type IdentityBreakGlassAttempt struct {
	BreakGlassCodeHash string
	Now                time.Time
}

// IdentitySessionResponse is returned after successful local login.
type IdentitySessionResponse struct {
	Status            string                             `json:"status"`
	Auth              session.BrowserSessionAuthResponse `json:"auth,omitempty"`
	CSRFToken         string                             `json:"csrf_token,omitempty"`
	IdleExpiresAt     time.Time                          `json:"idle_expires_at,omitempty"`
	AbsoluteExpiresAt time.Time                          `json:"absolute_expires_at,omitempty"`
	LockedUntil       time.Time                          `json:"locked_until,omitempty"`
}
