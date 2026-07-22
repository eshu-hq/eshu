// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrBootstrapCredentialNotFound indicates no bootstrap credential row exists
// for the given tenant/workspace, so ResetBootstrapCredential has nothing to
// rotate.
var ErrBootstrapCredentialNotFound = errors.New("identity bootstrap credential not found")

// BootstrapAdminTenantID and BootstrapAdminWorkspaceID are the fixed
// (tenant_id, workspace_id) slot the bootstrap admin always occupies. Eshu's
// self-hosted deployment model is single-tenant, so there is no
// per-deployment override; both go/cmd/api (seeding) and go/cmd/eshu (the
// `eshu admin initial-credential`/`reset-initial-credential` CLI) import
// these from here rather than each hard-coding the literal, so the two
// independent binaries can never drift out of agreement on which row they
// read and write.
const (
	BootstrapAdminTenantID    = "default"
	BootstrapAdminWorkspaceID = "default"
)

// BootstrapCredentialAAD returns the deterministic additional-authenticated-
// data bytes callers bind the one-time admin bootstrap credential envelope
// to (epic #4962 AAD scheme "eshu:onetime-admin:v1"). It is never stored in
// the envelope; secretcrypto.Keyring.Seal and Open callers reconstruct it
// from the row's tenant and workspace identity and must pass the identical
// bytes to both, or Open fails closed with secretcrypto.ErrDecrypt.
func BootstrapCredentialAAD(tenantID, workspaceID string) []byte {
	return []byte(fmt.Sprintf("eshu:onetime-admin:v1|%s|%s", tenantID, workspaceID))
}

// BootstrapCredentialSeal carries an already-sealed (secretcrypto.Keyring.Seal)
// one-time admin credential envelope to persist for one (tenant, workspace).
// The store never seals or opens envelopes itself; callers own the crypto
// substrate (go/internal/secretcrypto) and pass already-sealed text here,
// the same "caller hashes/seals, store only persists" split
// LocalIdentityBootstrapRecord.PasswordHash uses for the local user row.
type BootstrapCredentialSeal struct {
	TenantID         string
	WorkspaceID      string
	SubjectIDHash    string
	UsernameHash     string
	SealedCredential string
	KeyID            string
	GeneratedAt      time.Time
}

// OpenableBootstrapCredential is the retrievable (not yet consumed) sealed
// one-time admin credential envelope, returned for CLI/status retrieval.
type OpenableBootstrapCredential struct {
	SealedCredential string
	KeyID            string
}

// ResetBootstrapCredentialInput carries the caller-generated replacement
// plaintext's already-sealed envelope and already-hashed password for an
// atomic reset. The store never generates plaintext, seals, or hashes;
// callers (the `eshu admin reset-initial-credential` CLI) own that via
// crypto/rand, secretcrypto.Keyring.Seal, and bcrypt.
type ResetBootstrapCredentialInput struct {
	TenantID               string
	WorkspaceID            string
	SealedCredential       string
	KeyID                  string
	PasswordHash           string
	PasswordAlgorithm      string
	PasswordParametersHash string
	// MFAFactorID is a caller-generated fresh identity_mfa_factors.factor_id
	// for the re-enrolled recovery-code factor (issue #5602). Required: a
	// reset that rotates the password without also re-enrolling the MFA
	// recovery factor leaves the printed recovery code unable to
	// authenticate, which is the bug this field exists to close.
	MFAFactorID string
	// RecoveryCodeHash is query.IdentityHash(<the freshly generated plaintext
	// recovery code>), the same hash-only shape every other recovery-code
	// field this store persists uses. ResetBootstrapCredential never sees or
	// generates the plaintext.
	RecoveryCodeHash string
	ResetAt          time.Time
}

