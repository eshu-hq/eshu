// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime_test

// Readiness enrollment suspended under drift-gate option A (PR #9544 fourth
// round): the attestation proof below stays in the Go suite and skips clean
// without proof DSNs; re-enroll post-refit. See live-tests-07b.yaml.

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	pgaccess "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestInstalledRolePolicyAttestsRuntimePhysicalConnection(t *testing.T) {
	adminDSN := os.Getenv("ESHU_ROLE_ATTEST_DSN")
	bootstrapDSN := os.Getenv("ESHU_ROLE_ATTEST_BOOTSTRAP_DSN")
	runtimeDSN := os.Getenv("ESHU_ROLE_ATTEST_RUNTIME_DSN")
	readerDSN := os.Getenv("ESHU_ROLE_ATTEST_READER_DSN")
	controlDSN := os.Getenv("ESHU_ROLE_ATTEST_CONTROL_DSN")
	producerDSN := os.Getenv("ESHU_ROLE_ATTEST_PRODUCER_DSN")
	advisoryDSN := os.Getenv("ESHU_ROLE_ATTEST_ADVISORY_DSN")
	if os.Getenv("ESHU_ROLE_ATTEST_DISPOSABLE") != "1" || adminDSN == "" || bootstrapDSN == "" || runtimeDSN == "" || readerDSN == "" || controlDSN == "" || producerDSN == "" || advisoryDSN == "" {
		t.Skip("requires an explicitly marked disposable PostgreSQL role fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close() }()
	// Each proof run gets unique role names plus its own database owned
	// by its owner role. The shared fixture database serves concurrent
	// shards, so bootstrapping its public schema as different owners
	// collides on object ownership, and fixed role names collide across
	// runs. A dedicated database makes the proof hermetic; cleanup drops
	// only this run's database and names.
	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	proofDB := "role_migration_" + suffix
	roleName := func(base string) string { return base + "_" + suffix }
	renameRoles := func(blob string) string {
		for _, base := range []string{"eshu_role_owner", "eshu_role_bootstrap", "eshu_role_runtime", "eshu_role_reader", "eshu_role_control", "eshu_role_producer", "eshu_role_advisory"} {
			blob = strings.ReplaceAll(blob, base, roleName(base))
		}
		return blob
	}
	// proofDSN keeps host, port, and parameters from the harness DSN but
	// targets this run's database authenticating as the given proof role.
	// The readiness harness maps every role DSN to one superuser
	// connection; the proof re-points credentials at the roles it
	// creates itself below.
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
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		quoted := make([]string, 0, 7)
		for _, base := range []string{"eshu_role_owner", "eshu_role_bootstrap", "eshu_role_runtime", "eshu_role_reader", "eshu_role_control", "eshu_role_producer", "eshu_role_advisory"} {
			quoted = append(quoted, `"`+roleName(base)+`"`)
		}
		// Best effort on a disposable fixture: the uniquely-named
		// database goes first (pools close before cleanup runs), then
		// the names nothing else references.
		_, _ = admin.ExecContext(cleanupCtx, `DROP DATABASE IF EXISTS "`+proofDB+`" WITH (FORCE)`)
		_, _ = admin.ExecContext(cleanupCtx, `DROP ROLE IF EXISTS `+strings.Join(quoted, ","))
	})
	const rolesTemplate = `
CREATE ROLE eshu_role_owner NOLOGIN;
CREATE ROLE eshu_role_bootstrap LOGIN PASSWORD 'local-proof-only';
CREATE ROLE eshu_role_runtime LOGIN PASSWORD 'local-proof-only';
CREATE ROLE eshu_role_reader LOGIN PASSWORD 'local-proof-only';
CREATE ROLE eshu_role_control LOGIN PASSWORD 'local-proof-only';
CREATE ROLE eshu_role_producer LOGIN PASSWORD 'local-proof-only';
CREATE ROLE eshu_role_advisory LOGIN PASSWORD 'local-proof-only';
GRANT eshu_role_owner TO eshu_role_bootstrap WITH INHERIT TRUE, SET TRUE;
ALTER ROLE eshu_role_bootstrap SET role TO eshu_role_owner;
GRANT pg_read_all_stats TO eshu_role_owner;
GRANT EXECUTE ON FUNCTION pg_catalog.pg_control_system()
  TO eshu_role_owner,eshu_role_runtime,eshu_role_reader,
     eshu_role_control,eshu_role_producer,eshu_role_advisory;`
	if _, err := admin.ExecContext(ctx, renameRoles(rolesTemplate)); err != nil {
		t.Fatalf("prepare disposable roles: %v", err)
	}
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE "`+proofDB+`" OWNER "`+roleName("eshu_role_owner")+`"`); err != nil {
		t.Fatalf("create proof database: %v", err)
	}
	proofAdmin, err := sql.Open("pgx", proofDSN(adminDSN, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = proofAdmin.Close() }()
	if _, err := proofAdmin.ExecContext(ctx, renameRoles(`GRANT CREATE ON SCHEMA public TO eshu_role_owner`)); err != nil {
		t.Fatalf("grant proof schema create: %v", err)
	}
	bootstrap, err := sql.Open("pgx", proofDSN(bootstrapDSN, roleName("eshu_role_bootstrap")))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bootstrap.Close() }()
	bootstrap.SetMaxOpenConns(1)
	var sessionRole, effectiveRole string
	if err := bootstrap.QueryRowContext(ctx, `SELECT session_user,current_user`).Scan(&sessionRole, &effectiveRole); err != nil {
		t.Fatal(err)
	}
	if sessionRole != roleName("eshu_role_bootstrap") || effectiveRole != roleName("eshu_role_owner") {
		t.Fatalf("bootstrap principal = %s/%s, want login/owner", sessionRole, effectiveRole)
	}
	if err := postgres.ApplyDefinitions(ctx, postgres.SQLDB{DB: bootstrap}, postgres.BootstrapDefinitions()); err != nil {
		t.Fatalf("apply migrations as protected owner: %v", err)
	}
	// Revoke database-level CREATE after bootstrap (hardening), keeping
	// the schema-level CREATE the bootstrap profile's required
	// inventory row attests below.
	if _, err := admin.ExecContext(ctx, `REVOKE CREATE ON DATABASE "`+proofDB+`" FROM "`+roleName("eshu_role_owner")+`"`); err != nil {
		t.Fatal(err)
	}
	const contracts = `[
 {"contract_id":"runtime-writer-1","workload_class":"runtime","connection_route":"writer","profile":"runtime-writer","session_role":"eshu_role_runtime","effective_role":"eshu_role_runtime"},
 {"contract_id":"api-writer-1","workload_class":"api","connection_route":"writer","profile":"runtime-writer","session_role":"eshu_role_runtime","effective_role":"eshu_role_runtime"},
 {"contract_id":"api-reader-1","workload_class":"api","connection_route":"reader","profile":"runtime-reader","session_role":"eshu_role_reader","effective_role":"eshu_role_reader"},
 {"contract_id":"mcp-writer-1","workload_class":"mcp-server","connection_route":"writer","profile":"runtime-writer","session_role":"eshu_role_runtime","effective_role":"eshu_role_runtime"},
 {"contract_id":"mcp-reader-1","workload_class":"mcp-server","connection_route":"reader","profile":"runtime-reader","session_role":"eshu_role_reader","effective_role":"eshu_role_reader"},
 {"contract_id":"workflow-control-1","workload_class":"workflow-coordinator","connection_route":"control","profile":"authority-control","session_role":"eshu_role_control","effective_role":"eshu_role_control"},
 {"contract_id":"typed-producer-1","workload_class":"collector-kubernetes-live","connection_route":"producer","profile":"typed-source-producer","session_role":"eshu_role_producer","effective_role":"eshu_role_producer"},
 {"contract_id":"advisory-producer-1","workload_class":"collector-vulnerability-intelligence","connection_route":"producer","profile":"typed-source-producer","session_role":"eshu_role_advisory","effective_role":"eshu_role_advisory"},
 {"contract_id":"schema-bootstrap-1","workload_class":"schema-bootstrap","connection_route":"bootstrap","profile":"schema-bootstrap","session_role":"eshu_role_bootstrap","effective_role":"eshu_role_owner"}
]`
	var databaseIdentity string
	if err := bootstrap.QueryRowContext(ctx,
		`SELECT eshu_security.install_role_policy_v1(0,1,$1::jsonb)`, renameRoles(contracts),
	).Scan(&databaseIdentity); err != nil {
		t.Fatalf("install role policy: %v", err)
	}
	var bootstrapRevision int64
	var bootstrapProfile string
	if err := bootstrap.QueryRowContext(ctx, `
