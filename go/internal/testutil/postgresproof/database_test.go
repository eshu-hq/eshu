// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import (
	"strings"
	"testing"
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