// HasBootstrappedLocalIdentity reports whether any local identity already
// exists. It is a cheap, lock-free read used only to let a caller skip
// redundant crypto work (bcrypt hashing, AES-GCM sealing) before attempting a
// bootstrap seed on a restart; it is never the correctness boundary for "was
// seeding already done" — BootstrapLocalIdentity's and
// GenerateBootstrapAdminWithCredential's own pg_advisory_xact_lock(3455)
// check-then-insert is. A benign race between this read and a concurrent
// replica's bootstrap attempt is harmless: the atomic method's own
// check-then-insert still applies.
func (s *IdentitySubjectStore) HasBootstrappedLocalIdentity(ctx context.Context) (bool, error) {
	if s.db == nil {
		return false, errors.New("identity subject store database is required")
	}
	count, err := countExistingLocalIdentityUsers(ctx, s.db)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GenerateBootstrapCredential idempotently inserts the sealed one-time admin
// credential envelope for one (tenant, workspace), guarded by
// pg_advisory_xact_lock(3456) (3455 is BootstrapLocalIdentity's own
// local-identity lock; this method only ever takes 3456 alone).
// GenerateBootstrapAdminWithCredential below takes both 3455 and 3456 in the
// same transaction, always in that fixed order — see its doc comment for why
// that ordering rules out a deadlock between the two keys. inserted is true
// only on a genuine first insert; a conflict (already provisioned) returns
// inserted=false with no
// error, so a caller that races another instance at startup must not re-seal
// or re-log the one-time banner in that case.
//
// Callers that are also creating the identity this credential belongs to in
// the same call (the ESHU_AUTH_BOOTSTRAP_MODE=generated startup path) should
// use GenerateBootstrapAdminWithCredential instead: calling
// BootstrapLocalIdentity and this method as two separate transactions leaves
// a crash window between them where the identity exists with no retrievable
// credential and no reset path (Reset requires a pre-existing credential
// row). This method remains useful on its own for a caller that already
// knows the identity was created in an earlier, already-committed step.
func (s *IdentitySubjectStore) GenerateBootstrapCredential(
	ctx context.Context,
	seal BootstrapCredentialSeal,
) (bool, error) {
	tx, err := s.beginLocalIdentityTx(ctx)
	if err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	inserted, err := insertBootstrapCredentialInTx(ctx, tx, seal)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit generate bootstrap credential: %w", err)
	}
	committed = true
	return inserted, nil
}

// GenerateBootstrapAdminWithCredential atomically creates the first local
// owner/admin identity (BootstrapLocalIdentity's own check-then-insert
// semantics, pg_advisory_xact_lock(3455)) AND seals its one-time generated
// credential envelope (pg_advisory_xact_lock(3456)) in the SAME transaction.
// This is the crash-safe composition ESHU_AUTH_BOOTSTRAP_MODE=generated must
// use: taking both locks in the same session in a fixed order (3455 then
// 3456) is safe — deadlock only becomes possible when two DIFFERENT
// transactions each hold one lock and block waiting for the other, which
// cannot happen here since one session acquires both. A process crash
// between "identity created" and "credential sealed" would otherwise strand
// an admin identity with no retrievable credential and no reset path
// (ResetBootstrapCredential requires a pre-existing credential row); doing
// both writes in one transaction means a crash before commit leaves neither
// write applied, and a clean restart retries the whole thing with a fresh
// generated password.
func (s *IdentitySubjectStore) GenerateBootstrapAdminWithCredential(
	ctx context.Context,
	identity LocalIdentityBootstrapRecord,
	seal BootstrapCredentialSeal,
) (bool, error) {
	identity = normalizeBootstrapRecord(identity)
	if err := validateBootstrapRecord(identity); err != nil {
		return false, err
	}

	tx, err := s.beginLocalIdentityTx(ctx)
	if err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, localIdentityBootstrapLockQuery); err != nil {
		return false, fmt.Errorf("lock local identity bootstrap: %w", err)
	}
	count, err := countExistingLocalIdentityUsers(ctx, tx)
	if err != nil {
		return false, err
	}
	if count > 0 {
		return false, ErrLocalIdentityBootstrapCompleted
	}
	if err := insertBootstrapLocalIdentity(ctx, tx, identity); err != nil {
		return false, err
	}

	inserted, err := insertBootstrapCredentialInTx(ctx, tx, seal)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit generate bootstrap admin with credential: %w", err)
	}
	committed = true
	return inserted, nil
}

