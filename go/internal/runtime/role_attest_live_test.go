// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

// De-enrolled from the live-postgres-readiness plan under the drift-gate
// option A (PR #9544, steer 6530394665, second round): the session-against-
// rotation refinement is suspended while physical-connection attestation
// stays proven by the enrolled seven-role proof and the enrolled hook-
// ordering proof, and the code below stays to re-enroll in the post-refit
// follow-up.

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestPostgresRoleContractAttestsActualPhysicalSession(t *testing.T) {
	dsn := os.Getenv("ESHU_ROLE_ATTEST_DSN")
	if dsn == "" || os.Getenv("ESHU_ROLE_ATTEST_DISPOSABLE") != "1" {
		t.Skip("requires an explicitly marked disposable PostgreSQL fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgconn.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	// The shared fixture database may already carry the authority layout
	// (another proof bootstraps it first), so stage the stub
	// idempotently and clean up only the stub: dropping the whole schema
	// could remove tables a concurrent proof still needs.
	const schema = `CREATE SCHEMA IF NOT EXISTS eshu_security;
DROP FUNCTION IF EXISTS eshu_security.attest_connection_v1(text,bigint,text,text);
CREATE FUNCTION eshu_security.attest_connection_v1(text,bigint,text,text)
RETURNS TABLE(policy_revision bigint, profile text, session_role_oid oid, effective_role_oid oid,
 database_oid oid, database_identity uuid, acl_inventory_version integer)
LANGUAGE sql SECURITY INVOKER AS $$
 SELECT $2, 'runtime-writer',
 (SELECT oid FROM pg_roles WHERE rolname=session_user),
 (SELECT oid FROM pg_roles WHERE rolname=current_user),
 (SELECT oid FROM pg_database WHERE datname=current_database()),
 '91d1dfab-4726-449a-948e-d459ee2fd0eb'::uuid, 1
 WHERE $1='runtime-writer-7' AND $2=7 AND $3='runtime' AND $4='writer'
$$;`
	if _, err := conn.Exec(ctx, schema).ReadAll(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `DROP FUNCTION IF EXISTS eshu_security.attest_connection_v1(text,bigint,text,text)`).ReadAll()
	}()
	identity := conn.ExecParams(ctx, `SELECT current_user,current_database()`, nil, nil, nil, nil).Read()
	if identity.Err != nil || len(identity.Rows) != 1 || len(identity.Rows[0]) != 2 {
		t.Fatalf("read disposable connection identity: %v", identity.Err)
	}
	role, database := string(identity.Rows[0][0]), string(identity.Rows[0][1])
	contract, err := LoadPostgresRoleContract(func(key string) string {
		if key == "ESHU_POSTGRES_ROLE_CONTRACT_JSON" {
			return fmt.Sprintf(`{"policy_revision":7,"database_identity":"91d1dfab-4726-449a-948e-d459ee2fd0eb","database":%q,"routes":{"writer":{"contract_id":"runtime-writer-7","session_role":%q,"effective_role":%q}}}`, database, role, role)
		}
		return ""
	}, PostgresWorkloadRuntime, PostgresRouteWriter)
	if err != nil {
		t.Fatal(err)
	}
	if err := contract.Attest(ctx, conn); err != nil {
		t.Fatalf("expected session denied: %v", err)
	}
	contract.Route.SessionRole = "other_role"
	if err := contract.Attest(ctx, conn); err == nil {
		t.Fatal("wrong authenticated identity accepted")
	}
	contract.Route.SessionRole = role
	contract.PolicyRevision = 8
	if err := contract.Attest(ctx, conn); err == nil {
		t.Fatal("stale revision accepted")
	}
}
