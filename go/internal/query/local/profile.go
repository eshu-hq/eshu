// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package local

import (
	"context"
	"time"
)

// IdentityAPITokenListItem is the metadata-only view of one API token
// that is safe to return to the owning subject. It never includes token_hash,
// display_handle_hash, or any raw credential value.
// display_handle_hash is omitted intentionally — it is SHA-256(display_label)
// and presenting a hash as a human label is misleading. DisplayLabel (issue
// #3708) is the real, non-secret operator-facing label and is safe to
// return as-is.
type IdentityAPITokenListItem struct {
	TokenID      string
	TokenClass   string
	DisplayLabel string
	IssuedAt     time.Time
	ExpiresAt    time.Time
	RevokedAt    time.Time
}

// IdentityMFAStatus is the safe-to-expose MFA state for one identity.
// It never includes credential handles, recovery code hashes, or factor IDs.
type IdentityMFAStatus struct {
	HasActiveMFA bool
	// FactorKind is the active MFA factor kind (e.g. "recovery_code") when
	// HasActiveMFA is true, and empty otherwise.
	FactorKind string
}

// IdentityProfileLister is the read surface for per-subject profile
// aggregation. It extends IdentityStore with list, MFA status, and
// TOTP enrollment operations.
type IdentityProfileLister interface {
	IdentityStore
	// ListAPITokensBySubject returns metadata-only token rows owned by the
	// subject identified by subjectIDHash. The result never includes token_hash.
	ListAPITokensBySubject(ctx context.Context, subjectIDHash string, asOf time.Time) ([]IdentityAPITokenListItem, error)
	// GetLocalIdentityMFAStatus returns the safe MFA state for the subject.
	// The result never includes credential handles or recovery hashes.
	GetLocalIdentityMFAStatus(ctx context.Context, subjectIDHash string, asOf time.Time) (IdentityMFAStatus, error)
	// ResolveLocalIdentityUserID resolves the internal user_id for a
	// session's subjectIDHash (issue #4986). Self-service TOTP enrollment
	// only ever holds a session's subjectIDHash; ok is false, with no error,
	// when no live user matches.
	ResolveLocalIdentityUserID(ctx context.Context, subjectIDHash string) (userID string, ok bool, err error)
	// BeginLocalIdentityTOTPEnrollment seals and persists a PENDING TOTP
	// factor for begin.UserID (issue #4986). The factor cannot satisfy MFA
	// login until ConfirmLocalIdentityTOTPEnrollment activates it.
	BeginLocalIdentityTOTPEnrollment(ctx context.Context, begin IdentityTOTPEnrollmentBegin) error
	// ConfirmLocalIdentityTOTPEnrollment verifies the first submitted code
	// against a pending TOTP factor and activates it on match.
	ConfirmLocalIdentityTOTPEnrollment(ctx context.Context, confirm IdentityTOTPEnrollmentConfirm) error
}

// IdentityTOTPEnrollmentBegin starts TOTP enrollment for one user
// (issue #4986). SecretPlaintext is sealed immediately by the store and
// never returned, logged, or persisted unsealed.
type IdentityTOTPEnrollmentBegin struct {
	UserID          string
	FactorID        string
	SecretPlaintext []byte
	CreatedAt       time.Time
}

// IdentityTOTPEnrollmentConfirm verifies the first submitted TOTP code
// against a pending enrollment (issue #4986).
type IdentityTOTPEnrollmentConfirm struct {
	UserID   string
	FactorID string
	Code     string
	Now      time.Time
}