// insertBootstrapCredentialInTx performs the advisory-locked idempotent
// insert shared by GenerateBootstrapCredential and
// GenerateBootstrapAdminWithCredential.
func insertBootstrapCredentialInTx(ctx context.Context, tx Transaction, seal BootstrapCredentialSeal) (bool, error) {
	seal = normalizeBootstrapCredentialSeal(seal)
	if err := validateBootstrapCredentialSeal(seal); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, bootstrapCredentialLockQuery); err != nil {
		return false, fmt.Errorf("lock bootstrap credential: %w", err)
	}
	rows, err := tx.QueryContext(
		ctx, generateBootstrapCredentialQuery,
		seal.TenantID,
		seal.WorkspaceID,
		seal.SubjectIDHash,
		seal.UsernameHash,
		seal.SealedCredential,
		seal.KeyID,
		seal.GeneratedAt,
	)
	if err != nil {
		return false, fmt.Errorf("generate bootstrap credential: %w", err)
	}
	inserted := rows.Next()
	rowsErr := rows.Err()
	closeErr := rows.Close()
	if rowsErr != nil {
		return false, fmt.Errorf("generate bootstrap credential: %w", rowsErr)
	}
	if closeErr != nil {
		return false, fmt.Errorf("generate bootstrap credential: %w", closeErr)
	}
	return inserted, nil
}

