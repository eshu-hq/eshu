// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// fakeSetupAdapterDB is a minimal in-memory pgstorage.ExecQueryer routing on
// query substrings, tailored to postgresSetupAdapter's read/write shapes
// (string-valued columns, unlike fakeSeedDB's int-only fakeSeedRows).
type fakeSetupAdapterDB struct {
	credentialRow    []any // sealed_credential, key_id — nil means "not found"
	subjectHashRow   []any // subject_id_hash
	ownerRow         []any // user_id
	consumedStateRow []any // consumed_at IS NOT NULL (bool) — nil means "no matching row" (fails closed to consumed=true)
	execs            []string
	execArgs         [][]any
}

func (f *fakeSetupAdapterDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	f.execs = append(f.execs, query)
	f.execArgs = append(f.execArgs, args)
	return fakeSetupResult{}, nil
}

// Begin satisfies pgstorage.Beginner so CompleteSetupMFA's transaction-scoped
// advisory-lock critical section can run against this fake: the transaction
// just delegates Exec/Query to the same underlying fake so query routing and
// exec-call recording stay in one place.
func (f *fakeSetupAdapterDB) Begin(context.Context) (pgstorage.Transaction, error) {
	return &fakeSetupAdapterTx{db: f}, nil
}

type fakeSetupAdapterTx struct {
	db *fakeSetupAdapterDB
}

func (tx *fakeSetupAdapterTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.db.ExecContext(ctx, query, args...)
}

func (tx *fakeSetupAdapterTx) QueryContext(ctx context.Context, query string, args ...any) (pgstorage.Rows, error) {
	return tx.db.QueryContext(ctx, query, args...)
}

func (tx *fakeSetupAdapterTx) Commit() error   { return nil }
func (tx *fakeSetupAdapterTx) Rollback() error { return nil }

func (f *fakeSetupAdapterDB) QueryContext(_ context.Context, query string, _ ...any) (pgstorage.Rows, error) {
	switch {
	case strings.Contains(query, "SELECT consumed_at IS NOT NULL"):
		if f.consumedStateRow == nil {
			return &fakeSetupRows{}, nil
		}
		return &fakeSetupRows{rows: [][]any{f.consumedStateRow}}, nil
	case strings.Contains(query, "FROM identity_bootstrap_credentials") && strings.Contains(query, "sealed_credential, key_id"):
		if f.credentialRow == nil {
			return &fakeSetupRows{}, nil
		}
		return &fakeSetupRows{rows: [][]any{f.credentialRow}}, nil
	case strings.Contains(query, "SELECT subject_id_hash"):
		if f.subjectHashRow == nil {
			return &fakeSetupRows{}, nil
		}
		return &fakeSetupRows{rows: [][]any{f.subjectHashRow}}, nil
	case strings.Contains(query, "SELECT user_id"):
		if f.ownerRow == nil {
			return &fakeSetupRows{}, nil
		}
		return &fakeSetupRows{rows: [][]any{f.ownerRow}}, nil
	default:
		return &fakeSetupRows{}, nil
	}
}

type fakeSetupResult struct{}

func (fakeSetupResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeSetupResult) RowsAffected() (int64, error) { return 1, nil }

type fakeSetupRows struct {
	rows  [][]any
	index int
}

func (r *fakeSetupRows) Next() bool { return r.index < len(r.rows) }

func (r *fakeSetupRows) Scan(dest ...any) error {
	row := r.rows[r.index]
	for i := range dest {
		switch target := dest[i].(type) {
		case *string:
			*target = row[i].(string)
		case *bool:
			*target = row[i].(bool)
		}
	}
	r.index++
	return nil
}

func (r *fakeSetupRows) Err() error   { return nil }
func (r *fakeSetupRows) Close() error { return nil }

func testSetupKeyring(t *testing.T) *secretcrypto.Keyring {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	keyring, err := secretcrypto.NewKeyring("key1", map[secretcrypto.KeyID][]byte{"key1": key})
	if err != nil {
		t.Fatalf("NewKeyring() error = %v", err)
	}
	return keyring
}

func TestSetupAdapterSetupNeededReflectsCredentialRow(t *testing.T) {
	t.Parallel()

	adapter := &postgresSetupAdapter{
		store: pgstorage.NewIdentitySubjectStore(&fakeSetupAdapterDB{
			credentialRow: []any{"ESK1.key1.nonce.ciphertext", "key1"},
		}),
	}
	needed, err := adapter.SetupNeeded(context.Background())
	if err != nil {
		t.Fatalf("SetupNeeded() error = %v", err)
	}
	if !needed {
		t.Fatal("SetupNeeded() = false, want true when an unconsumed credential row exists")
	}

	closedAdapter := &postgresSetupAdapter{
		store: pgstorage.NewIdentitySubjectStore(&fakeSetupAdapterDB{}),
	}
	needed, err = closedAdapter.SetupNeeded(context.Background())
	if err != nil {
		t.Fatalf("SetupNeeded() error = %v", err)
	}
	if needed {
		t.Fatal("SetupNeeded() = true, want false when no credential row is retrievable")
	}
}

