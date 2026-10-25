// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8824 disposable PostgreSQL proof that NotCentralVersionDeclarationSQL and IsCentralVersionDeclaration classify the same payload table alike, so the readiness count and the consumption sidecar drop exactly the same central version pins; read-only, evaluates the predicate over literal jsonb values; reads ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestNotCentralVersionDeclarationSQLAgreesWithGoLive evaluates the SQL
// predicate over every case of the shared table and checks it against both
// the table and IsCentralVersionDeclaration. The SQL is spliced into the
// readiness package.consumption count; the Go predicate gates the consumption
// sidecar. A drift between them would count a pin on one readiness arm and
// not the other.
func TestNotCentralVersionDeclarationSQLAgreesWithGoLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN"))
	if dsn != "" && os.Getenv("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE") != "1" {
		t.Fatal("ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN is set without ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE=1")
	}
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	}
	if dsn == "" {
		t.Skip("set ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN or ESHU_POSTGRES_DSN to run the central pin predicate agreement proof")
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open(): %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	query := `SELECT ` + NotCentralVersionDeclarationSQL + ` FROM (SELECT $1::jsonb AS payload) AS fact`
	for _, tc := range centralVersionDeclarationCases() {
		encoded, err := json.Marshal(tc.payload)
		if err != nil {
			t.Fatalf("%s: json.Marshal(): %v", tc.name, err)
		}
		var keep sql.NullBool
		if err := database.QueryRowContext(ctx, query, string(encoded)).Scan(&keep); err != nil {
			t.Fatalf("%s: evaluate NotCentralVersionDeclarationSQL: %v", tc.name, err)
		}
		if !keep.Valid {
			t.Errorf("%s: NotCentralVersionDeclarationSQL = NULL, want a boolean (a NULL drops the row)", tc.name)
			continue
		}
		if keep.Bool == tc.pin {
			t.Errorf("%s: SQL keeps row = %v, want %v (pin=%v)", tc.name, keep.Bool, !tc.pin, tc.pin)
		}
		if goPin := IsCentralVersionDeclaration(tc.payload); goPin == keep.Bool {
			t.Errorf("%s: Go pin=%v disagrees with SQL keep=%v", tc.name, goPin, keep.Bool)
		}
	}
}