SELECT policy_revision,profile FROM eshu_security.attest_connection_v1(
  'schema-bootstrap-1',1,'schema-bootstrap','bootstrap')`,
	).Scan(&bootstrapRevision, &bootstrapProfile); err != nil || bootstrapRevision != 1 || bootstrapProfile != "schema-bootstrap" {
		t.Fatalf("attest installed bootstrap session: revision=%d profile=%q err=%v", bootstrapRevision, bootstrapProfile, err)
	}
	var database string
	if err := bootstrap.QueryRowContext(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		t.Fatal(err)
	}
	assertion := renameRoles(fmt.Sprintf(`{"policy_revision":1,"database_identity":%q,"database":%q,"routes":{"writer":{"contract_id":"runtime-writer-1","session_role":"eshu_role_runtime","effective_role":"eshu_role_runtime"}}}`, databaseIdentity, database))
	getenv := func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN", "ESHU_CONTENT_STORE_DSN":
			return proofDSN(runtimeDSN, roleName("eshu_role_runtime"))
		case "ESHU_POSTGRES_ROLE_CONTRACT_JSON":
			return assertion
		case "ESHU_POSTGRES_ROLE_BOUNDARY_REQUIRED":
			return "true"
		default:
			return ""
		}
	}
	pool, err := runtimecfg.OpenPostgresForWorkload(ctx, getenv, runtimecfg.PostgresWorkloadRuntime)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()
	if err := pool.PingContext(ctx); err != nil {
		t.Fatalf("attest installed runtime physical connection: %v", err)
	}
	for _, workload := range []struct {
		class    runtimecfg.PostgresWorkload
		writerID string
		readerID string
	}{
		{runtimecfg.PostgresWorkloadAPI, "api-writer-1", "api-reader-1"},
		{runtimecfg.PostgresWorkloadMCP, "mcp-writer-1", "mcp-reader-1"},
	} {
		assertions := renameRoles(fmt.Sprintf(`{"policy_revision":1,"database_identity":%q,"database":%q,"routes":{"writer":{"contract_id":%q,"session_role":"eshu_role_runtime","effective_role":"eshu_role_runtime"},"reader":{"contract_id":%q,"session_role":"eshu_role_reader","effective_role":"eshu_role_reader"}}}`, databaseIdentity, database, workload.writerID, workload.readerID))
		readEnv := func(key string) string {
			switch key {
			case "ESHU_POSTGRES_DSN":
				return proofDSN(runtimeDSN, roleName("eshu_role_runtime"))
			case "ESHU_POSTGRES_READ_DSN":
				return proofDSN(readerDSN, roleName("eshu_role_reader"))
			case "ESHU_POSTGRES_READ_TOPOLOGY":
				return "primary"
			case "ESHU_POSTGRES_ROLE_CONTRACT_JSON":
				return assertions
			case "ESHU_POSTGRES_ROLE_BOUNDARY_REQUIRED":
				return "true"
			default:
				return ""
			}
		}
		cfg, err := pgaccess.LoadRoleAttestedConfig(readEnv, workload.class)
		if err != nil {
			t.Fatalf("%s role-attested config: %v", workload.class, err)
		}
		access, err := pgaccess.Open(ctx, cfg, nil)
		if err != nil {
			t.Fatalf("%s physical writer/reader attestation: %v", workload.class, err)
		}
		_ = access.Close()
	}
	controlAssertion := renameRoles(fmt.Sprintf(`{"policy_revision":1,"database_identity":%q,"database":%q,"routes":{"control":{"contract_id":"workflow-control-1","session_role":"eshu_role_control","effective_role":"eshu_role_control"}}}`, databaseIdentity, database))
	controlEnv := func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return proofDSN(controlDSN, roleName("eshu_role_control"))
		case "ESHU_POSTGRES_ROLE_CONTRACT_JSON":
			return controlAssertion
		case "ESHU_POSTGRES_ROLE_BOUNDARY_REQUIRED":
			return "true"
		default:
			return ""
		}
	}
	controlPool, err := runtimecfg.OpenPostgresForWorkload(ctx, controlEnv, runtimecfg.PostgresWorkloadCoordinator)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = controlPool.Close() }()
	if err := controlPool.PingContext(ctx); err != nil {
		t.Fatalf("attest installed coordinator control connection: %v", err)
	}
	advisoryAssertion := renameRoles(fmt.Sprintf(`{"policy_revision":1,"database_identity":%q,"database":%q,"routes":{"producer":{"contract_id":"advisory-producer-1","session_role":"eshu_role_advisory","effective_role":"eshu_role_advisory"}}}`, databaseIdentity, database))
	advisoryEnv := func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return proofDSN(runtimeDSN, roleName("eshu_role_runtime"))
		case "ESHU_POSTGRES_PRODUCER_DSN":
			return proofDSN(advisoryDSN, roleName("eshu_role_advisory"))
		case "ESHU_POSTGRES_ROLE_CONTRACT_JSON":
			return advisoryAssertion
		case "ESHU_POSTGRES_ROLE_BOUNDARY_REQUIRED":
			return "true"
		default:
			return ""
		}
	}
	advisoryPool, err := runtimecfg.OpenPostgresForWorkload(ctx, advisoryEnv, runtimecfg.PostgresWorkloadAdvisoryProducer)
	if err != nil {
		t.Fatalf("open dedicated advisory producer: %v", err)
	}
	defer func() { _ = advisoryPool.Close() }()
	var advisoryLogin string
	if err := advisoryPool.QueryRowContext(ctx, `SELECT session_user`).Scan(&advisoryLogin); err != nil || advisoryLogin != roleName("eshu_role_advisory") {
		t.Fatalf("advisory producer physical login = %q, err=%v", advisoryLogin, err)
	}
	const sourceGrant = `