func TestSetupAdapterVerifyBootstrapCredentialMatchesSealedPlaintext(t *testing.T) {
	t.Parallel()

	keyring := testSetupKeyring(t)
	aad := pgstorage.BootstrapCredentialAAD(pgstorage.BootstrapAdminTenantID, pgstorage.BootstrapAdminWorkspaceID)
	payload, err := json.Marshal(bootstrapCredentialPayload{
		Username:     "admin",
		Password:     "generated-secret",
		RecoveryCode: "recovery-abc",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	sealed, err := keyring.Seal(payload, aad)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}

	adapter := &postgresSetupAdapter{
		store: pgstorage.NewIdentitySubjectStore(&fakeSetupAdapterDB{
			credentialRow: []any{sealed, "key1"},
		}),
		keyring: keyring,
	}

	ok, err := adapter.VerifyBootstrapCredential(context.Background(), "admin", "generated-secret")
	if err != nil {
		t.Fatalf("VerifyBootstrapCredential() error = %v", err)
	}
	if !ok {
		t.Fatal("VerifyBootstrapCredential() = false, want true for the matching sealed plaintext")
	}

	ok, err = adapter.VerifyBootstrapCredential(context.Background(), "admin", "wrong-password")
	if err != nil {
		t.Fatalf("VerifyBootstrapCredential(wrong password) error = %v", err)
	}
	if ok {
		t.Fatal("VerifyBootstrapCredential() = true, want false for a wrong password")
	}
}

func TestSetupAdapterVerifyBootstrapCredentialFailsClosedWithoutKeyring(t *testing.T) {
	t.Parallel()

	adapter := &postgresSetupAdapter{
		store: pgstorage.NewIdentitySubjectStore(&fakeSetupAdapterDB{
			credentialRow: []any{"ESK1.key1.nonce.ciphertext", "key1"},
		}),
		keyring: nil,
	}
	ok, err := adapter.VerifyBootstrapCredential(context.Background(), "admin", "anything")
	if err == nil {
		t.Fatal("expected an error when the decryption keyring is not configured")
	}
	if ok {
		t.Fatal("VerifyBootstrapCredential() = true, want false when unconfigured")
	}
}

func TestSetupAdapterResolveSetupOwner(t *testing.T) {
	t.Parallel()

	db := &fakeSetupAdapterDB{
		subjectHashRow: []any{"sha256:owner-subject"},
		ownerRow:       []any{"user-1"},
	}
	adapter := &postgresSetupAdapter{store: pgstorage.NewIdentitySubjectStore(db)}

	owner, err := adapter.ResolveSetupOwner(context.Background())
	if err != nil {
		t.Fatalf("ResolveSetupOwner() error = %v", err)
	}
	if owner.UserID != "user-1" || owner.SubjectIDHash != "sha256:owner-subject" {
		t.Fatalf("owner = %#v, want resolved user/subject", owner)
	}
	if owner.TenantID != pgstorage.BootstrapAdminTenantID || owner.WorkspaceID != pgstorage.BootstrapAdminWorkspaceID {
		t.Fatalf("owner tenant/workspace = %q/%q, want the fixed bootstrap slot", owner.TenantID, owner.WorkspaceID)
	}
}

// TestSetupAdapterCompleteSetupMFADelegatesToAtomicStoreMethod proves the
// adapter forwards to the postgres store's atomic CompleteSetupMFA
// (identity_setup_completion.go, #4990) rather than the old two-call
// RotateSetupMFA/CompleteSetup split, and threads the completed bool back
// to the caller unchanged.
func TestSetupAdapterCompleteSetupMFADelegatesToAtomicStoreMethod(t *testing.T) {
	t.Parallel()

	db := &fakeSetupAdapterDB{
		// selectBootstrapCredentialConsumedState (the first QueryContext
		// inside CompleteSetupMFA's transaction, run under the advisory
		// lock): not yet consumed.
		consumedStateRow: []any{false},
	}
	adapter := &postgresSetupAdapter{store: pgstorage.NewIdentitySubjectStore(db)}

	completed, err := adapter.CompleteSetupMFA(context.Background(), query.CompleteSetupMFAInput{
		TenantID:           pgstorage.BootstrapAdminTenantID,
		WorkspaceID:        pgstorage.BootstrapAdminWorkspaceID,
		SubjectIDHash:      "sha256:owner-subject",
		UserID:             "user-1",
		MFAFactorID:        "mfa-factor-1",
		MFAFactorKind:      "recovery_code",
		RecoveryCodeHashes: []string{"sha256:code-a"},
		Now:                time.Now(),
	})
	if err != nil {
		t.Fatalf("CompleteSetupMFA() error = %v", err)
	}
	if !completed {
		t.Fatal("CompleteSetupMFA() completed = false, want true")
	}
	foundLock, foundConsume := false, false
	for _, exec := range db.execs {
		if strings.Contains(exec, "pg_advisory_xact_lock(3456)") {
			foundLock = true
		}
		if strings.Contains(exec, "UPDATE identity_bootstrap_credentials") {
			foundConsume = true
		}
	}
	if !foundLock || !foundConsume {
		t.Fatalf("CompleteSetupMFA did not run the expected lock+consume statements: execs = %#v", db.execs)
	}
}
