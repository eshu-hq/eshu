// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10820 disposable PostgreSQL 18 proof that the census 2e tables are purged with the repository: per-table deletion with a neighbor repository untouched, ledger counts, trigger re-creation, service snapshot cascade, and writer lock-order probes
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
)

// censusLiveCheck counts one census table for a partition. $1 is the partition
// (repo id) or, for a query that names ANY($1) or unnest($1, the scope ids.
// Every count matches the seeded ids exactly, never by prefix: the neighbor
// partition and scopes extend the target's by a suffix, so a LIKE prefix would
// count the neighbor's rows as the target's. Relationship and service rows
// carry no scope column, so their seeded ids (scope id plus a fixed suffix) are
// the only link.
type censusLiveCheck struct {
	table string
	query string
	// ledger is the expected tombstone ledger count for the target partition.
	ledger int64
}

var censusLiveChecks = []censusLiveCheck{
	{"reducer_readiness_waits", `SELECT count(*) FROM reducer_readiness_waits WHERE scope_id = ANY($1)`, 2},
	{"projection_decisions", `SELECT count(*) FROM projection_decisions WHERE repository_id = $1`, 2},
	{"projection_decision_evidence", `SELECT count(*) FROM projection_decision_evidence WHERE decision_id IN ($1 || '-d1', $1 || '-d2')`, 2},
	{"crossplane_satisfied_by_redrive_state", `SELECT count(*) FROM crossplane_satisfied_by_redrive_state WHERE xrd_scope_id = ANY($1)`, 2},
	{"crossplane_satisfied_by_redrive_target_ledger", `SELECT count(*) FROM crossplane_satisfied_by_redrive_target_ledger WHERE target_scope_id = ANY($1)`, 2},
	{"relationship_generations", `SELECT count(*) FROM relationship_generations WHERE scope = ANY($1)`, 2},
	{"relationship_evidence_facts", `SELECT count(*) FROM relationship_evidence_facts WHERE evidence_id IN (SELECT s || '-ev' || n FROM unnest($1::text[]) s, unnest(ARRAY['1','2']) n)`, 4},
	{"relationship_candidates", `SELECT count(*) FROM relationship_candidates WHERE candidate_id IN (SELECT s || '-cand' FROM unnest($1::text[]) s)`, 2},
	{"service_materialization_generations", `SELECT count(*) FROM service_materialization_generations WHERE scope_id = ANY($1)`, 2},
	{"service_evidence_snapshots", `SELECT count(*) FROM service_evidence_snapshots WHERE generation_id IN (SELECT s || '-svc' FROM unnest($1::text[]) s)`, 4},
	{"config_artifact_paths", `SELECT count(*) FROM config_artifact_paths WHERE repo_id = $1`, 1},
	{"content_file_ref_deltas", `SELECT count(*) FROM content_file_ref_deltas WHERE repo_id = $1`, 1},
	{"content_file_ref_delta_bases", `SELECT count(*) FROM content_file_ref_delta_bases WHERE repo_id = $1`, 1},
	{"code_function_fingerprint", `SELECT count(*) FROM code_function_fingerprint WHERE repo_id = $1`, 1},
	{"code_fingerprint_band", `SELECT count(*) FROM code_fingerprint_band WHERE repo_id = $1`, 1},
	{"function_summaries", `SELECT count(*) FROM function_summaries WHERE repo = $1`, 1},
	{"function_sources", `SELECT count(*) FROM function_sources WHERE repo = $1`, 1},
	{"function_graph_ids", `SELECT count(*) FROM function_graph_ids WHERE repo = $1`, 1},
	{"infra_resource_entities", `SELECT count(*) FROM infra_resource_entities WHERE repo_id = $1`, 1},
	{"infra_resource_entity_dirty_repos", `SELECT count(*) FROM infra_resource_entity_dirty_repos WHERE repo_id = $1`, 1},
	{"package_manifest_consumption_key_dirty_scopes", `SELECT count(*) FROM package_manifest_consumption_key_dirty_scopes WHERE scope_id = ANY($1)`, 1},
}

// censusCount runs a census check for one partition and its scope ids.
func censusCount(t *testing.T, ctx context.Context, database *sql.DB, check censusLiveCheck, partition string, scopeIDs []string) int64 {
	t.Helper()
	if strings.Contains(check.query, "ANY($1)") || strings.Contains(check.query, "unnest($1") {
		return liveCount(t, ctx, database, check.query, scopeIDs)
	}
	return liveCount(t, ctx, database, check.query, partition)
}

