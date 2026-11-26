// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9299 disposable PostgreSQL proof that a complete protected static impact write commits its finding fact and authority revision instead of dead-lettering on the missing publication transaction
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_POSTGRES_DISPOSABLE

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

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// protectedPublishProof is one disposable database with the protected
// publication policy seeded and a least-privilege worker login connected.
type protectedPublishProof struct {
	ctx              context.Context
	admin            *sql.DB
	worker           *sql.DB
	policyRevision   int
	databaseIdentity string
}

// openProtectedPublishProof builds the protected-publish proof database: the
// runtime-writer role contract, the inventory grants and a worker connection
// that can reach publish_impact_publication_v1 only through the contract.
func openProtectedPublishProof(t *testing.T) *protectedPublishProof {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	optIn := os.Getenv("ESHU_POSTGRES_DISPOSABLE")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN and ESHU_POSTGRES_DISPOSABLE=1 to run the protected publish proof")
	}
	// The family DSN names the proof's working database (for example "eshu" in
	// the reducer contention gate); the disposable helpers require the
	// administrative "postgres" database, so derive it the way
	// openIsolatedLiveDB does instead of passing the family DSN through.
	adminDSN := disposableAdminDSN(t, dsn)
	// Clones the process template (#10148 PR-A) instead of bootstrapping again.
	template := postgresproof.OpenBootstrapTemplate(t, adminDSN, optIn, 5*time.Minute,
		postgresproof.FlavourPublicBootstrap,
		func(ctx context.Context, database *sql.DB) error {
			return ApplyBootstrap(ctx, SQLDB{DB: database})
		})
	ctx, admin := postgresproof.OpenBootstrapTemplateClone(t, adminDSN, optIn, 5*time.Minute, template)

	for _, function := range []string{
		"eshu_security.authorize_impact_publication_v1",
		"eshu_security.publish_impact_publication_v1",
	} {
		var exists bool
		if err := admin.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_proc WHERE oid = to_regprocedure($1))`, function+"(text,text,bigint,jsonb,bigint,uuid)").Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("protected publisher %s is not implemented: complete protected writes cannot commit", function)
		}
	}

	const roleName = "publish_proof_worker"
	const rolePassword = "local-proof-only"
	if _, err := admin.ExecContext(ctx, `DROP ROLE IF EXISTS "`+roleName+`"`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `CREATE ROLE "`+roleName+`" LOGIN PASSWORD '`+rolePassword+`'`); err != nil {
		t.Fatalf("create proof worker role: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP ROLE IF EXISTS "`+roleName+`"`)
	})
	var roleOID int64
	if err := admin.QueryRowContext(ctx, `SELECT oid::bigint FROM pg_roles WHERE rolname=$1`, roleName).Scan(&roleOID); err != nil {
		t.Fatal(err)
	}
	const policyRevision = 929901
	const databaseIdentity = "11111111-2222-4333-8444-555555555555"
	if _, err := admin.ExecContext(ctx, `INSERT INTO eshu_security.role_policy_state
		(policy_revision, database_oid, database_identity, system_identifier,
		 inventory_version, owner_role_oid, authority_epoch, active)
		SELECT $1, (SELECT oid FROM pg_database WHERE datname=current_database()), $2,
		 (SELECT system_identifier FROM pg_control_system()), 1,
		 (SELECT oid FROM pg_roles WHERE rolname=current_user), 1, TRUE`,
		policyRevision, databaseIdentity); err != nil {
		t.Fatalf("seed proof policy state: %v", err)
	}
	if _, err := admin.ExecContext(ctx, `INSERT INTO eshu_security.role_contracts
		(contract_id, policy_revision, workload_class, connection_route, profile,
		 session_role_oid, effective_role_oid, enabled)
		VALUES ('proof-runtime-writer',$1,'scanner-worker','writer','runtime-writer',$2,$2,TRUE)`,
		policyRevision, roleOID); err != nil {
		t.Fatalf("seed proof worker contract: %v", err)
	}
	if _, err := admin.ExecContext(ctx, `INSERT INTO scanner_advisory_desired_policy
		(desired_revision, desired_sha256) VALUES (1,$1)`,
		strings.Repeat("0", 64)); err != nil {
		t.Fatalf("seed desired advisory policy: %v", err)
	}
	var dbName string
	if err := admin.QueryRowContext(ctx, `SELECT current_database()`).Scan(&dbName); err != nil {
		t.Fatal(err)
	}
	// Inventory-required privileges first: the protected capability assert
	// fail-closes on any required grant the worker lacks, so every required
	// runtime-writer row is granted from the inventory itself.
	grantInventoryRequired(t, ctx, admin, roleName, "runtime-writer")
	grantImpactWriterPregrants(t, ctx, admin, dbName, roleName)
	workerDSN, err := proofWorkerDSN(adminDSN, dbName, roleName, rolePassword)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := sql.Open("pgx", workerDSN)
	if err != nil {
		t.Fatal(err)
	}
	worker.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = worker.Close() })

	return &protectedPublishProof{
		ctx: ctx, admin: admin, worker: worker,
		policyRevision: policyRevision, databaseIdentity: databaseIdentity,
	}
}

