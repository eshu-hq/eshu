package query

import (
	"context"
	"time"
)

// LocalIdentityAPITokenListItem is the metadata-only view of one API token
// that is safe to return to the owning subject. It never includes token_hash
// or any raw credential value.
type LocalIdentityAPITokenListItem struct {
	TokenID    string
	TokenClass string
	// DisplayLabel is the display_handle_hash value stored at creation time.
	// The raw label is not stored server-side; this field exposes the hash.
	DisplayLabel string
	IssuedAt     time.Time
	ExpiresAt    time.Time
	RevokedAt    time.Time
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
// aggregation. It extends LocalIdentityStore with list and MFA status queries.
type LocalIdentityProfileLister interface {
	LocalIdentityStore
	// ListAPITokensBySubject returns metadata-only token rows owned by the
	// subject identified by subjectIDHash. The result never includes token_hash.
	ListAPITokensBySubject(ctx context.Context, subjectIDHash string, asOf time.Time) ([]LocalIdentityAPITokenListItem, error)
	// GetLocalIdentityMFAStatus returns the safe MFA state for the subject.
	// The result never includes credential handles or recovery hashes.
	GetLocalIdentityMFAStatus(ctx context.Context, subjectIDHash string, asOf time.Time) (LocalIdentityMFAStatus, error)
}
