// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"time"
)

// LocalIdentityStore is the query-layer port for hash-only local identity
// persistence. Concrete implementations live outside internal/query.
type LocalIdentityStore interface {
	BootstrapLocalIdentity(context.Context, LocalIdentityBootstrapRecord) error
	AuthenticateLocalIdentity(
		context.Context,
		LocalIdentityAuthenticationAttempt,
	) (LocalIdentityAuthenticationResult, error)
	CreateLocalIdentityInvitation(context.Context, LocalIdentityInvitationRecord) error
	AcceptLocalIdentityInvitation(context.Context, LocalIdentityInvitationAcceptance) error
	ResetLocalIdentityPassword(context.Context, LocalIdentityPasswordReset) error
	RotateLocalIdentityPassword(context.Context, LocalIdentityPasswordRotation) (LocalIdentityAuthenticationResult, error)
	ResetLocalIdentityMFA(context.Context, LocalIdentityMFAReset) error
	DisableLocalIdentityUser(context.Context, LocalIdentityDisableUser) error
	EnableLocalIdentityBreakGlass(context.Context, LocalIdentityBreakGlassWindow) error
	ResolveLocalIdentityBreakGlass(context.Context, LocalIdentityBreakGlassAttempt) (LocalIdentityAuthContext, error)
	CreateLocalIdentityAPIToken(context.Context, LocalIdentityAPITokenCreate) error
	RevokeLocalIdentityAPIToken(context.Context, LocalIdentityAPITokenRevoke) error
	RotateLocalIdentityAPIToken(context.Context, LocalIdentityAPITokenRotate) error
}

// LocalIdentityBootstrapRecord contains hash-only first-owner setup state.
type LocalIdentityBootstrapRecord struct {
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

// LocalIdentityInvitationRecord stores one hash-only local signup invitation.
type LocalIdentityInvitationRecord struct {
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

// LocalIdentityInvitationAcceptance creates a user from a live invitation.
type LocalIdentityInvitationAcceptance struct {
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

// LocalIdentityAuthenticationAttempt carries a local credential proof.
type LocalIdentityAuthenticationAttempt struct {
	SubjectIDHash         string
	Password              string
	MFARecoveryCodeHash   string
	ConsumeRecoveryCodeAt time.Time
	Now                   time.Time
}

// LocalIdentityAuthStatus is the bounded result of local credential validation.
type LocalIdentityAuthStatus string

const (
	// LocalIdentityAuthAuthenticated means the password and required MFA passed.
	LocalIdentityAuthAuthenticated LocalIdentityAuthStatus = "authenticated"
	// LocalIdentityAuthInvalid means the submitted credential proof failed.
	LocalIdentityAuthInvalid LocalIdentityAuthStatus = "invalid"
	// LocalIdentityAuthMFARequired means the account needs MFA proof before login.
	LocalIdentityAuthMFARequired LocalIdentityAuthStatus = "mfa_required"
	// LocalIdentityAuthLocked means local login is temporarily locked.
	LocalIdentityAuthLocked LocalIdentityAuthStatus = "locked"
	// LocalIdentityAuthDisabled means the local identity is disabled.
	LocalIdentityAuthDisabled LocalIdentityAuthStatus = "disabled"
	// LocalIdentityAuthMustChangePassword means the password and any required
	// MFA proof both passed, but the credential must be rotated (issue #4976)
	// through RotateLocalIdentityPassword before a session is issued.
	LocalIdentityAuthMustChangePassword LocalIdentityAuthStatus = "must_change_password"
)

// LocalIdentityAuthContext is the query-layer auth context returned by storage.
//
// RoleIDs and the permission-catalog fields carry the same enforcement snapshot
// a scoped token for the same roles would carry. They are populated only for
// non-all-scope logins; all-scope (admin) logins keep PermissionCatalogEnforced
// false and remain fail-open.
type LocalIdentityAuthContext struct {
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

// LocalIdentityAuthenticationResult is the hash-safe local login result.
type LocalIdentityAuthenticationResult struct {
	Status        LocalIdentityAuthStatus
	Authenticated bool
	Auth          LocalIdentityAuthContext
	LockedUntil   time.Time
}

// LocalIdentityPasswordReset rotates one user's local password hash.
type LocalIdentityPasswordReset struct {
	UserID                 string
	CredentialID           string
	PasswordHash           string
	PasswordAlgorithm      string
	PasswordParametersHash string
	ResetAt                time.Time
}

// LocalIdentityPasswordRotation is a self-service credential rotation: the
// caller proves the CURRENT password (and MFA recovery-code proof, if the
// account has an active MFA factor) before the new password is accepted and
// a session is issued. Issue #4976's forced-rotation surface: it never
// depends on the caller already holding a session.
type LocalIdentityPasswordRotation struct {
	SubjectIDHash             string
	CurrentPassword           string
	NewPasswordHash           string
	NewPasswordAlgorithm      string
	NewPasswordParametersHash string
	CredentialID              string
	MFARecoveryCodeHash       string
	ConsumeRecoveryCodeAt     time.Time
	Now                       time.Time
}

// LocalIdentityMFAReset replaces active MFA factors and recovery hashes.
type LocalIdentityMFAReset struct {
	UserID              string
	MFAFactorID         string
	MFAFactorKind       string
	MFACredentialHandle string
	RecoveryCodeHashes  []string
	ResetAt             time.Time
}

// LocalIdentityDisableUser disables a user and revokes active local sessions.
type LocalIdentityDisableUser struct {
	UserID     string
	DisabledAt time.Time
}

// LocalIdentityBreakGlassWindow stores one time-boxed recovery window.
type LocalIdentityBreakGlassWindow struct {
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

// LocalIdentityBreakGlassAttempt resolves one time-boxed recovery proof.
type LocalIdentityBreakGlassAttempt struct {
	BreakGlassCodeHash string
	Now                time.Time
}

// LocalIdentitySessionResponse is returned after successful local login.
type LocalIdentitySessionResponse struct {
	Status            string                     `json:"status"`
	Auth              BrowserSessionAuthResponse `json:"auth,omitempty"`
	CSRFToken         string                     `json:"csrf_token,omitempty"`
	IdleExpiresAt     time.Time                  `json:"idle_expires_at,omitempty"`
	AbsoluteExpiresAt time.Time                  `json:"absolute_expires_at,omitempty"`
	LockedUntil       time.Time                  `json:"locked_until,omitempty"`
}
