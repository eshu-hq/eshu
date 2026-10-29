// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9299 disposable-role proof that the producer role can use only the ordinary ingestion writes its protected advisory transaction invokes
//eshu:readiness:env ESHU_PRODUCER_ACL_DISPOSABLE ESHU_PRODUCER_ACL_DSN ESHU_PRODUCER_ACL_OWNER_DSN ESHU_PRODUCER_ACL_PRODUCER_DSN

import (
	"context"
	"database/sql"
	"os"
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
	admin := open(adminDSN)
	if _, err := admin.ExecContext(ctx, `
CREATE ROLE eshu_acl_owner LOGIN PASSWORD 'local-proof-only';
CREATE ROLE eshu_acl_producer LOGIN PASSWORD 'local-proof-only';
GRANT CREATE ON DATABASE postgres TO eshu_acl_owner;
GRANT USAGE,CREATE ON SCHEMA public TO eshu_acl_owner;
GRANT EXECUTE ON FUNCTION pg_catalog.pg_control_system()
  TO eshu_acl_owner,eshu_acl_producer;`); err != nil {
		t.Fatalf("prepare disposable roles: %v", err)
	}
	owner := open(ownerDSN)
	if err := ApplyDefinitions(ctx, SQLDB{DB: owner}, BootstrapDefinitions()); err != nil {
		t.Fatalf("apply migrations as protected owner: %v", err)
	}
	if _, err := admin.ExecContext(ctx,
		`REVOKE CREATE ON DATABASE postgres FROM eshu_acl_owner`); err != nil {
		t.Fatal(err)
	}
	const contracts = `[{
  "contract_id":"typed-producer-acl", "workload_class":"collector-vulnerability-intelligence",
  "connection_route":"producer", "profile":"typed-source-producer",
  "session_role":"eshu_acl_producer", "effective_role":"eshu_acl_producer"
}]`
	if _, err := owner.ExecContext(ctx,
		`SELECT eshu_security.install_role_policy_v1(0,1,$1::jsonb)`, contracts); err != nil {
		t.Fatalf("install producer role policy: %v", err)
	}
	producer := open(producerDSN)
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
