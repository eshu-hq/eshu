// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"strings"
	"time"
)

// Normalize/validate helpers for the bootstrap-credential inputs
// (identity_bootstrap_credential.go), split into their own file to keep both
// under the 500-line cap.

func normalizeBootstrapCredentialSeal(seal BootstrapCredentialSeal) BootstrapCredentialSeal {
	seal.TenantID = strings.TrimSpace(seal.TenantID)
	seal.WorkspaceID = strings.TrimSpace(seal.WorkspaceID)
	seal.SubjectIDHash = strings.TrimSpace(seal.SubjectIDHash)
	seal.UsernameHash = strings.TrimSpace(seal.UsernameHash)
	seal.SealedCredential = strings.TrimSpace(seal.SealedCredential)
	seal.KeyID = strings.TrimSpace(seal.KeyID)
	if seal.GeneratedAt.IsZero() {
		seal.GeneratedAt = time.Now().UTC()
	} else {
		seal.GeneratedAt = seal.GeneratedAt.UTC()
	}
	return seal
}

func validateBootstrapCredentialSeal(seal BootstrapCredentialSeal) error {
	if seal.TenantID == "" || seal.WorkspaceID == "" || seal.SubjectIDHash == "" ||
		seal.UsernameHash == "" || seal.SealedCredential == "" || seal.KeyID == "" {
		return errors.New("bootstrap credential seal is incomplete")
	}
	return nil
}

func normalizeResetBootstrapCredentialInput(in ResetBootstrapCredentialInput) ResetBootstrapCredentialInput {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.WorkspaceID = strings.TrimSpace(in.WorkspaceID)
	in.SealedCredential = strings.TrimSpace(in.SealedCredential)
	in.KeyID = strings.TrimSpace(in.KeyID)
	in.PasswordHash = strings.TrimSpace(in.PasswordHash)
	in.PasswordAlgorithm = strings.TrimSpace(in.PasswordAlgorithm)
	in.PasswordParametersHash = strings.TrimSpace(in.PasswordParametersHash)
	in.MFAFactorID = strings.TrimSpace(in.MFAFactorID)
	in.RecoveryCodeHash = strings.TrimSpace(in.RecoveryCodeHash)
	if in.ResetAt.IsZero() {
		in.ResetAt = time.Now().UTC()
	} else {
		in.ResetAt = in.ResetAt.UTC()
	}
	return in
}

func validateResetBootstrapCredentialInput(in ResetBootstrapCredentialInput) error {
	if in.TenantID == "" || in.WorkspaceID == "" || in.SealedCredential == "" || in.KeyID == "" ||
		in.PasswordHash == "" || in.PasswordAlgorithm == "" || in.PasswordParametersHash == "" ||
		in.MFAFactorID == "" || in.RecoveryCodeHash == "" {
		return errors.New("reset bootstrap credential input is incomplete")
	}
	return nil
}
