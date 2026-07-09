// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// fakeSeedDB is a minimal in-memory pgstorage.ExecQueryer + Beginner used
// only to drive seedInitialAdmin's control flow (mode selection, error
// propagation, banner/telemetry outcome) without a real Postgres connection.
// countRows controls what the very first QueryContext call inside
// BootstrapLocalIdentity's transaction (countExistingLocalIdentityUsers)
// returns; every other query/exec succeeds trivially.
type fakeSeedDB struct {
	mu        sync.Mutex
	countRows int64
	execArgs  [][]any
	execs     []string
	queries   []string
	// credentialConflict simulates the ON CONFLICT (tenant_id, workspace_id)
	// DO NOTHING branch of the credential insert losing the race: RETURNING 1
	// produces zero rows even though the identity insert in the same
	// transaction succeeded. Exercises the extremely-unlikely-but-possible
	// path where insertBootstrapCredentialInTx reports inserted=false.
	credentialConflict bool
}

func (f *fakeSeedDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execs = append(f.execs, query)
	f.execArgs = append(f.execArgs, args)
	return fakeSeedResult{}, nil
}

func (f *fakeSeedDB) QueryContext(_ context.Context, query string, _ ...any) (pgstorage.Rows, error) {
	f.mu.Lock()
	f.queries = append(f.queries, query)
	f.mu.Unlock()
	if strings.Contains(query, "COUNT(DISTINCT") {
		return &fakeSeedRows{rows: [][]any{{f.countRows}}}, nil
	}
	if strings.Contains(query, "ON CONFLICT (tenant_id, workspace_id) DO NOTHING") {
		if f.credentialConflict {
			// Lost the race: RETURNING 1 produces no rows.
			return &fakeSeedRows{}, nil
		}
		// A true insert: one row from RETURNING 1.
		return &fakeSeedRows{rows: [][]any{{1}}}, nil
	}
	return &fakeSeedRows{}, nil
}

func (f *fakeSeedDB) Begin(context.Context) (pgstorage.Transaction, error) {
	return &fakeSeedTx{db: f}, nil
}

type fakeSeedTx struct {
	db *fakeSeedDB
}

func (tx *fakeSeedTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.db.ExecContext(ctx, query, args...)
}

func (tx *fakeSeedTx) QueryContext(ctx context.Context, query string, args ...any) (pgstorage.Rows, error) {
	return tx.db.QueryContext(ctx, query, args...)
}

func (tx *fakeSeedTx) Commit() error   { return nil }
func (tx *fakeSeedTx) Rollback() error { return nil }

type fakeSeedResult struct{}

func (fakeSeedResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeSeedResult) RowsAffected() (int64, error) { return 1, nil }

type fakeSeedRows struct {
	rows  [][]any
	index int
}

func (r *fakeSeedRows) Next() bool { return r.index < len(r.rows) }

func (r *fakeSeedRows) Scan(dest ...any) error {
	row := r.rows[r.index]
	for i := range dest {
		switch target := dest[i].(type) {
		case *int64:
			*target = row[i].(int64)
		case *int:
			*target = row[i].(int)
		}
	}
	r.index++
	return nil
}

func (r *fakeSeedRows) Err() error   { return nil }
func (r *fakeSeedRows) Close() error { return nil }

// fakeAuditAppender captures every governance-audit event appended during a
// test so assertions can check reason codes, decisions, and (for the
// negative-leakage proofs) that no event field ever carries plaintext.
type fakeAuditAppender struct {
	mu     sync.Mutex
	events []governanceaudit.Event
}

func (f *fakeAuditAppender) Append(_ context.Context, events []governanceaudit.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, events...)
	return nil
}

func auditEventsContain(events []governanceaudit.Event, reasonCode string) bool {
	for _, e := range events {
		if e.ReasonCode == reasonCode {
			return true
		}
	}
	return false
}

// auditEventsLeak reports whether any event's formatted representation
// contains any of the given plaintext values.
func auditEventsLeak(events []governanceaudit.Event, plaintexts ...string) bool {
	for _, e := range events {
		rendered := fmt.Sprintf("%+v", e)
		for _, p := range plaintexts {
			if p != "" && strings.Contains(rendered, p) {
				return true
			}
		}
	}
	return false
}

