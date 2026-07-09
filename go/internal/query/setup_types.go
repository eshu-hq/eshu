// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"time"
)

// SetupStore is the query-layer port for the first-run setup wizard
// (epic #4962, issue #4965). It never touches secretcrypto or DEK material
// directly — that stays in the cmd/api adapter, mirroring the split
// go/cmd/api/seed_initial_admin.go already uses (handlers depend on ports,
// not concrete backend drivers). Every method here receives or returns
// already-hashed/already-verified values, the same "caller hashes/seals,
// store only persists" split LocalIdentityStore uses.
type SetupStore interface {
	// SetupNeeded reports whether the wizard should still be reachable: a
	// retrievable (unconsumed) bootstrap credential exists for the fixed
	// bootstrap tenant/workspace slot. Every mutating setup route MUST
	// re-check this and fail closed with 410 Gone when it returns false —
	// the wizard cannot be re-entered once any identity exists.
	SetupNeeded(ctx context.Context) (bool, error)
	// VerifyBootstrapCredential checks submitted plaintext username/password
	// against the sealed bootstrap credential envelope. ok is false for any
	// mismatch, missing, or already-consumed credential — the caller never
	// learns which, so a wrong guess cannot be used to probe instance state.
	VerifyBootstrapCredential(ctx context.Context, username, password string) (ok bool, err error)
	// ResolveSetupOwner resolves the identity the bootstrap credential
	// belongs to, for the password/MFA rotation calls that follow a
	// successful VerifyBootstrapCredential.
	ResolveSetupOwner(ctx context.Context) (SetupOwner, error)
	// RotateSetupPassword replaces the owner's local password, revoking the
	// prior credential in the same store-layer transaction.
	RotateSetupPassword(ctx context.Context, reset LocalIdentityPasswordReset) error
	// CompleteSetupMFA atomically rotates the owner's MFA recovery-code
	// factor and permanently consumes the bootstrap credential envelope in
	// one transaction / advisory-locked critical section (#4990), so two
	// concurrent completions for the same owner cannot both rotate MFA or
	// both believe they sealed the wizard. completed is false, with no
	// error, when a concurrent caller already completed setup first inside
	// the same critical section — the caller's generated recovery codes
	// were never persisted in that case and MUST be discarded; the caller
	// MUST fail closed (never issue a session claiming success) rather than
	// treat this as a normal completion.
	CompleteSetupMFA(ctx context.Context, in CompleteSetupMFAInput) (completed bool, err error)
}

// CompleteSetupMFAInput carries the caller-hashed recovery codes and owner
// identity CompleteSetupMFA needs to atomically rotate MFA and consume the
// bootstrap credential.
type CompleteSetupMFAInput struct {
	TenantID            string
	WorkspaceID         string
	SubjectIDHash       string
	UserID              string
	MFAFactorID         string
	MFAFactorKind       string
	MFACredentialHandle string
	RecoveryCodeHashes  []string
	Now                 time.Time
}

// SetupOwner identifies the local identity the bootstrap credential belongs
// to, resolved after a successful claim.
type SetupOwner struct {
	UserID        string
	SubjectIDHash string
	TenantID      string
	WorkspaceID   string
}

// SetupStateResponse is the public, pre-auth GET /api/v0/auth/setup-state body.
type SetupStateResponse struct {
	NeedsSetup    bool   `json:"needs_setup"`
	BootstrapMode string `json:"bootstrap_mode"`
}

// SetupCompleteResponse is returned by the final MFA-enrollment step. It
// carries the newly generated recovery codes exactly once — the console must
// render them with copy-all and download, and must not advance past this
// step until the operator confirms they saved them. The codes are never
// logged or persisted in clear text; only their hashes reach storage.
type SetupCompleteResponse struct {
	Status            string                     `json:"status"`
	RecoveryCodes     []string                   `json:"recovery_codes"`
	Auth              BrowserSessionAuthResponse `json:"auth"`
	CSRFToken         string                     `json:"csrf_token,omitempty"`
	IdleExpiresAt     time.Time                  `json:"idle_expires_at,omitempty"`
	AbsoluteExpiresAt time.Time                  `json:"absolute_expires_at,omitempty"`
}
