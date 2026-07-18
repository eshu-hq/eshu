// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"time"
)

const (
	defaultLocalIdentityLockoutThreshold = 5
	defaultLocalIdentityLockoutWindow    = 15 * time.Minute
	localIdentityOwnerRoleID             = "owner"
)

// Local identity sentinel errors keep handler mapping stable without exposing
// credential material or account existence beyond the intended status.
var (
	ErrLocalIdentityAdminMFARequired      = errors.New("local identity admin mfa required")
	ErrLocalIdentityBootstrapCompleted    = errors.New("local identity bootstrap already completed")
	ErrLocalIdentityBreakGlassUnavailable = errors.New("local identity break-glass recovery unavailable")
	ErrLocalIdentityInvitationRequired    = errors.New("local identity invitation required")
	ErrLocalIdentityAPITokenUnavailable   = errors.New("local identity api token unavailable")
	ErrLocalIdentityTransactionRequired   = errors.New("local identity store transaction support is required")
	// ErrLocalIdentityMFARequiredByPolicy means the tenant's sign-in policy
	// has RequireMFAForAllUsers=true (issue #4968) and this invitation
	// acceptance did not enroll an MFA factor. Distinct from
	// ErrLocalIdentityAdminMFARequired, which is the unconditional
	// admin/owner-bootstrap MFA requirement independent of tenant policy.
	ErrLocalIdentityMFARequiredByPolicy = errors.New("local identity mfa required by tenant sign-in policy")
)

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
	// MustChangePassword forces a rotation before this credential can ever
	// obtain a full session (issue #4976). Set true only for the
	// ESHU_ADMIN_USERNAME/PASSWORD[_FILE]-seeded bootstrap admin
	// (go/cmd/api/seed_initial_admin.go seedBootstrapAdminFromEnv); the
	// ESHU_AUTH_BOOTSTRAP_MODE=generated path sets it false because the
	// first-run setup wizard (#4965) already achieves effective rotation.
	MustChangePassword bool
}

// LocalIdentityInvitationRecord stores one hash-only invite and role assignment.
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

// LocalIdentityAuthenticationAttempt contains the raw password proof and
// MFA proof used to authenticate one local user. Exactly one MFA proof
// field is expected when the account requires MFA: MFATOTPCode is checked
// first when both are set (see AuthenticateLocalIdentity), so a recovery
// code is never consumed when a TOTP code was also submitted.
type LocalIdentityAuthenticationAttempt struct {
	SubjectIDHash         string
	Password              string
	MFARecoveryCodeHash   string
	ConsumeRecoveryCodeAt time.Time
	// MFATOTPCode is the raw (unhashed) authenticator-app code submitted at
	// login (issue #4986). Unlike MFARecoveryCodeHash, this is never hashed
	// by the caller — it is verified against the user's sealed TOTP secret
	// inside AuthenticateLocalIdentity via verifyLocalIdentityTOTPCode.
	MFATOTPCode string
	Now         time.Time
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
	// MFA proof both passed, but the credential is flagged
	// must_change_password=true (issue #4976): no session is issued until the
	// caller rotates the password through
	// IdentitySubjectStore.RotateLocalIdentityPassword.
	LocalIdentityAuthMustChangePassword LocalIdentityAuthStatus = "must_change_password"
)

// LocalIdentityAuthContext is the authorization context returned after login.
//
// RoleIDs, PermissionCatalogEnforced, AllowedPermissionFeatures, and
// AllowedPermissionDataClasses carry the same permission-catalog snapshot a
// scoped token for the same roles would carry, so a cookie session enforces
// identically. They are populated only for non-all-scope (non-admin) logins;
// all-scope sessions keep PermissionCatalogEnforced=false and remain fail-open.
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

// LocalIdentityAuthenticationResult is the hash-safe login result.
type LocalIdentityAuthenticationResult struct {
	Status        LocalIdentityAuthStatus
	Authenticated bool
	Auth          LocalIdentityAuthContext
	LockedUntil   time.Time
}

// LocalIdentityPasswordReset rotates the local password and clears lockout.
type LocalIdentityPasswordReset struct {
	UserID                 string
	CredentialID           string
	PasswordHash           string
	PasswordAlgorithm      string
	PasswordParametersHash string
	ResetAt                time.Time
}

// LocalIdentityMFAReset revokes old MFA factors and installs recovery hashes.
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

// LocalIdentityBreakGlassWindow stores one operator-enabled recovery window.
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

// LocalIdentityBreakGlassAttempt resolves a time-boxed recovery proof.
type LocalIdentityBreakGlassAttempt struct {
	BreakGlassCodeHash string
	Now                time.Time
}

// LocalIdentityAPITokenCreate stores one generated API token hash.
type LocalIdentityAPITokenCreate struct {
	TokenID            string
	TokenHash          string
	TokenClass         string
	TenantID           string
	WorkspaceID        string
	UserID             string
	ServicePrincipalID string
	DisplayHandleHash  string
	// DisplayLabel is the real, non-secret operator-facing label persisted as
	// plaintext (issue #3708). It is display-only and distinct from
	// DisplayHandleHash, which remains a one-way hash of the same input.
	DisplayLabel       string
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

// LocalIdentityAPITokenRotate atomically revokes one token and inserts its replacement.
type LocalIdentityAPITokenRotate struct {
	OldTokenID      string
	NewTokenID      string
	NewTokenHash    string
	TenantID        string
	WorkspaceID     string
	RotatedAt       time.Time
	NewTokenExpires time.Time
}

type localIdentityCredentialRow struct {
	UserID             string
	TenantID           string
	WorkspaceID        string
	SubjectIDHash      string
	PasswordHash       string
	Status             string
	DisabledAt         time.Time
	LockedUntil        time.Time
	FailedAttempts     int64
	HasAdminRole       bool
	HasActiveMFA       bool
	PolicyRevisionHash string
	MustChangePassword bool
}

// LocalIdentityPasswordRotation is a self-service credential rotation: the
// caller proves the CURRENT password (and MFA recovery-code proof, if the
// account has an active MFA factor) before the new password is accepted.
// Unlike LocalIdentityPasswordReset (admin-only, requires an existing
// all-scope session), rotation never depends on the caller already holding a
// session -- it is the only path a must-change-password credential (issue
// #4976) can use to ever obtain one. On success the new credential always has
// must_change_password=false, whether or not it was true before.
type LocalIdentityPasswordRotation struct {
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
	// set — see RotateLocalIdentityPassword.
	MFATOTPCode string
	Now         time.Time
}

type localIdentityInvitationRow struct {
	InviteID           string
	TenantID           string
	WorkspaceID        string
	RoleID             string
	PolicyRevisionHash string
}