func withBootstrapBannerCapture(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	original := bootstrapBannerWriter
	bootstrapBannerWriter = &buf
	t.Cleanup(func() { bootstrapBannerWriter = original })
	return &buf
}

func testGetenv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestSeedInitialAdminSkipsWhenModeDisabled(t *testing.T) {
	banner := withBootstrapBannerCapture(t)

	db := &fakeSeedDB{}
	audit := &fakeAuditAppender{}
	err := seedInitialAdmin(context.Background(), db, testGetenv(map[string]string{
		"ESHU_AUTH_BOOTSTRAP_MODE": "disabled",
	}), nil, nil, audit)
	if err != nil {
		t.Fatalf("seedInitialAdmin() error = %v, want nil for disabled mode", err)
	}
	if banner.Len() != 0 {
		t.Fatalf("banner written for disabled mode: %q", banner.String())
	}
	if len(db.execs) != 0 {
		t.Fatalf("disabled mode issued %d DB execs, want 0", len(db.execs))
	}
	if !auditEventsContain(audit.events, bootstrapAuditReasonModeDisabled) {
		t.Fatalf("no durable audit event for disabled mode choice: %#v", audit.events)
	}
}

func TestSeedInitialAdminSkipsWhenModeSSOOnly(t *testing.T) {
	db := &fakeSeedDB{}
	audit := &fakeAuditAppender{}
	err := seedInitialAdmin(context.Background(), db, testGetenv(map[string]string{
		"ESHU_AUTH_BOOTSTRAP_MODE": "sso-only",
	}), nil, nil, audit)
	if err != nil {
		t.Fatalf("seedInitialAdmin() error = %v, want nil for sso-only mode", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("sso-only mode issued %d DB execs, want 0", len(db.execs))
	}
	if !auditEventsContain(audit.events, bootstrapAuditReasonModeSSOOnly) {
		t.Fatalf("no durable audit event for sso-only mode choice: %#v", audit.events)
	}
}

func TestSeedInitialAdminRejectsInvalidMode(t *testing.T) {
	db := &fakeSeedDB{}
	err := seedInitialAdmin(context.Background(), db, testGetenv(map[string]string{
		"ESHU_AUTH_BOOTSTRAP_MODE": "bogus",
	}), nil, nil, nil)
	if err == nil {
		t.Fatal("seedInitialAdmin() error = nil, want error for invalid mode")
	}
}

func TestSeedInitialAdminEnvSeedPrintsOnlyRecoveryCodeNotPassword(t *testing.T) {
	banner := withBootstrapBannerCapture(t)

	db := &fakeSeedDB{countRows: 0}
	audit := &fakeAuditAppender{}
	err := seedInitialAdmin(context.Background(), db, testGetenv(map[string]string{
		"ESHU_ADMIN_USERNAME": "operator",
		"ESHU_ADMIN_PASSWORD": "correct-horse-battery-staple",
	}), nil, nil, audit)
	if err != nil {
		t.Fatalf("seedInitialAdmin() error = %v", err)
	}
	out := banner.String()
	if !strings.Contains(out, "operator") {
		t.Fatalf("banner missing username: %q", out)
	}
	if strings.Contains(out, "correct-horse-battery-staple") {
		t.Fatalf("banner leaked the operator-supplied password: %q", out)
	}
	if !strings.Contains(out, "recovery code:") {
		t.Fatalf("banner missing recovery code line: %q", out)
	}
	if !auditEventsContain(audit.events, bootstrapAuditReasonModeSeededEnv) {
		t.Fatalf("no durable audit event for env-seed mode choice: %#v", audit.events)
	}
	if auditEventsLeak(audit.events, "correct-horse-battery-staple") {
		t.Fatalf("audit event leaked the operator-supplied password: %#v", audit.events)
	}
}

func TestSeedInitialAdminGeneratedRequiresDEK(t *testing.T) {
	db := &fakeSeedDB{countRows: 0}
	err := seedInitialAdmin(context.Background(), db, testGetenv(map[string]string{
		"ESHU_AUTH_BOOTSTRAP_MODE": "generated",
	}), nil, nil, nil)
	if err == nil {
		t.Fatal("seedInitialAdmin() error = nil, want error when no DEK is configured")
	}
}

// composeDevDEK is the fixed, publicly-known, all-zero dev-only
// ESHU_AUTH_SECRET_ENC_KEY placeholder shipped in docker-compose.yaml,
// docker-compose.neo4j.yml, .github/workflows/e2e-tests.yml, and
// scripts/verify-golden-corpus-gate.sh so a fresh local/CI stack can boot
// ESHU_AUTH_BOOTSTRAP_MODE=generated (the default) without extra setup. It
// is duplicated across those four non-Go files (YAML/shell have no shared
// import mechanism with Go); this constant exists only so this test can
// prove that literal decodes to a usable 32-byte key end to end. If any of
// the four copies drifts from this value, update all five together.
const composeDevDEK = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

// TestSeedInitialAdminGeneratedSucceedsWithComposeDevDEK proves the exact DEK
// literal shipped in the local/CI Compose stacks and the golden-corpus-gate
// script is a valid 32-byte key that lets ESHU_AUTH_BOOTSTRAP_MODE=generated
// (the default) succeed end to end, rather than fail closed at API startup
// (PR #4979 CI failure: nornicdb/neo4j e2e lanes and the golden-corpus gate
// all boot a fresh API with no DEK configured).
func TestSeedInitialAdminGeneratedSucceedsWithComposeDevDEK(t *testing.T) {
	banner := withBootstrapBannerCapture(t)

	db := &fakeSeedDB{countRows: 0}
	err := seedInitialAdmin(context.Background(), db, testGetenv(map[string]string{
		"ESHU_AUTH_BOOTSTRAP_MODE": "generated",
		"ESHU_AUTH_SECRET_ENC_KEY": composeDevDEK,
	}), nil, nil, nil)
	if err != nil {
		t.Fatalf("seedInitialAdmin() error = %v, want the compose dev DEK to boot generated mode successfully", err)
	}
	if banner.Len() == 0 {
		t.Fatal("banner not written; generated mode did not run with the compose dev DEK")
	}
}

func TestSeedInitialAdminGeneratedSealsAndPrintsFullBundle(t *testing.T) {
	banner := withBootstrapBannerCapture(t)

	dek := "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=" // base64 of 32 raw bytes
	db := &fakeSeedDB{countRows: 0}
	audit := &fakeAuditAppender{}
	err := seedInitialAdmin(context.Background(), db, testGetenv(map[string]string{
		"ESHU_AUTH_BOOTSTRAP_MODE": "generated",
		"ESHU_AUTH_SECRET_ENC_KEY": dek,
		"ESHU_ADMIN_USERNAME":      "owner",
	}), nil, nil, audit)
	if err != nil {
		t.Fatalf("seedInitialAdmin() error = %v", err)
	}
	out := banner.String()
	if !strings.Contains(out, "owner") || !strings.Contains(out, "password:") || !strings.Contains(out, "recovery code:") {
		t.Fatalf("banner missing expected fields: %q", out)
	}
	if !fakeSeedExecsContain(db.queries, "identity_bootstrap_credentials") {
		t.Fatalf("generated mode did not persist a sealed bootstrap credential row: %#v", db.queries)
	}
	if !auditEventsContain(audit.events, bootstrapAuditReasonModeGenerated) {
		t.Fatalf("no durable audit event for generated mode choice: %#v", audit.events)
	}
	if !auditEventsContain(audit.events, bootstrapAuditReasonGenerated) {
		t.Fatalf("no durable audit event for credential generation: %#v", audit.events)
	}
}

// TestSeedInitialAdminGeneratedCredentialConflictPrintsNoBanner proves that
// when the credential insert loses the ON CONFLICT DO NOTHING race (the
// identity row was freshly created in the same transaction, but a
// credential envelope already existed for the fixed bootstrap-admin tenant/
// workspace slot), the freshly generated password/recovery code — which
// were never persisted into identity_bootstrap_credentials — are never
// printed. Printing them would hand the operator a credential that cannot
// authenticate (PR #4979 review).
func TestSeedInitialAdminGeneratedCredentialConflictPrintsNoBanner(t *testing.T) {
	banner := withBootstrapBannerCapture(t)

	dek := "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	db := &fakeSeedDB{countRows: 0, credentialConflict: true}
	audit := &fakeAuditAppender{}
	err := seedInitialAdmin(context.Background(), db, testGetenv(map[string]string{
		"ESHU_AUTH_BOOTSTRAP_MODE": "generated",
		"ESHU_AUTH_SECRET_ENC_KEY": dek,
		"ESHU_ADMIN_USERNAME":      "owner",
	}), nil, nil, audit)
	if err != nil {
		t.Fatalf("seedInitialAdmin() error = %v", err)
	}
	if banner.Len() != 0 {
		t.Fatalf("banner printed a credential that was never persisted: %q", banner.String())
	}
	if !auditEventsContain(audit.events, bootstrapAuditReasonModeGenerated) {
		t.Fatalf("no durable audit event for generated mode choice: %#v", audit.events)
	}
	if !auditEventsContain(audit.events, bootstrapAuditReasonGenerated) {
		t.Fatalf("no durable audit event for credential generation attempt: %#v", audit.events)
	}
}

func TestSeedInitialAdminAlreadyProvisionedSkipsGenerateAndBanner(t *testing.T) {
	banner := withBootstrapBannerCapture(t)

	dek := "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	db := &fakeSeedDB{countRows: 1} // an identity already exists
	audit := &fakeAuditAppender{}
	err := seedInitialAdmin(context.Background(), db, testGetenv(map[string]string{
		"ESHU_AUTH_BOOTSTRAP_MODE": "generated",
		"ESHU_AUTH_SECRET_ENC_KEY": dek,
	}), nil, nil, audit)
	if err != nil {
		t.Fatalf("seedInitialAdmin() error = %v, want nil (sealed/no-op)", err)
	}
	if banner.Len() != 0 {
		t.Fatalf("banner printed when an identity already existed: %q", banner.String())
	}
	if fakeSeedExecsContain(db.queries, "identity_bootstrap_credentials") {
		t.Fatal("GenerateBootstrapCredential ran even though identities already existed")
	}
	if !auditEventsContain(audit.events, bootstrapAuditReasonModeSealed) {
		t.Fatalf("no durable audit event for sealed_existing mode choice: %#v", audit.events)
	}
	if auditEventsContain(audit.events, bootstrapAuditReasonGenerated) {
		t.Fatal("a credential-generation audit event fired even though no generation happened")
	}
}

// TestSeedInitialAdminGeneratedSkipsCryptoWhenAlreadyProvisioned proves the
// early HasBootstrappedLocalIdentity check runs before any crypto work: a
// restart with countRows=1 and NO DEK configured still succeeds (does not
// fail closed on a missing DEK it no longer needs) and never resolves a
// keyring or seals anything.
func TestSeedInitialAdminGeneratedSkipsCryptoWhenAlreadyProvisioned(t *testing.T) {
	db := &fakeSeedDB{countRows: 1}
	err := seedInitialAdmin(context.Background(), db, testGetenv(map[string]string{
		"ESHU_AUTH_BOOTSTRAP_MODE": "generated",
		// Deliberately no ESHU_AUTH_SECRET_ENC_KEY: the early exists-check
		// must short-circuit before the DEK is ever required.
	}), nil, nil, nil)
	if err != nil {
		t.Fatalf("seedInitialAdmin() error = %v, want nil (no DEK required once already provisioned)", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("no writes expected once already provisioned: %#v", db.execs)
	}
}

// TestSeedInitialAdminNegativeLeakage proves the generated plaintext
// password/recovery code never reach structured logging or durable audit
// events: they must appear only in the one-time banner writer.
