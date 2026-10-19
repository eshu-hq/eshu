// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
)

// seedPurgeLiveScope writes one fenced repository partition: two scopes with
// superseded and failed generations, a superseded work row per generation,
// repo-keyed content rows, refs, scope-keyed reindex and observation rows,
// one shared intent, one unroutable intent, and one dead letter per scope,
// and one grant row in each of the four scope-FK grant tables. It returns
// the partition key and scope ids.
func seedPurgeLiveScope(t *testing.T, ctx context.Context, database *sql.DB, partition string) []string {
	t.Helper()
	scopeIDs := []string{"purge-live-main", "purge-live-ref"}
	for _, scopeID := range scopeIDs {
		if _, err := database.ExecContext(ctx, `INSERT INTO ingestion_scopes
			(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
			 observed_at, ingested_at, status, active_generation_id, payload)
			VALUES ($1, 'repository', 'git', $2, 'git_collector', $3, NOW(), NOW(), 'active', NULL, '{}'::jsonb)`,
			scopeID, "key-"+scopeID, partition); err != nil {
			t.Fatalf("seed scope %s: %v", scopeID, err)
		}
		for _, gen := range []struct {
			id, status string
		}{{scopeID + "-gen-old", "superseded"}, {scopeID + "-gen-failed", "failed"}} {
			if _, err := database.ExecContext(ctx, `INSERT INTO scope_generations
				(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
				VALUES ($1, $2, 'push', NOW(), NOW(), $3, '{}'::jsonb)`, gen.id, scopeID, gen.status); err != nil {
				t.Fatalf("seed generation %s: %v", gen.id, err)
			}
			if _, err := database.ExecContext(ctx, `INSERT INTO fact_work_items
				(work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
				VALUES ($1, $2, $3, 'reducer', 'd1', 'superseded', NOW(), NOW())`,
				"purge-live-w-"+gen.id, scopeID, gen.id); err != nil {
				t.Fatalf("seed work row %s: %v", gen.id, err)
			}
		}
		if _, err := database.ExecContext(ctx, `INSERT INTO repository_reindex_requests (scope_id, requested_at)
			VALUES ($1, NOW())`, scopeID); err != nil {
			t.Fatalf("seed reindex request %s: %v", scopeID, err)
		}
		if _, err := database.ExecContext(ctx, `INSERT INTO repository_selection_observations
			(scope_id, selector_id, selector_kind, selector_owner, state,
			 state_since, state_cycle_count, evaluated_at, liveness_window_seconds)
			VALUES ($1, 'sel-1', 'explicit', 'owner', 'selected', NOW(), 1, NOW(), 60)`, scopeID); err != nil {
			t.Fatalf("seed selection observation %s: %v", scopeID, err)
		}
		if _, err := database.ExecContext(ctx, `INSERT INTO shared_projection_intents
			(intent_id, projection_domain, partition_key, scope_id, repository_id,
			 source_run_id, generation_id, payload, created_at)
			VALUES ($1, 'd1', $2, $3, $2, 'run-1', $4, '{}'::jsonb, NOW())`,
			"purge-live-intent-"+scopeID, partition, scopeID, scopeID+"-gen-old"); err != nil {
			t.Fatalf("seed shared intent %s: %v", scopeID, err)
		}
		if _, err := database.ExecContext(ctx, `INSERT INTO shared_projection_unroutable_intents
			(intent_id, projection_domain, partition_key, repository_id, scope_id,
			 generation_id, evidence_source, reason, decided_at)
			VALUES ($1, 'd1', $2, $2, $3, $4, 'test', 'no-route', NOW())`,
			"purge-live-unroutable-"+scopeID, partition, scopeID, scopeID+"-gen-old"); err != nil {
			t.Fatalf("seed unroutable intent %s: %v", scopeID, err)
		}
		if _, err := database.ExecContext(ctx, `INSERT INTO collector_generation_dead_letters
			(generation_id, scope_id, collector_kind, source_system, scope_kind,
			 partition_key, trigger_kind, failure_class, failure_message, status,
			 first_dead_lettered_at, last_dead_lettered_at, updated_at)
			VALUES ($1, $2, 'git_collector', 'git', 'repository', $3, 'push',
			 'test', 'seeded', 'pending', NOW(), NOW(), NOW())`,
			scopeID+"-gen-old", scopeID, partition); err != nil {
			t.Fatalf("seed dead letter %s: %v", scopeID, err)
		}
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO content_files
		(repo_id, relative_path, content, content_hash, line_count, indexed_at)
		VALUES ($1, 'main.go', 'package main', 'hash-1', 1, NOW())`, partition); err != nil {
		t.Fatalf("seed content file: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO content_entities
		(entity_id, repo_id, relative_path, entity_type, entity_name,
		 start_line, end_line, source_cache, indexed_at)
		VALUES ('purge-live-e1', $1, 'main.go', 'func', 'main', 1, 1, '', NOW())`, partition); err != nil {
		t.Fatalf("seed content entity: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO content_file_references
		(repo_id, relative_path, reference_kind, reference_value, indexed_at)
		VALUES ($1, 'main.go', 'import', 'fmt', NOW())`, partition); err != nil {
		t.Fatalf("seed content reference: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO repository_refs
		(repo_id, ref_kind, name, head_sha, observed_at, indexed_at)
		VALUES ($1, 'branch', 'main', 'sha-1', NOW(), NOW())`, partition); err != nil {
		t.Fatalf("seed repository ref: %v", err)
	}
	seedPurgeLiveGrants(t, ctx, database, partition, scopeIDs[0])
	return scopeIDs
}

// seedPurgeLiveGrants writes one row in each of the four scope-FK grant
// tables for the repository, with the tenant/workspace/role parents they
// reference. The grants cascade with the scope delete; the parents stay.
func seedPurgeLiveGrants(t *testing.T, ctx context.Context, database *sql.DB, partition, scopeID string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, `INSERT INTO tenants
		(tenant_id, status, policy_revision_hash, created_at, updated_at)
		VALUES ('purge-live-t', 'active', 'prh', NOW(), NOW())`); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO workspaces
		(tenant_id, workspace_id, status, policy_revision_hash, created_at, updated_at)
		VALUES ('purge-live-t', 'purge-live-w', 'active', 'prh', NOW(), NOW())`); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO tenant_scope_grants
		(tenant_id, workspace_id, scope_id, subject_class, grant_source,
		 policy_revision_hash, effective_at, created_at, updated_at)
		VALUES ('purge-live-t', 'purge-live-w', $1, 'user', 'test', 'prh', NOW(), NOW(), NOW())`, scopeID); err != nil {
		t.Fatalf("seed scope grant: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO tenant_repository_grants
		(tenant_id, workspace_id, repo_id, scope_id, subject_class, grant_source,
		 policy_revision_hash, effective_at, created_at, updated_at)
		VALUES ('purge-live-t', 'purge-live-w', $1, $2, 'user', 'test', 'prh', NOW(), NOW(), NOW())`,
		partition, scopeID); err != nil {
		t.Fatalf("seed repository grant: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO identity_roles
		(tenant_id, role_id, role_key_hash, status, policy_revision_hash, created_at, updated_at)
		VALUES ('purge-live-t', 'purge-live-r', 'rkh', 'active', 'prh', NOW(), NOW())`); err != nil {
		t.Fatalf("seed role: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO identity_role_scope_targets
		(tenant_id, workspace_id, role_id, scope_id, status, grant_source,
		 policy_revision_hash, effective_at, created_at, updated_at)
		VALUES ('purge-live-t', 'purge-live-w', 'purge-live-r', $1, 'active', 'test', 'prh', NOW(), NOW(), NOW())`,
		scopeID); err != nil {
		t.Fatalf("seed role scope target: %v", err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO identity_role_repository_targets
		(tenant_id, workspace_id, role_id, repo_id, scope_id, status, grant_source,
		 policy_revision_hash, effective_at, created_at, updated_at)
		VALUES ('purge-live-t', 'purge-live-w', 'purge-live-r', $1, $2, 'active', 'test', 'prh', NOW(), NOW(), NOW())`,
		partition, scopeID); err != nil {
		t.Fatalf("seed role repository target: %v", err)
	}
}

// liveCount counts rows matching the predicate.
func liveCount(t *testing.T, ctx context.Context, database *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var count int64
	if err := database.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		t.Fatalf("live count %q: %v", query, err)
	}
	return count
}