// TestSupplyChainImpactProtectedStaticPublishLive drives the production
// writer path end to end as a contracted runtime-writer: a complete
// protected static write must commit its finding fact and its authority
// revision row. Before the publisher exists this fails with "protected scan
// impact publication transaction is required" (the golden-corpus drain
// dead-letter behind the same error); after, the write commits and the
// revision pins the fence, payload hash, role revision, and identity.
func TestSupplyChainImpactProtectedStaticPublishLive(t *testing.T) {
	proof := openProtectedPublishProof(t)
	ctx, admin, worker := proof.ctx, proof.admin, proof.worker
	policyRevision, databaseIdentity := proof.policyRevision, proof.databaseIdentity

	const scopeID = "publish-proof-scope"
	const generationID = "publish-proof-gen"
	const factID = "publish-proof-fact"
	now := time.Now().UTC().Truncate(time.Second)
	mustExecProofRow(t, ctx, admin, `INSERT INTO ingestion_scopes
		(scope_id, scope_kind, source_system, source_key, collector_kind,
		 partition_key, observed_at, ingested_at, status, payload)
		VALUES ($1,'container_image','synthetic',$1,'synthetic',$1,$2,$2,'active','{}'::jsonb)`,
		scopeID, now)
	mustExecProofRow(t, ctx, admin, `INSERT INTO scope_generations
		(generation_id, scope_id, trigger_kind, observed_at, ingested_at,
		 status, activated_at, payload)
		VALUES ($1,$2,'synthetic',$3,$3,'active',$3,'{}'::jsonb)`,
		generationID, scopeID, now)
	mustExecProofRow(t, ctx, admin,
		`UPDATE ingestion_scopes SET active_generation_id=$1 WHERE scope_id=$2`,
		generationID, scopeID)
	write := supplychaincore.SupplyChainImpactWrite{
		FencingToken: 7,
		IntentID:     "intent-publish-proof",
		ScopeID:      scopeID,
		GenerationID: generationID,
		SourceSystem: "vulnerability_intelligence",
		Findings: []supplychaincore.SupplyChainImpactFinding{
			{
				CVEID:           "CVE-2026-9299-1",
				PackageID:       "pkg:npm/publish-proof",
				Status:          supplychaincore.SupplyChainImpactAffectedExact,
				RepositoryID:    "repo://publish-proof/api",
				EvidenceFactIDs: []string{factID},
			},
		},
		StaticNonAdvisoryEvidenceFactIDs: []string{factID},
		ProtectedCompleteSet:             true,
	}
	writer := supplychaincore.PostgresSupplyChainImpactWriter{
		DB: SupplyChainImpactBeginner{Beginner: SQLDB{DB: worker}},
	}
	result, err := writer.WriteSupplyChainImpactFindings(ctx, write)
	if err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v, want nil", err)
	}
	if result.FactsWritten != 1 {
		t.Fatalf("FactsWritten = %d, want 1", result.FactsWritten)
	}
	var revisionCount int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM scanner_static_impact_finding_authority_revisions
		WHERE impact_scope_id=$1 AND impact_generation_id=$2 AND impact_fencing_token=7
		  AND advisory_dependency_count=0 AND pinned_role_policy_revision=$3
		  AND pinned_database_identity=$4`,
		scopeID, generationID, policyRevision, databaseIdentity).Scan(&revisionCount); err != nil {
		t.Fatal(err)
	}
	if revisionCount != 1 {
		t.Fatalf("authority revisions = %d, want exactly one pinned static revision", revisionCount)
	}
	// The writer keeps a finding only in the supports table since #10880 PR-C2:
	// the support exists and no finding fact was written (the reject trigger is armed).
	var supportCount, factCount int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM supply_chain_impact_finding_supports
		WHERE scope_id=$1 AND generation_id=$2`, scopeID, generationID).Scan(&supportCount); err != nil {
		t.Fatal(err)
	}
	if supportCount != 1 {
		t.Fatalf("supports = %d, want the one committed finding", supportCount)
	}
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM fact_records
		WHERE scope_id=$1 AND generation_id=$2 AND fact_kind='reducer_supply_chain_impact_finding'`, scopeID, generationID).Scan(&factCount); err != nil {
		t.Fatal(err)
	}
	if factCount != 0 {
		t.Fatalf("finding facts = %d, want 0: the writer no longer writes them", factCount)
	}

	// A byte-identical retry at the same fence replays as a no-op instead
	// of conflicting: the write commits and the revision count is unchanged.
	replay, err := writer.WriteSupplyChainImpactFindings(ctx, write)
	if err != nil {
		t.Fatalf("identical retry WriteSupplyChainImpactFindings() error = %v, want nil", err)
	}
	if replay.FactsWritten != 1 {
		t.Fatalf("identical retry FactsWritten = %d, want 1", replay.FactsWritten)
	}
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM scanner_static_impact_finding_authority_revisions
		WHERE impact_scope_id=$1 AND impact_generation_id=$2 AND impact_fencing_token=7`,
		scopeID, generationID).Scan(&revisionCount); err != nil {
		t.Fatal(err)
	}
	if revisionCount != 1 {
		t.Fatalf("authority revisions after retry = %d, want exactly one", revisionCount)
	}
	requireNoSupportsFenceMarks(t, ctx, admin)

	// A revoked writer authority fail-closes the next pass: with the
	// worker contract disabled, authorization refuses before any fact write.
	mustExecProofRow(t, ctx, admin, `UPDATE eshu_security.role_contracts SET enabled=FALSE`)
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, write); err == nil {
		t.Fatal("WriteSupplyChainImpactFindings() after contract revoke = nil, want the authority refusal")
	} else if !strings.Contains(err.Error(), "principal denied") {
		t.Fatalf("WriteSupplyChainImpactFindings() after contract revoke = %v, want the principal refusal", err)
	}
}

