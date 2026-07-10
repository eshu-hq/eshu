// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"strings"
	"time"
)

func normalizeBootstrapRecord(record LocalIdentityBootstrapRecord) LocalIdentityBootstrapRecord {
	record.TenantID = strings.TrimSpace(record.TenantID)
	record.WorkspaceID = strings.TrimSpace(record.WorkspaceID)
	record.UserID = strings.TrimSpace(record.UserID)
	record.SubjectIDHash = strings.TrimSpace(record.SubjectIDHash)
	record.ProfileHandleHash = strings.TrimSpace(record.ProfileHandleHash)
	record.PasswordHash = strings.TrimSpace(record.PasswordHash)
	record.PasswordAlgorithm = strings.TrimSpace(record.PasswordAlgorithm)
	record.PasswordParametersHash = strings.TrimSpace(record.PasswordParametersHash)
	record.MFAFactorID = strings.TrimSpace(record.MFAFactorID)
	record.MFAFactorKind = strings.TrimSpace(record.MFAFactorKind)
	record.MFACredentialHandle = strings.TrimSpace(record.MFACredentialHandle)
	record.PolicyRevisionHash = strings.TrimSpace(record.PolicyRevisionHash)
	record.RecoveryCodeHashes = cleanLocalIdentityHashes(record.RecoveryCodeHashes)
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	} else {
		record.CreatedAt = record.CreatedAt.UTC()
	}
	return record
}

func validateBootstrapRecord(record LocalIdentityBootstrapRecord) error {
	if record.TenantID == "" || record.WorkspaceID == "" || record.UserID == "" ||
		record.SubjectIDHash == "" || record.PasswordHash == "" ||
		record.PasswordAlgorithm == "" || record.PasswordParametersHash == "" ||
		record.PolicyRevisionHash == "" {
		return errors.New("local identity bootstrap record is incomplete")
	}
	if record.MFAFactorID == "" || record.MFAFactorKind == "" || len(record.RecoveryCodeHashes) == 0 {
		return ErrLocalIdentityAdminMFARequired
	}
	return nil
}

func normalizeInvitationRecord(record LocalIdentityInvitationRecord) LocalIdentityInvitationRecord {
	record.InviteID = strings.TrimSpace(record.InviteID)
	record.TenantID = strings.TrimSpace(record.TenantID)
	record.WorkspaceID = strings.TrimSpace(record.WorkspaceID)
	record.InviteCodeHash = strings.TrimSpace(record.InviteCodeHash)
	record.InviteeHandleHash = strings.TrimSpace(record.InviteeHandleHash)
	record.InviterSubjectIDHash = strings.TrimSpace(record.InviterSubjectIDHash)
	record.RoleID = strings.TrimSpace(record.RoleID)
	record.Status = strings.TrimSpace(record.Status)
	record.PolicyRevisionHash = strings.TrimSpace(record.PolicyRevisionHash)
	record.ExpiresAt = record.ExpiresAt.UTC()
	record.CreatedAt = record.CreatedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	if record.Status == "" {
		record.Status = "active"
	}
	return record
}

func validateInvitationRecord(record LocalIdentityInvitationRecord) error {
	if record.InviteID == "" || record.TenantID == "" || record.WorkspaceID == "" ||
		record.InviteCodeHash == "" || record.RoleID == "" ||
		record.PolicyRevisionHash == "" || record.ExpiresAt.IsZero() ||
		record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() {
		return errors.New("local identity invitation record is incomplete")
	}
	if !record.ExpiresAt.After(record.CreatedAt) {
		return errors.New("local identity invitation expires_at must be after created_at")
	}
	return nil
}