// TestRetirementPurgeLive proves the narrowed purge against real Postgres:
// the keyed leftovers go by explicit delete, the scope rows go by one
// delete with generations, work rows, and grants cascading, and the
// tombstone reaches complete/done with the deleted-row ledger reporting
// the cascade the tombstone did not count itself.
func TestRetirementPurgeLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_purge",
		"set ESHU_POSTGRES_DSN to run the retirement purge Postgres proof")
	partition := "PARTITION:purge-live"
	scopeIDs := seedPurgeLiveScope(t, ctx, database, partition)

	store := NewRetirementStore(SQLDB{DB: database})
	marker, err := store.InsertPending(ctx, RetirementRequest{
		RetirementID: "rr_purge_live", RepoID: partition, ScopeID: scopeIDs[0],
		ReasonCode: RetirementReasonOperatorRetired, ActorClass: "operator",
		IdempotencyKeyHash: "purge-live-key",
	})
	if err != nil {
		t.Fatalf("InsertPending: %v", err)
	}
	if marker.Phase != RetirementPhaseFenced {
		t.Fatalf("marker phase = %q, want fenced", marker.Phase)
	}

	purger := NewRetirementPurger(SQLDB{DB: database}, SQLDB{DB: database})
	result, err := purger.PurgeRetired(ctx, PurgeRequest{RepoIDs: []string{partition}})
	if err != nil {
		t.Fatalf("PurgeRetired: %v", err)
	}
	if len(result.Completed) != 1 || len(result.Blocked) != 0 {
		t.Fatalf("result = %+v, want one completion and no blocks", result)
	}

	for _, check := range []struct {
		table, query string
	}{
		{"ingestion_scopes", `SELECT count(*) FROM ingestion_scopes WHERE partition_key = $1`},
		{"scope_generations", `SELECT count(*) FROM scope_generations WHERE scope_id = ANY($1)`},
		{"fact_work_items", `SELECT count(*) FROM fact_work_items WHERE scope_id = ANY($1)`},
		{"content_files", `SELECT count(*) FROM content_files WHERE repo_id = $1`},
		{"content_entities", `SELECT count(*) FROM content_entities WHERE repo_id = $1`},
		{"content_file_references", `SELECT count(*) FROM content_file_references WHERE repo_id = $1`},
		{"repository_refs", `SELECT count(*) FROM repository_refs WHERE repo_id = $1`},
		{"repository_reindex_requests", `SELECT count(*) FROM repository_reindex_requests WHERE scope_id = ANY($1)`},
		{"repository_selection_observations", `SELECT count(*) FROM repository_selection_observations WHERE scope_id = ANY($1)`},
		{"shared_projection_intents", `SELECT count(*) FROM shared_projection_intents WHERE scope_id = ANY($1)`},
		{"shared_projection_unroutable_intents", `SELECT count(*) FROM shared_projection_unroutable_intents WHERE scope_id = ANY($1)`},
		{"collector_generation_dead_letters", `SELECT count(*) FROM collector_generation_dead_letters WHERE scope_id = ANY($1)`},
		{"tenant_scope_grants", `SELECT count(*) FROM tenant_scope_grants WHERE scope_id = ANY($1)`},
		{"tenant_repository_grants", `SELECT count(*) FROM tenant_repository_grants WHERE scope_id = ANY($1)`},
		{"identity_role_scope_targets", `SELECT count(*) FROM identity_role_scope_targets WHERE scope_id = ANY($1)`},
		{"identity_role_repository_targets", `SELECT count(*) FROM identity_role_repository_targets WHERE scope_id = ANY($1)`},
	} {
		arg := any(partition)
		if len(check.query) > 0 && (check.table == "scope_generations" || check.table == "fact_work_items" ||
			check.table == "repository_reindex_requests" || check.table == "repository_selection_observations" ||
			check.table == "shared_projection_intents" || check.table == "shared_projection_unroutable_intents" ||
			check.table == "collector_generation_dead_letters" ||
			check.table == "tenant_scope_grants" || check.table == "tenant_repository_grants" ||
			check.table == "identity_role_scope_targets" || check.table == "identity_role_repository_targets") {
			arg = scopeIDs
		}
		if count := liveCount(t, ctx, database, check.query, arg); count != 0 {
			t.Fatalf("%s rows = %d, want 0 after purge", check.table, count)
		}
	}
	// The grant parents outlive the cascade: the purge drops access, never
	// the tenant, workspace, or role rows.
	if count := liveCount(t, ctx, database, `SELECT count(*) FROM tenants WHERE tenant_id = 'purge-live-t'`); count != 1 {
		t.Fatalf("tenants rows = %d, want 1 (parents survive the cascade)", count)
	}

	var state, phase string
	var retiredAt sql.NullTime
	var ledgerJSON string
	if err := database.QueryRowContext(ctx, `SELECT state, phase, retired_at, rows_deleted::text
		FROM repository_retirements WHERE retirement_id = 'rr_purge_live'`).Scan(&state, &phase, &retiredAt, &ledgerJSON); err != nil {
		t.Fatalf("read tombstone: %v", err)
	}
	if state != RetirementStateComplete || phase != RetirementPhaseDone {
		t.Fatalf("tombstone = %s/%s, want complete/done", state, phase)
	}
	if !retiredAt.Valid {
		t.Fatal("retired_at is NULL, want the purge timestamp")
	}
	var ledger map[string]int64
	if err := json.Unmarshal([]byte(ledgerJSON), &ledger); err != nil {
		t.Fatalf("ledger decode: %v", err)
	}
	for table, want := range map[string]int64{
		"ingestion_scopes": 2, "content_files": 1, "content_entities": 1,
		"content_file_references": 1, "repository_refs": 1,
		"repository_reindex_requests": 2, "repository_selection_observations": 2,
		"shared_projection_intents": 2, "shared_projection_unroutable_intents": 2,
		"collector_generation_dead_letters": 2,
		"tenant_scope_grants":               1, "tenant_repository_grants": 1,
		"identity_role_scope_targets": 1, "identity_role_repository_targets": 1,
	} {
		if ledger[table] != want {
			t.Fatalf("ledger[%s] = %d, want %d (full ledger %v)", table, ledger[table], want, ledger)
		}
	}
}