// requireNoSupportsFenceMarks proves the real impact writer, running through a
// role-bound login, carries the maintainer's transaction setting and so marks no
// scope dirty: the fence is for writers without the maintainer only.
func requireNoSupportsFenceMarks(t *testing.T, ctx context.Context, admin *sql.DB) {
	t.Helper()
	var marks int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM supply_chain_impact_finding_supports_dirty`).Scan(&marks); err != nil {
		t.Fatalf("count fence marks: %v", err)
	}
	if marks != 0 {
		t.Fatalf("the role-bound impact writer left %d scopes marked dirty, want 0: it must carry the maintainer setting", marks)
	}
}

func mustExecProofRow(t *testing.T, ctx context.Context, admin *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := admin.ExecContext(ctx, query, args...); err != nil {
		t.Fatal(err)
	}
}

// grantImpactWriterPregrants issues the DBA pregrant list a least-privilege
// runtime-writer login needs to run the supply-chain impact writer: the base
// tables the writer touches outside any SECURITY DEFINER body, which the
// role ACL inventory (migration 243a) does not list. Every restricted-role
// impact proof calls it after grantInventoryRequired, so a table the writer
// starts to touch is added here once, not per test. It documents the same
// requirement a database with least-privilege logins must meet in production
// (docs/public/reference/telemetry/supply-chain-impact-supports.md and
// supply-chain-impact-winners.md).
func grantImpactWriterPregrants(t *testing.T, ctx context.Context, admin *sql.DB, dbName, roleName string) {
	t.Helper()
	for _, statement := range []string{
		`GRANT CONNECT ON DATABASE "` + dbName + `" TO "` + roleName + `"`,
		// Ordinary fact and admission DML the writer path issues outside
		// any DEFINER body. Base-table privileges are a DBA pregrant outside
		// the migration set (243a); the proof grants them explicitly.
		`GRANT SELECT, INSERT, UPDATE ON supply_chain_impact_write_admission TO "` + roleName + `"`,
		// The impact writer records per-scope pass state in the same
		// transaction (#10389): an ordinary base table, so a DBA pregrant
		// like the admission table above.
		`GRANT SELECT, INSERT, UPDATE ON supply_chain_impact_scope_pass_state TO "` + roleName + `"`,
		`GRANT SELECT, INSERT, UPDATE ON fact_records TO "` + roleName + `"`,
		// The impact writer keeps the per-support table current in the same
		// transaction (#10880 PR-B1): upsert and retraction delete. An ordinary base
		// table, so a DBA pregrant like the admission table above. The old-writer
		// fence trigger needs no grant for any login: its function runs as the
		// owner (migration 338), and this writer's transaction sets the
		// supports_v1 setting so the trigger skips its rows anyway.
		`GRANT SELECT, INSERT, UPDATE, DELETE ON supply_chain_impact_finding_supports TO "` + roleName + `"`,
		// The writer's two retraction statements record the keys they retract in the same
		// transaction (#10880 PR-C1, migration 351): the record is read for the keys it
		// already holds (NOT EXISTS) and inserted into. An ordinary base table, so a DBA
		// pregrant like the supports table; rows leave it through the generation foreign key
		// cascade, which the owner runs. The scope-delete and scanner capture functions
		// (migrations 352, 353) are SECURITY DEFINER and need no grant for any login.
		`GRANT SELECT, INSERT ON supply_chain_impact_retracted_finding_keys TO "` + roleName + `"`,
		// The first-seen stamp claim (#11031, migration 357) inserts one row per
		// stable fact key and reads it back in the same transaction. An ordinary
		// base table, so a DBA pregrant like the tables above; ON CONFLICT DO
		// NOTHING never updates and nothing deletes, so INSERT plus SELECT
		// suffice. (The runtime-writer login owns its tables in QA/prod, so this
		// grant only matters to least-privilege proofs like this one.)
		`GRANT SELECT, INSERT ON supply_chain_impact_finding_first_seen TO "` + roleName + `"`,
		// The change set behind the holder fan-out (#9695) reads scope_generations
		// inside the write transaction to find the previous generation. The
		// runtime-writer login already needs this SELECT on main, for every
		// reducer claim (storage/postgres/reducer_generation_filter_sql.go:121-123)
		// and for the impact load (storage/postgres/facts/impact/branches.go:82).
		// It is an ordinary base table, not a protected object; the inventory
		// above is unchanged.
		`GRANT SELECT ON scope_generations TO "` + roleName + `"`,
		// The Go-evidence recheck at the end of the write transaction (#11327) reads
		// the go scope's active pointer. The runtime-writer login already reads
		// ingestion_scopes for every reducer claim, so this is the same ordinary
		// base-table SELECT, listed here so a restricted login that lacks it fails
		// the proof instead of production.
		`GRANT SELECT ON ingestion_scopes TO "` + roleName + `"`,
		// The writer stages the identifiers of the findings it wrote in the
		// same transaction (#9805, #10837 direction C). Like fact_records,
		// the staging queue is an ordinary base table whose privileges are a
		// DBA pregrant: INSERT plus the ordering sequence (the collapsed
		// version math runs as the maintainer, not the writer, so the writer
		// needs no UPDATE and no log grant for the capture itself; the log
		// grants below stay for the pre-existing paths that share this role).
		`GRANT INSERT ON supply_chain_impact_winners_dirty_staging TO "` + roleName + `"`,
		`GRANT USAGE ON SEQUENCE supply_chain_impact_winners_dirty_staging_seq TO "` + roleName + `"`,
		`GRANT SELECT, INSERT, UPDATE ON supply_chain_impact_winners_dirty_log TO "` + roleName + `"`,
		`GRANT USAGE ON SEQUENCE supply_chain_impact_winners_dirty_log_seq_seq TO "` + roleName + `"`,
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("grant proof privilege: %v", err)
		}
	}
}

func grantInventoryRequired(t *testing.T, ctx context.Context, admin *sql.DB, role, profile string) {
	t.Helper()
	rows, err := admin.QueryContext(ctx, `SELECT object_kind, object_identity, privilege_name
		FROM eshu_security.role_acl_inventory
		WHERE inventory_version=1 AND profile=$1 AND required AND object_kind<>'table_owner'`, profile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	quoted := `"` + role + `"`
	for rows.Next() {
		var kind, identity, privilege string
		if err := rows.Scan(&kind, &identity, &privilege); err != nil {
			t.Fatal(err)
		}
		var statement string
		switch kind {
		case "table":
			statement = `GRANT ` + privilege + ` ON TABLE ` + identity + ` TO ` + quoted
		case "function":
			statement = `GRANT ` + privilege + ` ON FUNCTION ` + identity + ` TO ` + quoted
		case "schema":
			statement = `GRANT ` + privilege + ` ON SCHEMA ` + identity + ` TO ` + quoted
		default:
			t.Fatalf("unsupported inventory kind %q for %s", kind, identity)
		}
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("grant %s on %s: %v", privilege, identity, err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func proofWorkerDSN(adminDSN, dbName, role, password string) (string, error) {
	parsed, err := url.Parse(adminDSN)
	if err != nil {
		return "", fmt.Errorf("parse admin DSN: %w", err)
	}
	parsed.User = url.UserPassword(role, password)
	parsed.Path = "/" + dbName
	return parsed.String(), nil
}

// TestSupplyChainImpactProtectedPublishCapturesWinnersDirtyKeysLive is the
// seeded-violation pair for the scanner triggers on the real publish path
// (#9805, #10837 direction C). The protected publication function inserts
// classification and association rows as its owner, from the runtime-writer's
// transaction. Under the staging queue every mark stages its own row, so the
// discriminant is the staged row count by source: with the insert triggers
// disabled only the writer's own marks land; with them enabled the scanner
// trigger's marks land too, which is the capture hole the audit exists to
// repair.
func TestSupplyChainImpactProtectedPublishCapturesWinnersDirtyKeysLive(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN")) == "" {
		t.Skip("set ESHU_POSTGRES_DSN and ESHU_POSTGRES_DISPOSABLE=1 to run the scanner capture proof")
	}
	proof := openProtectedPublishProof(t)
	ctx, admin := proof.ctx, proof.admin

	writer := supplychaincore.PostgresSupplyChainImpactWriter{
		DB: SupplyChainImpactBeginner{Beginner: SQLDB{DB: proof.worker}},
	}
	now := time.Now().UTC().Truncate(time.Second)
	seedScope := func(scope, generation string) {
		mustExecProofRow(t, ctx, admin, `INSERT INTO ingestion_scopes
			(scope_id, scope_kind, source_system, source_key, collector_kind,
			 partition_key, observed_at, ingested_at, status, payload)
			VALUES ($1,'container_image','synthetic',$1,'synthetic',$1,$2,$2,'active','{}'::jsonb)`, scope, now)
		mustExecProofRow(t, ctx, admin, `INSERT INTO scope_generations
			(generation_id, scope_id, trigger_kind, observed_at, ingested_at,
			 status, activated_at, payload)
			VALUES ($1,$2,'synthetic',$3,$3,'active',$3,'{}'::jsonb)`, generation, scope, now)
		mustExecProofRow(t, ctx, admin, `UPDATE ingestion_scopes SET active_generation_id=$1 WHERE scope_id=$2`, generation, scope)
	}
	publish := func(scope, generation, cve string) {
		t.Helper()
		const evidence = "scanner-capture-evidence"
		_, err := writer.WriteSupplyChainImpactFindings(ctx, supplychaincore.SupplyChainImpactWrite{
			FencingToken: 7, IntentID: "intent-" + scope, ScopeID: scope, GenerationID: generation,
			SourceSystem: "vulnerability_intelligence",
			Findings: []supplychaincore.SupplyChainImpactFinding{{
				CVEID: cve, PackageID: "pkg:npm/scanner-capture", Status: supplychaincore.SupplyChainImpactAffectedExact,
				RepositoryID: "repo://scanner-capture/api", EvidenceFactIDs: []string{evidence},
			}},
			StaticNonAdvisoryEvidenceFactIDs: []string{evidence},
			ProtectedCompleteSet:             true,
		})
		if err != nil {
			t.Fatalf("publish %s: %v", scope, err)
		}
	}
	stagedMarksOf := func(cve string) (writer, scanner int64) {
		t.Helper()
		if err := admin.QueryRowContext(ctx, `
SELECT count(*) FILTER (WHERE source IN ('fact_upsert', 'fact_retract')),
       count(*) FILTER (WHERE source = 'scanner')
FROM supply_chain_impact_winners_dirty_staging
WHERE kind = 'fact' AND package_id = 'pkg:npm/scanner-capture'
  AND repository_id = 'repo://scanner-capture/api' AND c0 = $1`, cve).Scan(&writer, &scanner); err != nil {
			t.Fatalf("read staged marks for %s: %v", cve, err)
		}
		return writer, scanner
	}

	// Violation: the insert triggers are disabled, so only the writer's own
	// marks stage and no scanner mark lands.
	mustExecProofRow(t, ctx, admin, `ALTER TABLE scanner_image_result_fact_classifications DISABLE TRIGGER scanner_classifications_winners_dirty_insert`)
	mustExecProofRow(t, ctx, admin, `ALTER TABLE scanner_image_result_fact_associations DISABLE TRIGGER scanner_associations_winners_dirty_insert`)
	seedScope("scanner-capture-red", "scanner-capture-red/g1")
	publish("scanner-capture-red", "scanner-capture-red/g1", "CVE-2026-9805-1")
	if writer, scanner := stagedMarksOf("CVE-2026-9805-1"); writer < 1 || scanner != 0 {
		t.Fatalf("with the scanner triggers disabled the publish staged writer=%d scanner=%d, want writer>=1 scanner=0 (writer marks only)", writer, scanner)
	}

	// Shipped triggers: the same publish stages the writer's marks plus the
	// scanner trigger's marks of the same key.
	mustExecProofRow(t, ctx, admin, `ALTER TABLE scanner_image_result_fact_classifications ENABLE TRIGGER scanner_classifications_winners_dirty_insert`)
	mustExecProofRow(t, ctx, admin, `ALTER TABLE scanner_image_result_fact_associations ENABLE TRIGGER scanner_associations_winners_dirty_insert`)
	seedScope("scanner-capture-green", "scanner-capture-green/g1")
	publish("scanner-capture-green", "scanner-capture-green/g1", "CVE-2026-9805-2")
	if writer, scanner := stagedMarksOf("CVE-2026-9805-2"); writer < 1 || scanner < 1 {
		t.Fatalf("with the scanner triggers enabled the publish staged writer=%d scanner=%d, want writer>=1 scanner>=1 (writer marks plus scanner marks)", writer, scanner)
	}
}