func normalizeInvitationAcceptance(
	acceptance LocalIdentityInvitationAcceptance,
) LocalIdentityInvitationAcceptance {
	acceptance.InviteCodeHash = strings.TrimSpace(acceptance.InviteCodeHash)
	acceptance.UserID = strings.TrimSpace(acceptance.UserID)
	acceptance.SubjectIDHash = strings.TrimSpace(acceptance.SubjectIDHash)
	acceptance.ProfileHandleHash = strings.TrimSpace(acceptance.ProfileHandleHash)
	acceptance.PasswordHash = strings.TrimSpace(acceptance.PasswordHash)
	acceptance.PasswordAlgorithm = strings.TrimSpace(acceptance.PasswordAlgorithm)
	acceptance.PasswordParametersHash = strings.TrimSpace(acceptance.PasswordParametersHash)
	acceptance.MFAFactorID = strings.TrimSpace(acceptance.MFAFactorID)
	acceptance.MFAFactorKind = strings.TrimSpace(acceptance.MFAFactorKind)
	acceptance.MFACredentialHandle = strings.TrimSpace(acceptance.MFACredentialHandle)
	acceptance.RecoveryCodeHashes = cleanLocalIdentityHashes(acceptance.RecoveryCodeHashes)
	if acceptance.AcceptedAt.IsZero() {
		acceptance.AcceptedAt = time.Now().UTC()
	} else {
		acceptance.AcceptedAt = acceptance.AcceptedAt.UTC()
	}
	return acceptance
}

func validateInvitationAcceptance(acceptance LocalIdentityInvitationAcceptance) error {
	if acceptance.InviteCodeHash == "" || acceptance.UserID == "" ||
		acceptance.SubjectIDHash == "" || acceptance.PasswordHash == "" ||
		acceptance.PasswordAlgorithm == "" || acceptance.PasswordParametersHash == "" {
		return errors.New("local identity invitation acceptance is incomplete")
	}
	if acceptance.MFAFactorID != "" && (acceptance.MFAFactorKind == "" || len(acceptance.RecoveryCodeHashes) == 0) {
		return errors.New("local identity mfa factor requires recovery code hashes")
	}
	return nil
}

func normalizeAuthenticationAttempt(
	attempt LocalIdentityAuthenticationAttempt,
) LocalIdentityAuthenticationAttempt {
	attempt.SubjectIDHash = strings.TrimSpace(attempt.SubjectIDHash)
	attempt.MFARecoveryCodeHash = strings.TrimSpace(attempt.MFARecoveryCodeHash)
	if attempt.Now.IsZero() {
		attempt.Now = time.Now().UTC()
	} else {
		attempt.Now = attempt.Now.UTC()
	}
	if !attempt.ConsumeRecoveryCodeAt.IsZero() {
		attempt.ConsumeRecoveryCodeAt = attempt.ConsumeRecoveryCodeAt.UTC()
	}
	return attempt
}

func normalizePasswordReset(reset LocalIdentityPasswordReset) LocalIdentityPasswordReset {
	reset.UserID = strings.TrimSpace(reset.UserID)
	reset.CredentialID = strings.TrimSpace(reset.CredentialID)
	reset.PasswordHash = strings.TrimSpace(reset.PasswordHash)
	reset.PasswordAlgorithm = strings.TrimSpace(reset.PasswordAlgorithm)
	reset.PasswordParametersHash = strings.TrimSpace(reset.PasswordParametersHash)
	if reset.ResetAt.IsZero() {
		reset.ResetAt = time.Now().UTC()
	} else {
		reset.ResetAt = reset.ResetAt.UTC()
	}
	return reset
}

func validatePasswordReset(reset LocalIdentityPasswordReset) error {
	if reset.UserID == "" || reset.CredentialID == "" || reset.PasswordHash == "" ||
		reset.PasswordAlgorithm == "" || reset.PasswordParametersHash == "" {
		return errors.New("local identity password reset is incomplete")
	}
	return nil
}

func normalizePasswordRotation(rotation LocalIdentityPasswordRotation) LocalIdentityPasswordRotation {
	rotation.SubjectIDHash = strings.TrimSpace(rotation.SubjectIDHash)
	rotation.NewPasswordHash = strings.TrimSpace(rotation.NewPasswordHash)
	rotation.NewPasswordAlgorithm = strings.TrimSpace(rotation.NewPasswordAlgorithm)
	rotation.NewPasswordParametersHash = strings.TrimSpace(rotation.NewPasswordParametersHash)
	rotation.CredentialID = strings.TrimSpace(rotation.CredentialID)
	rotation.MFARecoveryCodeHash = strings.TrimSpace(rotation.MFARecoveryCodeHash)
	if rotation.Now.IsZero() {
		rotation.Now = time.Now().UTC()
	} else {
		rotation.Now = rotation.Now.UTC()
	}
	if !rotation.ConsumeRecoveryCodeAt.IsZero() {
		rotation.ConsumeRecoveryCodeAt = rotation.ConsumeRecoveryCodeAt.UTC()
	}
	return rotation
}

