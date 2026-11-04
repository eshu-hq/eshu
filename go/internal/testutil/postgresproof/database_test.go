// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestValidateAdminDSNRejectsUnsafeOrUnapprovedTargets(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		dsn     string
		optIn   string
		wantErr string
	}{
		{
			name:    "retained application database",
			dsn:     "postgres://postgres:secret@127.0.0.1:5432/eshu?sslmode=disable",
			optIn:   "1",
			wantErr: `database must be "postgres"`,
		},
		{
			name:    "missing destructive proof opt in",
			dsn:     "postgres://postgres:secret@127.0.0.1:5432/postgres?sslmode=disable",
			wantErr: "explicit disposable-database opt-in",
		},
		{
			name:    "wrong destructive proof opt in",
			dsn:     "postgres://postgres:secret@127.0.0.1:5432/postgres?sslmode=disable",
			optIn:   "true",
			wantErr: "explicit disposable-database opt-in",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := validateAdminDSN(tc.dsn, tc.optIn)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validateAdminDSN() error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateAdminDSNAcceptsExplicitPostgresAdminTarget(t *testing.T) {
	t.Parallel()

	config, err := validateAdminDSN(
		"postgres://postgres:secret@127.0.0.1:5432/postgres?sslmode=disable",
		"1",
	)
	if err != nil {
		t.Fatalf("validateAdminDSN() error = %v", err)
	}
	if config.Database != "postgres" {
		t.Fatalf("database = %q, want postgres", config.Database)
	}
}

func TestWithDatabaseRetargetsOnlyTheDatabase(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		dsn      string
		database string
		wantDB   string
	}{
		{
			name:     "url form family dsn to postgres admin",
			dsn:      "postgres://eshu:change-me@127.0.0.1:15432/eshu_claim_scheduled?sslmode=disable",
			database: "postgres",
			wantDB:   "postgres",
		},
		{
			name:     "url form admin dsn to generated database",
			dsn:      "postgres://eshu:change-me@127.0.0.1:15432/postgres?sslmode=disable",
			database: "eshu_content_index_proof_deadbeef",
			wantDB:   "eshu_content_index_proof_deadbeef",
		},
		{
			name:     "keyword form keeps credentials",
			dsn:      "host=127.0.0.1 port=15432 user=eshu password=change-me dbname=eshu_claim_scheduled sslmode=disable",
			database: "postgres",
			wantDB:   "postgres",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := WithDatabase(tc.dsn, tc.database)
			if err != nil {
				t.Fatalf("WithDatabase() error = %v", err)
			}
			config, err := pgx.ParseConfig(got)
			if err != nil {
				t.Fatalf("parse derived DSN: %v", err)
			}
			if config.Database != tc.wantDB {
				t.Fatalf("database = %q, want %q", config.Database, tc.wantDB)
			}
			if config.User != "eshu" || config.Host != "127.0.0.1" || config.Port != 15432 {
				t.Fatalf("derived DSN lost connection identity: user=%q host=%q port=%d",
					config.User, config.Host, config.Port)
			}
		})
	}
}

func TestWithDatabaseRejectsUnparseableDSN(t *testing.T) {
	t.Parallel()

	if _, err := WithDatabase("://missing-scheme", "postgres"); err == nil {
		t.Fatal("WithDatabase() = nil error, want a parse failure instead of the wrong database")
	}
}

func TestMissingAdminDSNSkipMessageIsGeneric(t *testing.T) {
	t.Parallel()

	message := missingAdminDSNSkipMessage()
	if strings.Contains(message, "ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN") {
		t.Fatalf("skip message keeps a caller-specific environment variable: %q", message)
	}
	if !strings.Contains(message, "disposable PostgreSQL proof DSN") {
		t.Fatalf("skip message = %q, want generic disposable proof guidance", message)
	}
}

