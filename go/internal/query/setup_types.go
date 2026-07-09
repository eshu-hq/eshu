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
	// RotateSetupMFA replaces the owner's MFA recovery-code factor.
	RotateSetupMFA(ctx context.Context, reset LocalIdentityMFAReset) error
	// CompleteSetup permanently consumes the bootstrap credential envelope
	// for the given owner subject, sealing the wizard shut. Subsequent
	// SetupNeeded calls return false forever — the wizard cannot be
	// re-entered.
	CompleteSetup(ctx context.Context, subjectIDHash string, now time.Time) error
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
