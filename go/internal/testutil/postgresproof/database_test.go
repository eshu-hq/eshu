// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
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
