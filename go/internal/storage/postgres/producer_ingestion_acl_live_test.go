// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// De-enrolled from the live-postgres-readiness plan under the drift-gate
// option A (PR #9544, steer 6530394665, second round): the producer
// write-scope refinement is suspended while the producer role itself stays
// attested by the enrolled seven-role proof, and the code below stays to
// re-enroll in the post-refit follow-up.

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestProducerIngestionACLsLive proves the producer can use only the ordinary
// ingestion writes its protected advisory transaction actually invokes.
func TestProducerIngestionACLsLive(t *testing.T) {
	adminDSN := os.Getenv("ESHU_PRODUCER_ACL_DSN")
	ownerDSN := os.Getenv("ESHU_PRODUCER_ACL_OWNER_DSN")
	producerDSN := os.Getenv("ESHU_PRODUCER_ACL_PRODUCER_DSN")
	if os.Getenv("ESHU_PRODUCER_ACL_DISPOSABLE") != "1" ||
		adminDSN == "" || ownerDSN == "" || producerDSN == "" {
		t.Skip("requires explicitly marked disposable PostgreSQL role fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	open := func(dsn string) *sql.DB {
		t.Helper()
		database, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatal(err)
		}
		database.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = database.Close() })
		return database
	}
	// Each proof run gets its own database owned by its owner role: the
	// shared fixture database serves concurrent shards, so bootstrapping
	// its public schema as different owners collides on object
	// ownership. A dedicated database makes the proof hermetic.
	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	proofDB := "producer_acl_" + suffix
	proofDSN := func(dsn, role string) string {
		t.Helper()
		parsed, err := url.Parse(dsn)
		if err != nil {
			t.Fatalf("parse role DSN: %v", err)
		}
		parsed.Path = "/" + proofDB
		if role != "" {
			parsed.User = url.UserPassword(role, "local-proof-only")
		}
		return parsed.String()
	}
	admin := open(adminDSN)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = admin.ExecContext(cleanupCtx, `DROP DATABASE IF EXISTS "`+proofDB+`" WITH (FORCE)`)
		_, _ = admin.ExecContext(cleanupCtx, `DROP ROLE IF EXISTS "eshu_acl_owner_`+suffix+`", "eshu_acl_producer_`+suffix+`"`)
	})
	if _, err := admin.ExecContext(ctx, `
CREATE ROLE eshu_acl_owner_`+suffix+` LOGIN PASSWORD 'local-proof-only';
CREATE ROLE eshu_acl_producer_`+suffix+` LOGIN PASSWORD 'local-proof-only';
GRANT EXECUTE ON FUNCTION pg_catalog.pg_control_system()
  TO eshu_acl_owner_`+suffix+`,eshu_acl_producer_`+suffix+`;`); err != nil {
		t.Fatalf("prepare disposable roles: %v", err)
	}
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE "`+proofDB+`" OWNER "eshu_acl_owner_`+suffix+`"`); err != nil {
		t.Fatalf("create proof database: %v", err)
	}
	proofAdmin, err := sql.Open("pgx", proofDSN(adminDSN, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = proofAdmin.Close() }()
	if _, err := proofAdmin.ExecContext(ctx, `GRANT USAGE,CREATE ON SCHEMA public TO "eshu_acl_owner_`+suffix+`"`); err != nil {
		t.Fatalf("grant proof schema create: %v", err)
	}
	owner := open(proofDSN(ownerDSN, "eshu_acl_owner_"+suffix))
	if err := ApplyDefinitions(ctx, SQLDB{DB: owner}, BootstrapDefinitions()); err != nil {
		t.Fatalf("apply migrations as protected owner: %v", err)
	}
	if _, err := admin.ExecContext(ctx,
		`REVOKE CREATE ON DATABASE "`+proofDB+`" FROM "eshu_acl_owner_`+suffix+`"`); err != nil {
		t.Fatal(err)
	}
	const contractsTemplate = `[{
  "contract_id":"typed-producer-acl", "workload_class":"collector-vulnerability-intelligence",
  "connection_route":"producer", "profile":"typed-source-producer",
  "session_role":"eshu_acl_producer", "effective_role":"eshu_acl_producer"
}]`
	contracts := strings.ReplaceAll(contractsTemplate, "eshu_acl_producer", "eshu_acl_producer_"+suffix)
	if _, err := owner.ExecContext(ctx,
		`SELECT eshu_security.install_role_policy_v1(0,1,$1::jsonb)`, contracts); err != nil {
		t.Fatalf("install producer role policy: %v", err)
	}
	producer := open(proofDSN(producerDSN, "eshu_acl_producer_"+suffix))
	if _, err := producer.ExecContext(ctx, `SELECT 1 FROM eshu_security.attest_connection_v1(
  'typed-producer-acl',1,'collector-vulnerability-intelligence','producer')`); err != nil {
		t.Fatalf("attest producer connection: %v", err)
	}
	for _, permission := range []struct{ table, privilege string }{
		{"workflow_work_items", "SELECT"},
		{"workflow_work_items", "UPDATE"},
		{"workflow_claims", "SELECT"},
		{"workflow_claims", "UPDATE"},
		{"tenant_scope_grants", "SELECT"},
		{"tenants", "SELECT"},
		{"workspaces", "SELECT"},
		{"repository_retirements", "SELECT"},
		{"ingestion_scopes", "SELECT"},
		{"ingestion_scopes", "INSERT"},
		{"ingestion_scopes", "UPDATE"},
		{"scope_generations", "SELECT"},
		{"scope_generations", "INSERT"},
		{"scope_generations", "UPDATE"},
		{"fact_records", "SELECT"},
		{"fact_records", "INSERT"},
		{"fact_records", "UPDATE"},
		{"fact_work_items", "SELECT"},
		{"fact_work_items", "INSERT"},
		{"projector_scope_claim_fences", "SELECT"},
		{"projector_scope_claim_fences", "INSERT"},
		{"relationship_reference_candidate_keys", "DELETE"},
		{"relationship_reference_candidate_keys", "INSERT"},
		{"relationship_reference_candidate_keys", "UPDATE"},
		{"package_manifest_consumption_keys", "DELETE"},
		{"package_manifest_consumption_keys", "INSERT"},
		{"package_manifest_consumption_keys", "UPDATE"},
		{"package_registry_identity_keys", "DELETE"},
		{"package_registry_identity_keys", "INSERT"},
		{"package_registry_identity_keys", "UPDATE"},
	} {
		var allowed bool
		if err := producer.QueryRowContext(ctx,
			`SELECT has_table_privilege(current_user,$1,$2)`,
			"public."+permission.table, permission.privilege).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		if !allowed {
			t.Errorf("producer lacks %s on %s", permission.privilege, permission.table)
		}
	}
	for _, name := range []string{
		"scanner_authority_epoch_seq", "scanner_running_image_observation_id_seq",
		"container_image_identity_activation_epoch_seq",
	} {
		var allowed bool
		if err := producer.QueryRowContext(ctx,
			`SELECT has_sequence_privilege(current_user,$1,'USAGE')`,
			"public."+name).Scan(&allowed); err != nil {
			t.Fatal(err)
		}
		if allowed {
			t.Errorf("producer has protected sequence USAGE on %s", name)
		}
	}
	var protectedWrite bool
	if err := producer.QueryRowContext(ctx, `SELECT has_table_privilege(
  current_user,'public.scanner_advisory_public_registrations','INSERT')`).Scan(&protectedWrite); err != nil {
		t.Fatal(err)
	}
	if protectedWrite {
		t.Error("producer can directly insert protected registration")
	}
	// A new scope exercises the invoker projector-fence trigger as well as the
	// direct ingestion_scopes grant. A catalog privilege check alone misses it.
	if _, err := producer.ExecContext(ctx, `INSERT INTO ingestion_scopes
  (scope_id,scope_kind,source_system,source_key,collector_kind,partition_key,
   observed_at,ingested_at,status)
VALUES ('acl-public-scope','vulnerability_intelligence',
        'vulnerability_intelligence','acl-public-scope',
        'vulnerability_intelligence','acl-public-scope',now(),now(),'pending')`); err != nil {
		t.Fatalf("producer insert with projector-fence trigger: %v", err)
	}
	var fenceCount int
	if err := owner.QueryRowContext(ctx, `SELECT count(*) FROM projector_scope_claim_fences
WHERE scope_id='acl-public-scope'`).Scan(&fenceCount); err != nil {
		t.Fatal(err)
	}
	if fenceCount != 1 {
		t.Fatalf("projector claim fence rows = %d, want 1", fenceCount)
	}
}