// scriptedDropAdmin scripts DROP DATABASE outcomes for the teardown retry
// proof: it records every DROP and pg_terminate_backend call without touching
// a server, so the 55006 retry matrix runs deterministically.
type scriptedDropAdmin struct {
	dropCalls      int
	terminateCalls int
	dropResults    []error
	terminateErr   error
}

type stubSQLResult struct{}

func (stubSQLResult) LastInsertId() (int64, error) { return 0, nil }
func (stubSQLResult) RowsAffected() (int64, error) { return 0, nil }

func (f *scriptedDropAdmin) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	if strings.HasPrefix(query, "DROP DATABASE") {
		f.dropCalls++
		if len(f.dropResults) > 0 {
			err := f.dropResults[0]
			f.dropResults = f.dropResults[1:]
			return stubSQLResult{}, err
		}
		return stubSQLResult{}, nil
	}
	if strings.Contains(query, "pg_terminate_backend") {
		f.terminateCalls++
		return stubSQLResult{}, f.terminateErr
	}
	return stubSQLResult{}, fmt.Errorf("unexpected admin query: %q", query)
}

func objectInUseError() error {
	return &pgconn.PgError{Code: "55006", Message: `database "eshu_content_index_proof_deadbeef" is being accessed by other users`}
}

// TestTerminateAndDropRetriesObjectInUseThenSucceeds pins the #9985 remedy:
// a DROP that reports SQLSTATE 55006 (FORCE's signal-then-recount losing to
// slow-to-die backends) is retried after terminating the stragglers, instead
// of failing the test in teardown.
func TestTerminateAndDropRetriesObjectInUseThenSucceeds(t *testing.T) {
	t.Parallel()

	admin := &scriptedDropAdmin{dropResults: []error{objectInUseError(), objectInUseError(), nil}}
	if err := terminateAndDropDisposableDatabase(context.Background(), admin, "eshu_content_index_proof_deadbeef"); err != nil {
		t.Fatalf("terminateAndDropDisposableDatabase() = %v, want success after 55006 retries", err)
	}
	if admin.dropCalls != 3 {
		t.Fatalf("DROP calls = %d, want 3 (initial plus two 55006 retries)", admin.dropCalls)
	}
	if admin.terminateCalls != 3 {
		t.Fatalf("terminate calls = %d, want 3 (one before every DROP attempt)", admin.terminateCalls)
	}
}

// TestTerminateAndDropAbortsOnNonObjectInUse pins that only the teardown race
// (55006) retries: any other DROP failure returns immediately.
func TestTerminateAndDropAbortsOnNonObjectInUse(t *testing.T) {
	t.Parallel()

	admin := &scriptedDropAdmin{dropResults: []error{&pgconn.PgError{Code: "42501", Message: "permission denied"}}}
	if err := terminateAndDropDisposableDatabase(context.Background(), admin, "eshu_content_index_proof_deadbeef"); err == nil {
		t.Fatal("terminateAndDropDisposableDatabase() = nil, want the permission error")
	}
	if admin.dropCalls != 1 {
		t.Fatalf("DROP calls = %d, want 1 (no retry on non-55006)", admin.dropCalls)
	}
}

// TestTerminateAndDropGivesUpAfterBoundedAttempts pins the retry bound: a
// database that never drains fails loudly instead of stalling cleanup.
func TestTerminateAndDropGivesUpAfterBoundedAttempts(t *testing.T) {
	t.Parallel()

	results := make([]error, disposableDropMaxAttempts+2)
	for i := range results {
		results[i] = objectInUseError()
	}
	admin := &scriptedDropAdmin{dropResults: results}
	err := terminateAndDropDisposableDatabase(context.Background(), admin, "eshu_content_index_proof_deadbeef")
	if err == nil || !strings.Contains(err.Error(), "55006") {
		t.Fatalf("terminateAndDropDisposableDatabase() = %v, want the persistent 55006", err)
	}
	if admin.dropCalls != disposableDropMaxAttempts {
		t.Fatalf("DROP calls = %d, want %d (bounded, not endless)", admin.dropCalls, disposableDropMaxAttempts)
	}
}
