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
// hash-only MFA recovery proof used to authenticate one local user.
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
)

// LocalIdentityAuthContext is the authorization context returned after login.
type LocalIdentityAuthContext struct {
	TenantID           string
	WorkspaceID        string
	SubjectIDHash      string
	SubjectClass       string
	PolicyRevisionHash string
	AllScopes          bool
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
}

type localIdentityInvitationRow struct {
	InviteID           string
	TenantID           string
	WorkspaceID        string
	RoleID             string
	PolicyRevisionHash string
}
