// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"time"
)

// LocalIdentityAPITokenListItem is the metadata-only view of one API token
// that is safe to return to the owning subject. It never includes token_hash,
// display_handle_hash, or any raw credential value.
// display_handle_hash is omitted intentionally — it is SHA-256(display_label)
// and presenting a hash as a human label is misleading. See issue #3708 for
// persisting a real non-secret display label.
type LocalIdentityAPITokenListItem struct {
	TokenID    string
	TokenClass string
	IssuedAt   time.Time
	ExpiresAt  time.Time
	RevokedAt  time.Time
}

// LocalIdentityMFAStatus is the safe-to-expose MFA state for one identity.
// It never includes credential handles, recovery code hashes, or factor IDs.
type LocalIdentityMFAStatus struct {
	HasActiveMFA bool
	// FactorKind is the active MFA factor kind (e.g. "recovery_code") when
	// HasActiveMFA is true, and empty otherwise.
	FactorKind string
}

// LocalIdentityProfileLister is the read surface for per-subject profile
// aggregation. It extends LocalIdentityStore with list, MFA status, and
// TOTP enrollment operations.
type LocalIdentityProfileLister interface {
	LocalIdentityStore
	// ListAPITokensBySubject returns metadata-only token rows owned by the
	// subject identified by subjectIDHash. The result never includes token_hash.
	ListAPITokensBySubject(ctx context.Context, subjectIDHash string, asOf time.Time) ([]LocalIdentityAPITokenListItem, error)
	// GetLocalIdentityMFAStatus returns the safe MFA state for the subject.
	// The result never includes credential handles or recovery hashes.
	GetLocalIdentityMFAStatus(ctx context.Context, subjectIDHash string, asOf time.Time) (LocalIdentityMFAStatus, error)
	// ResolveLocalIdentityUserID resolves the internal user_id for a
	// session's subjectIDHash (issue #4986). Self-service TOTP enrollment
	// only ever holds a session's subjectIDHash; ok is false, with no error,
	// when no live user matches.
	ResolveLocalIdentityUserID(ctx context.Context, subjectIDHash string) (userID string, ok bool, err error)
	// BeginLocalIdentityTOTPEnrollment seals and persists a PENDING TOTP
	// factor for begin.UserID (issue #4986). The factor cannot satisfy MFA
	// login until ConfirmLocalIdentityTOTPEnrollment activates it.
	BeginLocalIdentityTOTPEnrollment(ctx context.Context, begin LocalIdentityTOTPEnrollmentBegin) error
	// ConfirmLocalIdentityTOTPEnrollment verifies the first submitted code
	// against a pending TOTP factor and activates it on match.
	ConfirmLocalIdentityTOTPEnrollment(ctx context.Context, confirm LocalIdentityTOTPEnrollmentConfirm) error
}

// LocalIdentityTOTPEnrollmentBegin starts TOTP enrollment for one user
// (issue #4986). SecretPlaintext is sealed immediately by the store and
// never returned, logged, or persisted unsealed.
type LocalIdentityTOTPEnrollmentBegin struct {
	UserID          string
	FactorID        string
	SecretPlaintext []byte
	CreatedAt       time.Time
}

// LocalIdentityTOTPEnrollmentConfirm verifies the first submitted TOTP code
// against a pending enrollment (issue #4986).
type LocalIdentityTOTPEnrollmentConfirm struct {
	UserID   string
	FactorID string
	Code     string
	Now      time.Time
}