// TestRetirementPurgeTerminalNotReachedLive proves the terminal gate against
// real Postgres: a scope with a still-active generation keeps every row,
// the tombstone stays pending/fenced, and the exit names the live status.
func TestRetirementPurgeTerminalNotReachedLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "retire_purge_gate",
		"set ESHU_POSTGRES_DSN to run the retirement purge gate Postgres proof")
	partition := "PARTITION:purge-gated"
	if _, err := database.ExecContext(ctx, `INSERT INTO ingestion_scopes
		(scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
		 observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ('purge-gated-main', 'repository', 'git', 'key-gated', 'git_collector', $1,
		 NOW(), NOW(), 'active', 'purge-gated-gen-live', '{}'::jsonb)`, partition); err != nil {
		t.Fatalf("seed scope: %v", err)
	}
	for _, gen := range []struct {
		id, status string
	}{
		{"purge-gated-gen-live", "active"},
		{"purge-gated-gen-old", "superseded"},
	} {
		if _, err := database.ExecContext(ctx, `INSERT INTO scope_generations
			(generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
			VALUES ($1, 'purge-gated-main', 'push', NOW(), NOW(), $2, '{}'::jsonb)`, gen.id, gen.status); err != nil {
			t.Fatalf("seed generation %s: %v", gen.id, err)
		}
	}

	store := NewRetirementStore(SQLDB{DB: database})
	if _, err := store.InsertPending(ctx, RetirementRequest{
		RetirementID: "rr_purge_gated", RepoID: partition, ScopeID: "purge-gated-main",
		ReasonCode: RetirementReasonOperatorRetired, ActorClass: "operator",
		IdempotencyKeyHash: "purge-gated-key",
	}); err != nil {
		t.Fatalf("InsertPending: %v", err)
	}

	purger := NewRetirementPurger(SQLDB{DB: database}, SQLDB{DB: database})
	result, err := purger.PurgeRetired(ctx, PurgeRequest{RepoIDs: []string{partition}})
	if err != nil {
		t.Fatalf("PurgeRetired: %v", err)
	}
	if len(result.Completed) != 0 || len(result.Blocked) != 1 {
		t.Fatalf("result = %+v, want one block and no completion", result)
	}
	blocked := result.Blocked[0]
	if blocked.Reason != PurgeReasonTerminalNotReached {
		t.Fatalf("reason = %q, want %q", blocked.Reason, PurgeReasonTerminalNotReached)
	}
	if blocked.Statuses != "active:1" {
		t.Fatalf("statuses = %q, want the live readback", blocked.Statuses)
	}
	if count := liveCount(t, ctx, database,
		`SELECT count(*) FROM ingestion_scopes WHERE partition_key = $1`, partition); count != 1 {
		t.Fatalf("scope rows = %d, want 1 (the gate writes nothing)", count)
	}
	open, err := store.GetOpen(ctx, partition)
	if err != nil {
		t.Fatalf("GetOpen: %v", err)
	}
	if open == nil || open.State != RetirementStatePending || open.Phase != RetirementPhaseFenced {
		t.Fatalf("tombstone = %+v, want the open pending/fenced row", open)
	}
}
