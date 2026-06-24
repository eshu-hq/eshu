package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// LocalIdentityMFAStatus is the safe-to-expose MFA state for one identity.
// It never includes credential handles, recovery code hashes, or factor IDs.
type LocalIdentityMFAStatus struct {
	HasActiveMFA bool
	// FactorKind is the active MFA factor kind (e.g. "recovery_code") when
	// HasActiveMFA is true, and empty otherwise.
	FactorKind string
}

// getLocalIdentityMFAStatusQuery returns whether the subject has an active MFA
// factor and its kind. It never selects credential handles or recovery hashes.
const getLocalIdentityMFAStatusQuery = `
SELECT
    EXISTS (
        SELECT 1
        FROM identity_mfa_factors f
        JOIN identity_users u ON u.user_id = f.user_id
        WHERE u.subject_id_hash = $1
          AND f.status = 'active'
          AND f.revoked_at IS NULL
    ) AS has_active_mfa,
    COALESCE((
        SELECT f.factor_kind
        FROM identity_mfa_factors f
        JOIN identity_users u ON u.user_id = f.user_id
        WHERE u.subject_id_hash = $1
          AND f.status = 'active'
          AND f.revoked_at IS NULL
          AND f.created_at <= $2
        ORDER BY f.created_at DESC
        LIMIT 1
    ), '') AS factor_kind
`

// GetLocalIdentityMFAStatus returns the safe MFA status for the subject
// identified by subjectIDHash. The result never includes credential handles,
// recovery code hashes, or factor IDs.
func (s *IdentitySubjectStore) GetLocalIdentityMFAStatus(
	ctx context.Context,
	subjectIDHash string,
	asOf time.Time,
) (LocalIdentityMFAStatus, error) {
	if s.db == nil {
		return LocalIdentityMFAStatus{}, errors.New("identity subject store database is required")
	}
	subjectIDHash = strings.TrimSpace(subjectIDHash)
	if subjectIDHash == "" {
		return LocalIdentityMFAStatus{}, errors.New("subject_id_hash is required")
	}
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	rows, err := s.db.QueryContext(ctx, getLocalIdentityMFAStatusQuery, subjectIDHash, asOf.UTC())
	if err != nil {
		return LocalIdentityMFAStatus{}, fmt.Errorf("get local identity mfa status: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return LocalIdentityMFAStatus{}, fmt.Errorf("get local identity mfa status: %w", err)
		}
		return LocalIdentityMFAStatus{}, nil
	}
	var status LocalIdentityMFAStatus
	if err := rows.Scan(&status.HasActiveMFA, &status.FactorKind); err != nil {
		return LocalIdentityMFAStatus{}, fmt.Errorf("scan local identity mfa status: %w", err)
	}
	if err := rows.Err(); err != nil {
		return LocalIdentityMFAStatus{}, fmt.Errorf("get local identity mfa status: %w", err)
	}
	return status, nil
}