INSERT INTO collector_instances
  (instance_id,collector_kind,mode,enabled,claims_enabled,configuration,last_observed_at,created_at,updated_at)
VALUES ('typed-k8s','kubernetes_live','continuous',true,true,'{}'::jsonb,now(),now(),now());
INSERT INTO scanner_running_image_producer_grants
  (principal_oid,producer_instance_id,source_scope_id,producer_class,observation_class,contract_version,active)
VALUES ('eshu_role_producer'::regrole,'typed-k8s','kubernetes://cluster-a',
        'kubernetes_pod','running_container',1,true);`
	if _, err := bootstrap.ExecContext(ctx, renameRoles(sourceGrant)); err != nil {
		t.Fatalf("install exact producer grant: %v", err)
	}
	producer, err := sql.Open("pgx", proofDSN(producerDSN, roleName("eshu_role_producer")))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = producer.Close() }()
	producerConn, err := producer.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = producerConn.Close() }()
	var producerRevision int64
	var producerProfile string
	if err := producerConn.QueryRowContext(ctx, `
SELECT policy_revision,profile FROM eshu_security.attest_connection_v1(
  'typed-producer-1',1,'collector-kubernetes-live','producer')`,
	).Scan(&producerRevision, &producerProfile); err != nil || producerRevision != 1 || producerProfile != "typed-source-producer" {
		t.Fatalf("attest installed producer session: revision=%d profile=%q err=%v", producerRevision, producerProfile, err)
	}
	const startCollection = `SELECT eshu_security.start_running_image_collection_v1(
  'typed-k8s','kubernetes://cluster-a','fresh-generation-a',
  'kubernetes_pod','running_container',now(),now(),
  'observed_interval',60,1,$1::uuid)`
	var fence int64
	if err := producerConn.QueryRowContext(ctx, startCollection, databaseIdentity).Scan(&fence); err != nil {
		t.Fatalf("authorized producer start: %v", err)
	}
	if fence <= 0 {
		t.Fatalf("authorized producer fence = %d, want positive", fence)
	}
	if err := pool.QueryRowContext(ctx, startCollection, databaseIdentity).Scan(&fence); err == nil {
		t.Fatal("generic runtime writer invoked producer-only collection start")
	}
	contract, err := runtimecfg.LoadPostgresRoleContract(getenv, runtimecfg.PostgresWorkloadRuntime, runtimecfg.PostgresRouteWriter)
	if err != nil {
		t.Fatal(err)
	}
	physical, err := pgconn.Connect(ctx, proofDSN(runtimeDSN, roleName("eshu_role_runtime")))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = physical.Close(context.Background()) }()
	contract.PolicyRevision = 2
	if err := contract.Attest(ctx, physical); err == nil {
		t.Fatal("stale role policy revision accepted")
	}
}