// SelectBootstrapCredential returns the retrievable sealed envelope for one
// (tenant, workspace). found is false when no row exists, the row was
// already consumed, or its ciphertext was already cleared by a login.
func (s *IdentitySubjectStore) SelectBootstrapCredential(
	ctx context.Context,
	tenantID, workspaceID string,
) (OpenableBootstrapCredential, bool, error) {
	if s.db == nil {
		return OpenableBootstrapCredential{}, false, errors.New("identity subject store database is required")
	}
	tenantID = strings.TrimSpace(tenantID)
	workspaceID = strings.TrimSpace(workspaceID)
	if tenantID == "" || workspaceID == "" {
		return OpenableBootstrapCredential{}, false, errors.New("select bootstrap credential requires tenant_id and workspace_id")
	}
	rows, err := s.db.QueryContext(ctx, selectBootstrapCredentialQuery, tenantID, workspaceID)
	if err != nil {
		return OpenableBootstrapCredential{}, false, fmt.Errorf("select bootstrap credential: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return OpenableBootstrapCredential{}, false, rows.Err()
	}
	var out OpenableBootstrapCredential
	if err := rows.Scan(&out.SealedCredential, &out.KeyID); err != nil {
		return OpenableBootstrapCredential{}, false, fmt.Errorf("select bootstrap credential: %w", err)
	}
	return out, true, rows.Err()
}

// ConsumeBootstrapCredential destroys the retrievable ciphertext for one
// (tenant, workspace, subject) on the bootstrap subject's first successful
// local login, clearing sealed_credential and setting consumed_at.
// subject_id_hash scopes the update so a different subject's login never
// accidentally consumes another subject's still-pending bootstrap envelope.
// consumed is true only when this call performed the transition; repeat
// calls (already consumed, or no matching row for this tenant/workspace/
// subject) are a no-op returning consumed=false with no error, so
// AuthenticateLocalIdentity can call this unconditionally on every
// successful login without special-casing the non-bootstrap case.
func (s *IdentitySubjectStore) ConsumeBootstrapCredential(
	ctx context.Context,
	tenantID, workspaceID, subjectIDHash string,
	consumedAt time.Time,
) (bool, error) {
	if s.db == nil {
		return false, errors.New("identity subject store database is required")
	}
	tenantID = strings.TrimSpace(tenantID)
	workspaceID = strings.TrimSpace(workspaceID)
	subjectIDHash = strings.TrimSpace(subjectIDHash)
	if tenantID == "" || workspaceID == "" || subjectIDHash == "" {
		return false, nil
	}
	if consumedAt.IsZero() {
		consumedAt = time.Now().UTC()
	} else {
		consumedAt = consumedAt.UTC()
	}
	result, err := s.db.ExecContext(ctx, consumeBootstrapCredentialQuery, tenantID, workspaceID, subjectIDHash, consumedAt)
	if err != nil {
		return false, fmt.Errorf("consume bootstrap credential: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("consume bootstrap credential: %w", err)
	}
	return affected > 0, nil
}

// ResetBootstrapCredential atomically regenerates and re-seals the bootstrap
// credential envelope, rotates the matching bcrypt hash in
// identity_local_credentials, AND re-enrolls the owner's MFA recovery-code
// factor (reenrollBootstrapCredentialRecoveryFactor) in the same transaction,
// so the database password, the sealed envelope, and the actual MFA recovery
// factor a login checks can never diverge. Before issue #5602 this method
// rotated only the envelope and the password: the printed recovery code was
// never persisted, so only the original first-run code (if still held) could
// authenticate. It always clears consumed_at (a reset re-arms retrieval
// regardless of prior consumption) and increments reset_count. Guarded by the
// same pg_advisory_xact_lock(3456) as
// GenerateBootstrapCredential/GenerateBootstrapAdminWithCredential, so a
// concurrent Generate/Reset on the same row serializes correctly.
// ConsumeBootstrapCredential is deliberately lock-free (its atomic
// conditional UPDATE WHERE consumed_at IS NULL is itself the concurrency
// guard: a concurrent Consume racing a Reset either clears the row that
// Reset is about to overwrite, or observes consumed_at already cleared by
// Reset and correctly no-ops), so it never needs to take 3456. Returns
// ErrBootstrapCredentialNotFound when no row
// exists for the tenant/workspace (for example, the admin was seeded from
// ESHU_ADMIN_USERNAME/PASSWORD and has no generated envelope to reset).
func (s *IdentitySubjectStore) ResetBootstrapCredential(
	ctx context.Context,
	in ResetBootstrapCredentialInput,
) error {
	in = normalizeResetBootstrapCredentialInput(in)
	if err := validateResetBootstrapCredentialInput(in); err != nil {
		return err
	}
	tx, err := s.beginLocalIdentityTx(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, bootstrapCredentialLockQuery); err != nil {
		return fmt.Errorf("lock bootstrap credential: %w", err)
	}
	subjectIDHash, err := selectBootstrapCredentialSubject(ctx, tx, in.TenantID, in.WorkspaceID)
	if err != nil {
		return err
	}
	userID, err := selectBootstrapCredentialOwnerUserID(ctx, tx, subjectIDHash)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(
		ctx, resetBootstrapCredentialQuery,
		in.TenantID, in.WorkspaceID, in.SealedCredential, in.KeyID, in.ResetAt,
	); err != nil {
		return fmt.Errorf("reset bootstrap credential: %w", err)
	}
	result, err := tx.ExecContext(
		ctx, rotateBootstrapCredentialPasswordQuery,
		userID, in.PasswordHash, in.PasswordAlgorithm, in.PasswordParametersHash, in.ResetAt,
	)
	if err != nil {
		return fmt.Errorf("rotate bootstrap credential password: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rotate bootstrap credential password: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("rotate bootstrap credential password: no active local credential for owning user")
	}
	if err := reenrollBootstrapCredentialRecoveryFactor(ctx, tx, userID, in.MFAFactorID, in.RecoveryCodeHash, in.ResetAt); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reset bootstrap credential: %w", err)
	}
	committed = true
	return nil
}

func selectBootstrapCredentialSubject(
	ctx context.Context,
	db ExecQueryer,
	tenantID, workspaceID string,
) (string, error) {
	rows, err := db.QueryContext(ctx, selectBootstrapCredentialSubjectQuery, tenantID, workspaceID)
	if err != nil {
		return "", fmt.Errorf("select bootstrap credential subject: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", fmt.Errorf("select bootstrap credential subject: %w", err)
		}
		return "", ErrBootstrapCredentialNotFound
	}
	var subjectIDHash string
	if err := rows.Scan(&subjectIDHash); err != nil {
		return "", fmt.Errorf("select bootstrap credential subject: %w", err)
	}
	return subjectIDHash, rows.Err()
}

func selectBootstrapCredentialOwnerUserID(
	ctx context.Context,
	db ExecQueryer,
	subjectIDHash string,
) (string, error) {
	rows, err := db.QueryContext(ctx, selectBootstrapCredentialOwnerUserIDQuery, subjectIDHash)
	if err != nil {
		return "", fmt.Errorf("select bootstrap credential owner: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", fmt.Errorf("select bootstrap credential owner: %w", err)
		}
		return "", errors.New("select bootstrap credential owner: no active user for subject")
	}
	var userID string
	if err := rows.Scan(&userID); err != nil {
		return "", fmt.Errorf("select bootstrap credential owner: %w", err)
	}
	return userID, rows.Err()
}

// normalizeBootstrapCredentialSeal, validateBootstrapCredentialSeal,
// normalizeResetBootstrapCredentialInput, and validateResetBootstrapCredentialInput
// live in identity_bootstrap_credential_validate.go (split out to keep this
// file under the 500-line cap).
