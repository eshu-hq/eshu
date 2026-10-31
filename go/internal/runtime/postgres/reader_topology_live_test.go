// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// De-enrolled from the live-postgres-readiness plan under the drift-gate
// option A (PR #9544, steer 6530394665, second round): the reader
// distinct-credential topology proof is suspended while the reader role
// itself stays attested by the enrolled seven-role proof, and the code
// below stays to re-enroll in the post-refit follow-up.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestDistinctReaderCredentialOnSamePhysicalPrimary(t *testing.T) {
	dsn := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	if dsn == "" || os.Getenv("ESHU_ROLE_ATTEST_DISPOSABLE") != "1" {
		t.Skip("requires an explicitly marked disposable PostgreSQL fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close(ctx) }()
	if _, err := admin.Exec(ctx, `CREATE ROLE eshu_reader_topology_probe LOGIN PASSWORD 'topology-fixture-password'`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.Exec(context.Background(), `REVOKE EXECUTE ON FUNCTION pg_catalog.pg_control_system() FROM eshu_reader_topology_probe`)
		_, _ = admin.Exec(context.Background(), `DROP ROLE eshu_reader_topology_probe`)
	}()
	if _, err := admin.Exec(ctx, `GRANT EXECUTE ON FUNCTION pg_catalog.pg_control_system() TO eshu_reader_topology_probe`); err != nil {
		t.Fatal(err)
	}
	reader, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	reader.User = "eshu_reader_topology_probe"
	reader.Password = "topology-fixture-password"
	readerDSN := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable", reader.Host, reader.Port, reader.User, reader.Password, reader.Database)
	env := map[string]string{
		"ESHU_POSTGRES_DSN":           dsn,
		"ESHU_POSTGRES_READ_DSN":      readerDSN,
		"ESHU_POSTGRES_READ_TOPOLOGY": "primary",
	}
	cfg, err := LoadConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	access, err := Open(ctx, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = access.Close() }()
	var sessionRole string
	var inRecovery bool
	if err := access.reader.QueryRowContext(ctx, `SELECT session_user,pg_is_in_recovery()`).Scan(&sessionRole, &inRecovery); err != nil {
		t.Fatal(err)
	}
	if sessionRole != "eshu_reader_topology_probe" || inRecovery || !access.samePrimary {
		t.Fatalf("reader session=%s recovery=%t same primary=%t", sessionRole, inRecovery, access.samePrimary)
	}
	roleCfg := cfg
	roleCfg.roleBoundaryConfigured = true
	roleCfg.WriterRoleAttestor = func(context.Context, *pgconn.PgConn) error { return nil }
	roleCfg.ReaderRoleAttestor = func(context.Context, *pgconn.PgConn) error { return nil }
	attested, err := Open(ctx, roleCfg, nil)
	if err != nil {
		t.Fatalf("role-attested primary reader baseline: %v", err)
	}
	_ = attested.Close()
	roleCfg.ReadDSN = readerDSN + " target_session_attrs=standby"
	denied, err := Open(ctx, roleCfg, nil)
	if err == nil {
		_ = denied.Close()
		t.Fatal("role attestor bypassed reader DSN target_session_attrs=standby on a primary")
	}
}