// validatePasswordRotation checks only the handler-controlled fields
// (credential id, new-password hash/algorithm/parameters hash): these come
// from server-side ID generation and bcrypt hashing, so an empty value means
// a caller wiring bug, not a bad end-user request, and is a hard error.
// SubjectIDHash and CurrentPassword are attacker-controlled request input;
// RotateLocalIdentityPassword checks those itself and returns
// LocalIdentityAuthInvalid (no error) for an empty value, mirroring
// AuthenticateLocalIdentity's convention for malformed login proof.
func validatePasswordRotation(rotation LocalIdentityPasswordRotation) error {
	if rotation.NewPasswordHash == "" || rotation.NewPasswordAlgorithm == "" ||
		rotation.NewPasswordParametersHash == "" || rotation.CredentialID == "" {
		return errors.New("local identity password rotation is incomplete")
	}
	return nil
}

func normalizeMFAReset(reset LocalIdentityMFAReset) LocalIdentityMFAReset {
	reset.UserID = strings.TrimSpace(reset.UserID)
	reset.MFAFactorID = strings.TrimSpace(reset.MFAFactorID)
	reset.MFAFactorKind = strings.TrimSpace(reset.MFAFactorKind)
	reset.MFACredentialHandle = strings.TrimSpace(reset.MFACredentialHandle)
	reset.RecoveryCodeHashes = cleanLocalIdentityHashes(reset.RecoveryCodeHashes)
	if reset.ResetAt.IsZero() {
		reset.ResetAt = time.Now().UTC()
	} else {
		reset.ResetAt = reset.ResetAt.UTC()
	}
	return reset
}

func validateMFAReset(reset LocalIdentityMFAReset) error {
	if reset.UserID == "" || reset.MFAFactorID == "" || reset.MFAFactorKind == "" ||
		len(reset.RecoveryCodeHashes) == 0 {
		return errors.New("local identity mfa reset is incomplete")
	}
	return nil
}

func normalizeDisableUser(disable LocalIdentityDisableUser) LocalIdentityDisableUser {
	disable.UserID = strings.TrimSpace(disable.UserID)
	if disable.DisabledAt.IsZero() {
		disable.DisabledAt = time.Now().UTC()
	} else {
		disable.DisabledAt = disable.DisabledAt.UTC()
	}
	return disable
}

func validateDisableUser(disable LocalIdentityDisableUser) error {
	if disable.UserID == "" {
		return errors.New("local identity disable user requires user_id")
	}
	return nil
}

func normalizeBreakGlassWindow(window LocalIdentityBreakGlassWindow) LocalIdentityBreakGlassWindow {
	window.RecoveryID = strings.TrimSpace(window.RecoveryID)
	window.TenantID = strings.TrimSpace(window.TenantID)
	window.WorkspaceID = strings.TrimSpace(window.WorkspaceID)
	window.SubjectIDHash = strings.TrimSpace(window.SubjectIDHash)
	window.BreakGlassCodeHash = strings.TrimSpace(window.BreakGlassCodeHash)
	window.Status = strings.TrimSpace(window.Status)
	window.ReasonCode = strings.TrimSpace(window.ReasonCode)
	window.PolicyRevisionHash = strings.TrimSpace(window.PolicyRevisionHash)
	window.EnabledAt = window.EnabledAt.UTC()
	window.ExpiresAt = window.ExpiresAt.UTC()
	window.CreatedAt = window.CreatedAt.UTC()
	window.UpdatedAt = window.UpdatedAt.UTC()
	if window.Status == "" {
		window.Status = "active"
	}
	return window
}

func validateBreakGlassWindow(window LocalIdentityBreakGlassWindow) error {
	if window.RecoveryID == "" || window.TenantID == "" || window.WorkspaceID == "" ||
		window.SubjectIDHash == "" || window.BreakGlassCodeHash == "" ||
		window.ReasonCode == "" || window.PolicyRevisionHash == "" ||
		window.EnabledAt.IsZero() || window.ExpiresAt.IsZero() {
		return errors.New("local identity break-glass window is incomplete")
	}
	if !window.ExpiresAt.After(window.EnabledAt) {
		return errors.New("local identity break-glass expires_at must be after enabled_at")
	}
	return nil
}

func cleanLocalIdentityHashes(values []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}
	return cleaned
}
