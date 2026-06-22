package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var errLocalIdentityRecoveryCodeInvalid = errors.New("local identity recovery code invalid")

func selectLocalIdentityInvitation(
	ctx context.Context,
	db ExecQueryer,
	inviteCodeHash string,
	asOf time.Time,
) (localIdentityInvitationRow, bool, error) {
	rows, err := db.QueryContext(ctx, selectLocalIdentityInvitationForAcceptQuery, inviteCodeHash, asOf)
	if err != nil {
		return localIdentityInvitationRow{}, false, fmt.Errorf("select local identity invitation: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return localIdentityInvitationRow{}, false, nil
	}
	var row localIdentityInvitationRow
	if err := rows.Scan(&row.InviteID, &row.TenantID, &row.WorkspaceID, &row.RoleID, &row.PolicyRevisionHash); err != nil {
		return localIdentityInvitationRow{}, false, fmt.Errorf("select local identity invitation: %w", err)
	}
	return row, true, rows.Err()
}

func selectLocalIdentityCredential(
	ctx context.Context,
	db ExecQueryer,
	subjectIDHash string,
	asOf time.Time,
) (localIdentityCredentialRow, bool, error) {
	rows, err := db.QueryContext(ctx, selectLocalIdentityCredentialQuery, subjectIDHash, asOf)
	if err != nil {
		return localIdentityCredentialRow{}, false, fmt.Errorf("select local identity credential: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return localIdentityCredentialRow{}, false, nil
	}
	var row localIdentityCredentialRow
	var disabledAt sql.NullTime
	var lockedUntil sql.NullTime
	if err := rows.Scan(
		&row.UserID,
		&row.TenantID,
		&row.WorkspaceID,
		&row.SubjectIDHash,
		&row.PasswordHash,
		&row.Status,
		&disabledAt,
		&lockedUntil,
		&row.FailedAttempts,
		&row.HasAdminRole,
		&row.HasActiveMFA,
		&row.PolicyRevisionHash,
	); err != nil {
		return localIdentityCredentialRow{}, false, fmt.Errorf("select local identity credential: %w", err)
	}
	if disabledAt.Valid {
		row.DisabledAt = disabledAt.Time.UTC()
	}
	if lockedUntil.Valid {
		row.LockedUntil = lockedUntil.Time.UTC()
	}
	return row, true, rows.Err()
}

func consumeLocalIdentityRecoveryCode(
	ctx context.Context,
	db ExecQueryer,
	userID string,
	attempt LocalIdentityAuthenticationAttempt,
) error {
	consumeAt := attempt.ConsumeRecoveryCodeAt
	if consumeAt.IsZero() {
		consumeAt = attempt.Now
	}
	result, err := db.ExecContext(ctx, consumeLocalIdentityRecoveryCodeQuery, userID, attempt.MFARecoveryCodeHash, consumeAt)
	if err != nil {
		return fmt.Errorf("consume local identity recovery code: %w", err)
	}
	if result != nil {
		affected, err := result.RowsAffected()
		if err == nil && affected == 0 {
			return errLocalIdentityRecoveryCodeInvalid
		}
	}
	return nil
}

func (s *IdentitySubjectStore) recordFailedLocalIdentityAttempt(
	ctx context.Context,
	row localIdentityCredentialRow,
	now time.Time,
) (LocalIdentityAuthenticationResult, error) {
	failedAttempts := row.FailedAttempts + 1
	lockedUntil := time.Time{}
	status := LocalIdentityAuthInvalid
	if failedAttempts >= defaultLocalIdentityLockoutThreshold {
		lockedUntil = now.Add(defaultLocalIdentityLockoutWindow)
		status = LocalIdentityAuthLocked
	}
	if _, err := s.db.ExecContext(
		ctx,
		upsertLocalIdentityFailedAttemptQuery,
		row.UserID,
		defaultLocalIdentityLockoutThreshold,
		nullTime(lockedUntil),
		now,
	); err != nil {
		return LocalIdentityAuthenticationResult{}, fmt.Errorf("record local identity failed attempt: %w", err)
	}
	return LocalIdentityAuthenticationResult{Status: status, LockedUntil: lockedUntil}, nil
}

type localIdentityUserCredentialRecord struct {
	UserID                 string
	SubjectIDHash          string
	ProfileHandleHash      string
	CredentialID           string
	PasswordHash           string
	PasswordAlgorithm      string
	PasswordParametersHash string
	CreatedAt              time.Time
}

type localIdentityRoleAssignment struct {
	TenantID           string
	WorkspaceID        string
	UserID             string
	RoleID             string
	Source             string
	PolicyRevisionHash string
	AssignedAt         time.Time
}

func insertLocalIdentityUserCredential(
	ctx context.Context,
	db ExecQueryer,
	record localIdentityUserCredentialRecord,
) error {
	if _, err := db.ExecContext(ctx, insertLocalIdentityUserQuery, record.UserID, record.SubjectIDHash, record.ProfileHandleHash, record.CreatedAt); err != nil {
		return fmt.Errorf("insert local identity user: %w", err)
	}
	if _, err := db.ExecContext(
		ctx,
		insertLocalIdentityCredentialQuery,
		record.CredentialID,
		record.UserID,
		record.PasswordHash,
		record.PasswordAlgorithm,
		record.PasswordParametersHash,
		record.CreatedAt,
	); err != nil {
		return fmt.Errorf("insert local identity credential: %w", err)
	}
	return nil
}

func insertLocalIdentityMFA(
	ctx context.Context,
	db ExecQueryer,
	userID string,
	factorID string,
	factorKind string,
	credentialHandle string,
	recoveryCodeHashes []string,
	createdAt time.Time,
) error {
	if _, err := db.ExecContext(ctx, insertLocalIdentityMFAFactorQuery, factorID, userID, factorKind, credentialHandle, createdAt); err != nil {
		return fmt.Errorf("insert local identity mfa factor: %w", err)
	}
	for _, hash := range recoveryCodeHashes {
		if _, err := db.ExecContext(ctx, insertLocalIdentityRecoveryCodeQuery, userID, factorID, hash, createdAt); err != nil {
			return fmt.Errorf("insert local identity recovery code: %w", err)
		}
	}
	return nil
}

func assignLocalIdentityRole(
	ctx context.Context,
	db ExecQueryer,
	assignment localIdentityRoleAssignment,
) error {
	if _, err := db.ExecContext(
		ctx,
		upsertLocalIdentityRoleQuery,
		assignment.TenantID,
		assignment.RoleID,
		localIdentityRoleKeyHash(assignment.RoleID),
		assignment.PolicyRevisionHash,
		assignment.AssignedAt,
	); err != nil {
		return fmt.Errorf("upsert local identity role: %w", err)
	}
	if _, err := db.ExecContext(
		ctx,
		insertLocalIdentityMembershipQuery,
		assignment.TenantID,
		assignment.WorkspaceID,
		assignment.UserID,
		assignment.Source,
		assignment.PolicyRevisionHash,
		assignment.AssignedAt,
	); err != nil {
		return fmt.Errorf("insert local identity membership: %w", err)
	}
	if _, err := db.ExecContext(
		ctx,
		insertLocalIdentityMembershipRoleQuery,
		assignment.TenantID,
		assignment.WorkspaceID,
		assignment.UserID,
		assignment.RoleID,
		assignment.Source,
		assignment.PolicyRevisionHash,
		assignment.AssignedAt,
	); err != nil {
		return fmt.Errorf("insert local identity membership role: %w", err)
	}
	return nil
}

func localIdentityCredentialID(userID string, suffix string) string {
	return "credential:" + strings.TrimSpace(userID) + ":" + strings.TrimSpace(suffix)
}

func localIdentityRoleKeyHash(roleID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(roleID)))
	return "sha256:" + hex.EncodeToString(sum[:])
}