// TestRetirementPurgeCensus2eTablesLive proves each #10820 table against real
// PostgreSQL: the retired repository's rows are gone (including the rows its
// own delete triggers re-create), a neighbor repository's rows are untouched,
// and the tombstone ledger carries each count.
func TestRetirementPurgeCensus2eTablesLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_census",
		"set ESHU_POSTGRES_DSN to run the census 2e purge Postgres proof")
	partition := "PARTITION:purge-live"
	scopeIDs := seedPurgeLiveScope(t, ctx, database, partition)
	seedCensus2eRows(t, ctx, database, partition, scopeIDs)
	sentinelPartition, sentinelScopes := seedCensusNeighbor(t, ctx, database)

	sentinelBefore := make(map[string]int64)
	for _, check := range censusLiveChecks {
		before := censusCount(t, ctx, database, check, partition, scopeIDs)
		if before == 0 {
			t.Fatalf("%s: seeded no rows for the target partition", check.table)
		}
		sentinelBefore[check.table] = censusCount(t, ctx, database, check, sentinelPartition, sentinelScopes)
		if sentinelBefore[check.table] == 0 {
			t.Fatalf("%s: seeded no rows for the sentinel partition", check.table)
		}
	}

	store := NewRetirementStore(SQLDB{DB: database})
	if _, err := store.InsertPending(ctx, RetirementRequest{
		RetirementID: "rr_census_live", RepoID: partition, ScopeID: scopeIDs[0],
		ReasonCode: RetirementReasonOperatorRetired, ActorClass: "operator",
		IdempotencyKeyHash: "census-live-key",
	}); err != nil {
		t.Fatalf("InsertPending: %v", err)
	}
	// The isolated pool is derive-aware (eshu.infra_inventory_writer = 'derive'),
	// which silences the content_entities delete trigger. Pin one connection
	// and clear the marker so the purge runs as a session that is not
	// derive-aware (the API/MCP writers), the case the delete order protects.
	database.SetMaxOpenConns(1)
	if _, err := database.ExecContext(ctx, `SET eshu.infra_inventory_writer = 'api'`); err != nil {
		t.Fatalf("clear derive marker: %v", err)
	}
	purger := NewRetirementPurger(SQLDB{DB: database}, SQLDB{DB: database})
	result, err := purger.PurgeRetired(ctx, PurgeRequest{RepoIDs: []string{partition}})
	if err != nil {
		t.Fatalf("PurgeRetired: %v", err)
	}
	if len(result.Completed) != 1 || len(result.Blocked) != 0 {
		t.Fatalf("result = %+v, want one completion and no blocks", result)
	}

	var ledgerJSON string
	if err := database.QueryRowContext(ctx,
		`SELECT rows_deleted::text FROM repository_retirements WHERE retirement_id = 'rr_census_live'`).Scan(&ledgerJSON); err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	var ledger map[string]int64
	if err := json.Unmarshal([]byte(ledgerJSON), &ledger); err != nil {
		t.Fatalf("ledger decode: %v", err)
	}
	for _, check := range censusLiveChecks {
		if got := censusCount(t, ctx, database, check, partition, scopeIDs); got != 0 {
			t.Errorf("%s: %d rows survive the purge", check.table, got)
		}
		if got := censusCount(t, ctx, database, check, sentinelPartition, sentinelScopes); got != sentinelBefore[check.table] {
			t.Errorf("%s: sentinel rows = %d, want %d untouched", check.table, got, sentinelBefore[check.table])
		}
		if ledger[check.table] != check.ledger {
			t.Errorf("ledger[%s] = %d, want %d (full ledger %v)", check.table, ledger[check.table], check.ledger, ledger)
		}
	}
}

// TestRetirementCensusCascadeChildrenAreForeignKeyed proves, against the
// migrated schema, that each table the guard excuses as a cascade child has a
// foreign key to its named parent with ON DELETE CASCADE.
func TestRetirementCensusCascadeChildrenAreForeignKeyed(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_census_fk",
		"set ESHU_POSTGRES_DSN to run the census cascade foreign-key proof")
	for child, parent := range censusCascadeChildren {
		var count int64
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM pg_constraint
			WHERE contype = 'f' AND confdeltype = 'c'
			  AND conrelid = $1::regclass AND confrelid = $2::regclass`, child, parent).Scan(&count); err != nil {
			t.Fatalf("%s: %v", child, err)
		}
		if count == 0 {
			t.Errorf("%s has no ON DELETE CASCADE foreign key to %s; the guard cannot excuse it", child, parent)
		}
	}
}

// TestRetirementConfigArtifactVersionRowIsRecreatedAtCommitLive pins the trigger
// behavior behind the census `kept` ruling for config_artifact_content_versions
// and config_artifact_repo_state (#10820). The purge deletes neither row: the
// content_files delete stages the repository and the content_files trigger
// (migration 186) bumps or re-creates the version row through a constraint
// trigger that fires at COMMIT, after every statement of the transaction, so even
// a transaction that deletes the state and version rows commits with a fresh
// version-1 row and no state row. That is why a purge delete cannot stick, and
// why the kept rows carry the monotonic fence counter a late derive write and a
// readmission depend on. The deletion here is a probe of the trigger only; the
// purge never issues it.
func TestRetirementConfigArtifactVersionRowIsRecreatedAtCommitLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_cfg_commit",
		"set ESHU_POSTGRES_DSN to run the config-artifact commit-trigger proof")
	repo := "PARTITION:cfg-commit"
	if _, err := database.ExecContext(ctx, `INSERT INTO content_files
		(repo_id, relative_path, content, content_hash, line_count, indexed_at)
		VALUES ($1, 'a.yaml', 'k: v', 'h', 1, NOW())`, repo); err != nil {
		t.Fatalf("seed content file: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO config_artifact_repo_state (repo_id, extractor_version)
		VALUES ($1, 1) ON CONFLICT (repo_id) DO NOTHING`, repo); err != nil {
		t.Fatalf("seed repo state: %v", err)
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM content_files WHERE repo_id = $1`, repo); err != nil {
		t.Fatalf("delete content files: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `WITH state AS (DELETE FROM config_artifact_repo_state WHERE repo_id = $1)
		DELETE FROM config_artifact_content_versions WHERE repo_id = $1`, repo); err != nil {
		t.Fatalf("delete state and version: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if got := liveCount(t, ctx, database, `SELECT count(*) FROM config_artifact_content_versions WHERE repo_id = $1`, repo); got != 1 {
		t.Fatalf("version rows after commit = %d, want 1: the commit trigger's behavior changed. "+
			"This reopens the #10820 ruling that keeps the config-artifact version and state rows "+
			"(a late derive write and a readmission depend on the counter); it does not license a purge delete", got)
	}
	if got := liveCount(t, ctx, database, `SELECT count(*) FROM config_artifact_repo_state WHERE repo_id = $1`, repo); got != 0 {
		t.Fatalf("state rows after commit = %d, want 0", got)
	}
}
