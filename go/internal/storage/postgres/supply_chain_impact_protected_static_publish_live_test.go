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

// TestSupplyChainImpactProtectedStaticPublishLive drives the production
// writer path end to end as a contracted runtime-writer: a complete
// protected static write must commit its finding fact and its authority
// revision row. Before the publisher exists this fails with "protected scan
// impact publication transaction is required" (the golden-corpus drain
// dead-letter behind the same error); after, the write commits and the
// revision pins the fence, payload hash, role revision, and identity.
func TestSupplyChainImpactProtectedStaticPublishLive(t *testing.T) {
	adminDSN := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	optIn := os.Getenv("ESHU_POSTGRES_DISPOSABLE")
	if adminDSN == "" {
		t.Skip("set ESHU_POSTGRES_DSN and ESHU_POSTGRES_DISPOSABLE=1 to run the protected publish proof")
	}
	ctx, admin := postgresproof.OpenDisposableBootstrapDatabase(t, adminDSN, optIn, 5*time.Minute,
		func(ctx context.Context, database *sql.DB) error {
			return ApplyBootstrap(ctx, SQLDB{DB: database})
		})

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
	for _, statement := range []string{
		`GRANT CONNECT ON DATABASE "` + dbName + `" TO "` + roleName + `"`,
		// Ordinary fact and admission DML the writer path issues outside
		// any DEFINER body. Base-table privileges are a DBA pregrant outside
		// the migration set (243a); the proof grants them explicitly.
		`GRANT SELECT, INSERT, UPDATE ON supply_chain_impact_write_admission TO "` + roleName + `"`,
		`GRANT SELECT, INSERT, UPDATE ON fact_records TO "` + roleName + `"`,
		// The change set behind the holder fan-out (#9695) reads scope_generations
		// inside the write transaction to find the previous generation. The
		// runtime-writer login already needs this SELECT on main, for every
		// reducer claim (storage/postgres/reducer_generation_filter_sql.go:121-123)
		// and for the impact load (storage/postgres/facts/impact/branches.go:82).
		// It is an ordinary base table, not a protected object; the inventory
		// above is unchanged.
		`GRANT SELECT ON scope_generations TO "` + roleName + `"`,
	} {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("grant proof privilege: %v", err)
		}
	}
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
	var factCount int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM fact_records
		WHERE scope_id=$1 AND generation_id=$2 AND NOT is_tombstone`, scopeID, generationID).Scan(&factCount); err != nil {
		t.Fatal(err)
	}
	if factCount != 1 {
		t.Fatalf("live impact facts = %d, want the one committed finding", factCount)
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

	// A revoked writer authority fail-closes the next pass: with the
	// worker contract disabled, authorization refuses before any fact write.
	mustExecProofRow(t, ctx, admin, `UPDATE eshu_security.role_contracts SET enabled=FALSE`)
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, write); err == nil {
		t.Fatal("WriteSupplyChainImpactFindings() after contract revoke = nil, want the authority refusal")
	} else if !strings.Contains(err.Error(), "principal denied") {
		t.Fatalf("WriteSupplyChainImpactFindings() after contract revoke = %v, want the principal refusal", err)
	}
}

func mustExecProofRow(t *testing.T, ctx context.Context, admin *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := admin.ExecContext(ctx, query, args...); err != nil {
		t.Fatal(err)
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
