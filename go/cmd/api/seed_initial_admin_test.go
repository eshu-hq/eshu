// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"sync"
	"testing"

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
	err := seedInitialAdmin(context.Background(), db, testGetenv(map[string]string{
		"ESHU_AUTH_BOOTSTRAP_MODE": "disabled",
	}), nil, nil)
	if err != nil {
		t.Fatalf("seedInitialAdmin() error = %v, want nil for disabled mode", err)
	}
	if banner.Len() != 0 {
		t.Fatalf("banner written for disabled mode: %q", banner.String())
	}
	if len(db.execs) != 0 {
		t.Fatalf("disabled mode issued %d DB execs, want 0", len(db.execs))
	}
}

func TestSeedInitialAdminSkipsWhenModeSSOOnly(t *testing.T) {
	db := &fakeSeedDB{}
	err := seedInitialAdmin(context.Background(), db, testGetenv(map[string]string{
		"ESHU_AUTH_BOOTSTRAP_MODE": "sso-only",
	}), nil, nil)
	if err != nil {
		t.Fatalf("seedInitialAdmin() error = %v, want nil for sso-only mode", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("sso-only mode issued %d DB execs, want 0", len(db.execs))
	}
}

func TestSeedInitialAdminRejectsInvalidMode(t *testing.T) {
	db := &fakeSeedDB{}
	err := seedInitialAdmin(context.Background(), db, testGetenv(map[string]string{
		"ESHU_AUTH_BOOTSTRAP_MODE": "bogus",
	}), nil, nil)
	if err == nil {
		t.Fatal("seedInitialAdmin() error = nil, want error for invalid mode")
	}
}

func TestSeedInitialAdminEnvSeedPrintsOnlyRecoveryCodeNotPassword(t *testing.T) {
	banner := withBootstrapBannerCapture(t)

	db := &fakeSeedDB{countRows: 0}
	err := seedInitialAdmin(context.Background(), db, testGetenv(map[string]string{
		"ESHU_ADMIN_USERNAME": "operator",
		"ESHU_ADMIN_PASSWORD": "correct-horse-battery-staple",
	}), nil, nil)
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
}

func TestSeedInitialAdminGeneratedRequiresDEK(t *testing.T) {
	db := &fakeSeedDB{countRows: 0}
	err := seedInitialAdmin(context.Background(), db, testGetenv(map[string]string{
		"ESHU_AUTH_BOOTSTRAP_MODE": "generated",
	}), nil, nil)
	if err == nil {
		t.Fatal("seedInitialAdmin() error = nil, want error when no DEK is configured")
	}
}

func TestSeedInitialAdminGeneratedSealsAndPrintsFullBundle(t *testing.T) {
	banner := withBootstrapBannerCapture(t)

	dek := "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=" // base64 of 32 raw bytes
	db := &fakeSeedDB{countRows: 0}
	err := seedInitialAdmin(context.Background(), db, testGetenv(map[string]string{
		"ESHU_AUTH_BOOTSTRAP_MODE": "generated",
		"ESHU_AUTH_SECRET_ENC_KEY": dek,
		"ESHU_ADMIN_USERNAME":      "owner",
	}), nil, nil)
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
}

func TestSeedInitialAdminAlreadyProvisionedSkipsGenerateAndBanner(t *testing.T) {
	banner := withBootstrapBannerCapture(t)

	dek := "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	db := &fakeSeedDB{countRows: 1} // an identity already exists
	err := seedInitialAdmin(context.Background(), db, testGetenv(map[string]string{
		"ESHU_AUTH_BOOTSTRAP_MODE": "generated",
		"ESHU_AUTH_SECRET_ENC_KEY": dek,
	}), nil, nil)
	if err != nil {
		t.Fatalf("seedInitialAdmin() error = %v, want nil (sealed/no-op)", err)
	}
	if banner.Len() != 0 {
		t.Fatalf("banner printed when an identity already existed: %q", banner.String())
	}
	if fakeSeedExecsContain(db.queries, "identity_bootstrap_credentials") {
		t.Fatal("GenerateBootstrapCredential ran even though identities already existed")
	}
}

// TestSeedInitialAdminNegativeLeakage proves the generated plaintext
// password/recovery code never reach structured logging: they must appear
// only in the one-time banner writer, nowhere in the slog output captured
// for the same seeding run.
func TestSeedInitialAdminNegativeLeakage(t *testing.T) {
	banner := withBootstrapBannerCapture(t)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	dek := "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	db := &fakeSeedDB{countRows: 0}
	err := seedInitialAdmin(context.Background(), db, testGetenv(map[string]string{
		"ESHU_AUTH_BOOTSTRAP_MODE": "generated",
		"ESHU_AUTH_SECRET_ENC_KEY": dek,
	}), nil, logger)
	if err != nil {
		t.Fatalf("seedInitialAdmin() error = %v", err)
	}

	bannerText := banner.String()
	passwordLine := extractBannerValue(t, bannerText, "password:")
	recoveryLine := extractBannerValue(t, bannerText, "recovery code:")

	logText := logBuf.String()
	if strings.Contains(logText, passwordLine) {
		t.Fatalf("structured log leaked the generated password: %q", logText)
	}
	if strings.Contains(logText, recoveryLine) {
		t.Fatalf("structured log leaked the generated recovery code: %q", logText)
	}
	for _, exec := range db.execArgs {
		for _, arg := range exec {
			s, ok := arg.(string)
			if !ok {
				continue
			}
			if s == passwordLine || s == recoveryLine {
				t.Fatalf("a non-credential-table SQL exec carried the plaintext: %v", exec)
			}
		}
	}
}

func extractBannerValue(t *testing.T, banner, label string) string {
	t.Helper()
	idx := strings.Index(banner, label)
	if idx < 0 {
		t.Fatalf("banner missing label %q: %q", label, banner)
	}
	rest := banner[idx+len(label):]
	end := strings.IndexByte(rest, '\n')
	if end < 0 {
		end = len(rest)
	}
	return strings.TrimSpace(rest[:end])
}

func fakeSeedExecsContain(execs []string, substr string) bool {
	for _, e := range execs {
		if strings.Contains(e, substr) {
			return true
		}
	}
	return false
}
