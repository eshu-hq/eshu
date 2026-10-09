// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// generationRetentionMigratedSchemaRequiredEnv turns a missing DSN into a
// failure. CI lanes that enroll these proofs set it to "1".
const generationRetentionMigratedSchemaRequiredEnv = "ESHU_REQUIRE_RETENTION_MIGRATED_SCHEMA_PROOF"

// generationRetentionMigratedSchemaStatements lists every SQL statement owned
// by generation_retention_sql.go and generation_retention_row_counts_sql.go.
// Each one is prepared against the migrated schema so a table or column that
// drifts from the migrations fails here instead of at the first prunable
// retention batch (#6809).
var generationRetentionMigratedSchemaStatements = map[string]string{
	"candidate":                        generationRetentionCandidateQuery,
	"row_counts":                       generationRetentionRowCountsQuery,
	"insert_event":                     insertGenerationRetentionEventQuery,
	"delete_shared_projection_intents": deleteSharedProjectionIntentsForGenerationsQuery,
	"delete_unroutable_intents":        deleteSharedProjectionUnroutableIntentsForGenerationsQuery,
	"prune_content_file_references":    pruneContentFileReferencesForGenerationsQuery,
	"prune_content_entities":           pruneContentEntitiesForGenerationsQuery,
	"prune_content_files":              pruneContentFilesForGenerationsQuery,
	"delete_scope_generations":         deleteScopeGenerationsForRetentionQuery,
}

// generationRetentionSchemaPrefix marks every schema the retention live tests
// create, so a leaked one is recognizable in pg_namespace.
const generationRetentionSchemaPrefix = "eshu_ret_"

// generationRetentionSchemaMaxBytes is Postgres's identifier limit (NAMEDATALEN
// 64 minus the terminating NUL); a longer name is silently truncated by the
// server, which would make two long test names share one schema.
const generationRetentionSchemaMaxBytes = 63

// generationRetentionMigratedSchemaName derives the isolated schema name for one
// test from its name, so concurrent tests never DROP each other's schema (#7260).
// It lowercases the name and replaces every byte outside [a-z0-9_] (including
// the "/" of a subtest) with "_". When the result would exceed the 63-byte
// identifier limit it truncates and appends "_" plus the first 8 hex digits of
// the SHA-256 of the original name, so two long names that share a prefix stay
// distinct. The result is deterministic and always a valid unquoted identifier.
func generationRetentionMigratedSchemaName(testName string) string {
	var sanitized strings.Builder
	for _, b := range []byte(strings.ToLower(testName)) {
		if (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_' {
			sanitized.WriteByte(b)
			continue
		}
		sanitized.WriteByte('_')
	}
	name := generationRetentionSchemaPrefix + sanitized.String()
	if len(name) <= generationRetentionSchemaMaxBytes {
		return name
	}
	digest := sha256.Sum256([]byte(testName))
	suffix := "_" + hex.EncodeToString(digest[:])[:8]
	return name[:generationRetentionSchemaMaxBytes-len(suffix)] + suffix
}

// installGenerationRetentionTrigramExtension installs pg_trgm into public, the
// same pin the sibling live helpers use. The bootstrap's own CREATE EXTENSION
// would otherwise land in the first schema on the search_path and vanish with
// that schema's DROP, breaking a concurrently bootstrapped sibling with
// "operator class gin_trgm_ops does not exist". IF NOT EXISTS alone does not
// make the first creation safe: two sessions can both pass the existence check
// and one fails with a pg_extension_name_index unique violation. The
// transaction-scoped advisory lock and the CREATE share one session, so the
// installs run one at a time and the lock releases at COMMIT.
func installGenerationRetentionTrigramExtension(ctx context.Context, t *testing.T, admin *sql.DB) {
	t.Helper()
	postgresproof.InstallTrigramExtension(ctx, t, admin)
}

// openGenerationRetentionMigratedSchema applies the real bootstrap migrations
// to an isolated schema of the database named by ESHU_POSTGRES_TEST_DSN or
// ESHU_POSTGRES_DSN, or skips. It never uses a hand-written schema, so the retention SQL is proven
// against the tables and columns production actually has.
//
// The retention tests that share this opener are safe under t.Parallel with
// each other (#7260): each test gets its own schema (see
// generationRetentionMigratedSchemaName), so two of them never drop each
// other's schema, and pg_trgm, which is database-wide, is installed once into
// public under an advisory lock (see installGenerationRetentionTrigramExtension)
// instead of into a private schema that a sibling's DROP would take with it.
// The lock serializes only this opener's installs: other live helpers in this
// package install pg_trgm without it, which is safe only while none of them
// runs under t.Parallel. Before parallelizing one of those, hoist this lock
// into a shared installer that every live pg_trgm install takes.
func openGenerationRetentionMigratedSchema(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	}
	if dsn == "" {
		// The reducer contention gate sets the require flag so a renamed DSN
		// variable fails the lane instead of skipping the proof there.
		if os.Getenv(generationRetentionMigratedSchemaRequiredEnv) == "1" {
			t.Fatalf("%s=1 but neither ESHU_POSTGRES_TEST_DSN nor ESHU_POSTGRES_DSN is set", generationRetentionMigratedSchemaRequiredEnv)
		}
		t.Skip("set ESHU_POSTGRES_TEST_DSN or ESHU_POSTGRES_DSN to run the migrated-schema retention proof")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	admin.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = admin.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	installGenerationRetentionTrigramExtension(ctx, t, admin)
	schema := generationRetentionMigratedSchemaName(t.Name())
	t.Logf("isolated retention schema %s", schema)
	if _, err := admin.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+quoteSQLIdentifier(schema)+" CASCADE; CREATE SCHEMA "+quoteSQLIdentifier(schema)); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+quoteSQLIdentifier(schema)+" CASCADE"); err != nil {
			t.Errorf("drop isolated schema: %v", err)
		}
	})

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse Postgres DSN: %v", err)
	}
	query := parsed.Query()
	// public stays on the path so a pg_trgm extension a sibling live test
	// installed there still resolves its operator classes; every table the
	// bootstrap creates lands in the isolated schema, which comes first.
	query.Set("search_path", schema+",public")
	parsed.RawQuery = query.Encode()
	database, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatalf("open isolated schema: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("apply bootstrap migrations: %v", err)
	}
	return database, ctx
}

// TestGenerationRetentionStatementsPrepareAgainstMigratedSchemaLive fails when
// any retention statement names a relation or column the migrations do not
// create.
func TestGenerationRetentionStatementsPrepareAgainstMigratedSchemaLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	for name, statement := range generationRetentionMigratedSchemaStatements {
		t.Run(name, func(t *testing.T) {
			prepared, err := database.PrepareContext(ctx, statement)
			if err != nil {
				t.Fatalf("prepare %s against migrated schema: %v", name, err)
			}
			_ = prepared.Close()
		})
	}
}

// TestGenerationRetentionPrunesMigratedSchemaLive seeds one prunable
// superseded generation with rows in every counted table that the migrations
// let a fixture populate, then proves the row-count query reports exactly those
// rows and the full prune removes them, on the real migrated schema.
func TestGenerationRetentionPrunesMigratedSchemaLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	seedGenerationRetentionMigratedFixture(t, ctx, database)

	want := map[string]int64{
		"fact_records":                        3,
		"fact_work_items":                     2,
		"fact_replay_events":                  1,
		"semantic_extraction_jobs":            0,
		"shared_projection_acceptance":        1,
		"graph_projection_phase_state":        0,
		"graph_projection_phase_repair_queue": 0,
		"iac_reachability_rows":               2,
		"shared_projection_intents":           1,
		"content_file_references":             2,
		"content_entities":                    2,
		"infra_resource_entities":             1,
		"content_files":                       1,
		// #7396: the cascade children of scope_generations the count
		// query must include so BatchRowLimit and the retention events
		// stop under-counting the prune. (eshu_search_index_terms_shadow
		// from the issue list is a transient migration artifact that the
		// migrated schema never contains; activation_obligations landed
		// after the issue and belongs to the same class.)
		"activation_obligations":                  1,
		"producer_activation_obligations":         1,
		"admission_decisions":                     1,
		"admission_decision_evidence":             2,
		"code_reachability_rows":                  2,
		"code_reachability_repository_watermarks": 1,
		"code_root_verdicts":                      1,
		"container_image_identity_cutovers":       1,
		"deferred_backfill_partition_memo":        1,
		"eshu_search_document_projection_state":   1,
		"eshu_search_index_documents":             1,
		"eshu_search_index_stats":                 1,
		"eshu_search_index_terms":                 1,
		"eshu_search_vector_metadata":             1,
		"eshu_search_vector_scope_state":          1,
		"eshu_search_vector_values":               1,
		"reducer_input_invalid_facts":             1,
		// #7784: the cascade grandchildren the prune deletes (keys via
		// the fact delete, secret lines via the file delete) plus the
		// explicitly reaped unroutable intents. #7799: the unroutable
		// count covers the well-formed doomed row and both
		// malformed-scope doomed rows the generation-only reap deletes.
		"package_manifest_consumption_keys":     2,
		"package_registry_identity_keys":        2,
		"relationship_reference_candidate_keys": 1,
		"content_file_secret_lines":             2,
		"shared_projection_unroutable_intents":  3,
		// The changed-since link ledger (#7127 ruling 2.8); this fixture
		// writes none of it.
		"changed_since_activations":        0,
		"changed_since_links":              0,
		"changed_since_link_deltas":        0,
		"changed_since_link_bucket_counts": 0,
	}

	tx, err := SQLDB{DB: database}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	totals, _, _, err := GenerationRetentionStore{}.countRows(ctx, tx, []string{"scope-1"}, []string{"gen-old"})
	_ = tx.Rollback()
	if err != nil {
		t.Fatalf("countRows() error = %v", err)
	}
	for table, wantCount := range want {
		if got := totals[table]; got != wantCount {
			t.Errorf("row count for %s = %d, want %d", table, got, wantCount)
		}
	}
	if len(totals) != len(want) {
		t.Errorf("countRows() reported %d tables, want %d: %v", len(totals), len(want), totals)
	}

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	result, err := store.PruneSupersededGenerations(ctx, GenerationRetentionPolicy{
		MinSupersededGenerations: 0,
		MaxSupersededAge:         time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            1_000_000,
		PolicyScope:              "global",
		PolicyRevision:           "6809-migrated-schema",
	})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 1 {
		t.Fatalf("GenerationsPruned = %d, want 1", result.GenerationsPruned)
	}
	for _, table := range []string{"fact_records", "content_file_references", "content_entities", "infra_resource_entities", "content_files", "shared_projection_intents", "activation_obligations", "producer_activation_obligations", "admission_decisions", "admission_decision_evidence", "code_reachability_rows", "code_reachability_repository_watermarks", "code_root_verdicts", "container_image_identity_cutovers", "deferred_backfill_partition_memo", "eshu_search_document_projection_state", "eshu_search_index_documents", "eshu_search_index_stats", "eshu_search_index_terms", "eshu_search_vector_metadata", "eshu_search_vector_scope_state", "eshu_search_vector_values", "reducer_input_invalid_facts", "package_manifest_consumption_keys", "package_registry_identity_keys", "relationship_reference_candidate_keys", "content_file_secret_lines", "shared_projection_unroutable_intents"} {
		if got := result.RowsPruned[table]; got != want[table] {
			t.Errorf("RowsPruned[%s] = %d, want %d", table, got, want[table])
		}
	}
	for _, table := range []string{"iac_reachability_rows", "content_file_references", "activation_obligations", "producer_activation_obligations", "code_reachability_rows", "code_reachability_repository_watermarks", "code_root_verdicts", "container_image_identity_cutovers", "deferred_backfill_partition_memo", "eshu_search_document_projection_state", "eshu_search_index_documents", "eshu_search_index_stats", "eshu_search_index_terms", "eshu_search_vector_metadata", "eshu_search_vector_scope_state", "eshu_search_vector_values", "reducer_input_invalid_facts"} {
		var remaining int
		if err := database.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&remaining); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if remaining != 0 {
			t.Errorf("%s has %d rows after prune, want 0", table, remaining)
		}
	}
	// #7784: the doomed main.tf file row is pruned with its secret
	// lines; the gen-active fact keeps kept.tf alive.
	var doomedFiles, keptFiles int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM content_files WHERE relative_path = 'main.tf'").Scan(&doomedFiles); err != nil {
		t.Fatalf("count pruned content_files: %v", err)
	}
	if doomedFiles != 0 {
		t.Errorf("pruned content_files after prune = %d, want 0", doomedFiles)
	}
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM content_files WHERE relative_path = 'kept.tf'").Scan(&keptFiles); err != nil {
		t.Fatalf("count retained content_files: %v", err)
	}
	if keptFiles != 1 {
		t.Errorf("retained content_files after prune = %d, want 1", keptFiles)
	}
	// #7700: the doomed decision and its evidence rows are cascade-deleted
	// with the prune; the retained gen-active decision and its evidence
	// row survive.
	var doomedDecisions int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM admission_decisions WHERE decision_id = 'dec-1'").Scan(&doomedDecisions); err != nil {
		t.Fatalf("count pruned decisions: %v", err)
	}
	if doomedDecisions != 0 {
		t.Errorf("pruned admission_decisions after prune = %d, want 0", doomedDecisions)
	}
	var keptDecisions int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM admission_decisions WHERE decision_id = 'dec-kept'").Scan(&keptDecisions); err != nil {
		t.Fatalf("count retained decisions: %v", err)
	}
	if keptDecisions != 1 {
		t.Errorf("retained admission_decisions after prune = %d, want 1", keptDecisions)
	}
	var doomedEvidence int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM admission_decision_evidence WHERE evidence_id IN ('ev-1', 'ev-2')").Scan(&doomedEvidence); err != nil {
		t.Fatalf("count pruned evidence: %v", err)
	}
	if doomedEvidence != 0 {
		t.Errorf("pruned admission_decision_evidence after prune = %d, want 0", doomedEvidence)
	}
	var keptEvidence int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM admission_decision_evidence WHERE evidence_id = 'ev-kept'").Scan(&keptEvidence); err != nil {
		t.Fatalf("count retained evidence: %v", err)
	}
	if keptEvidence != 1 {
		t.Errorf("retained admission_decision_evidence after prune = %d, want 1", keptEvidence)
	}
	assertGenerationRetentionGrandchildren7784(t, ctx, database)
	// #7695: gen-old's facts are pruned; the retained holder's gen-active fact
	// survives. #7784 adds a second gen-active fact, the file fact that
	// keeps kept.tf (and its secret line) alive.
	var retainedFacts int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM fact_records WHERE generation_id = 'gen-active'").Scan(&retainedFacts); err != nil {
		t.Fatalf("count retained gen-active facts: %v", err)
	}
	if retainedFacts != 2 {
		t.Errorf("retained gen-active fact_records after prune = %d, want 2", retainedFacts)
	}
	var doomedFacts int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM fact_records WHERE generation_id = 'gen-old'").Scan(&doomedFacts); err != nil {
		t.Fatalf("count pruned gen-old facts: %v", err)
	}
	if doomedFacts != 0 {
		t.Errorf("pruned gen-old fact_records after prune = %d, want 0", doomedFacts)
	}
	// #7695: the retained holder's rows survive the prune. The exact-count
	// assertions above already prove entity-3 is excluded from gen-old's
	// content_entities and infra_resource_entities counts.
	for _, table := range []string{"content_entities", "infra_resource_entities"} {
		var remaining int
		if err := database.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE entity_id = 'entity-3'").Scan(&remaining); err != nil {
			t.Fatalf("count retained %s: %v", table, err)
		}
		if remaining != 1 {
			t.Errorf("retained %s entity-3 rows after prune = %d, want 1", table, remaining)
		}
		var doomed int
		if err := database.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE entity_id = 'entity-1'").Scan(&doomed); err != nil {
			t.Fatalf("count pruned %s: %v", table, err)
		}
		if doomed != 0 {
			t.Errorf("pruned %s entity-1 rows after prune = %d, want 0", table, doomed)
		}
		if table == "content_entities" {
			var doomedMirrorless int
			if err := database.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE entity_id = 'entity-2'").Scan(&doomedMirrorless); err != nil {
				t.Fatalf("count pruned %s: %v", table, err)
			}
			if doomedMirrorless != 0 {
				t.Errorf("pruned %s entity-2 rows after prune = %d, want 0", table, doomedMirrorless)
			}
		}
	}
}

// seedGenerationRetentionMigratedFixture inserts one active generation and one
// prunable superseded generation ("gen-old") with dependent rows.
func seedGenerationRetentionMigratedFixture(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	steps := []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ('scope-1', 'repository', 'git', 'repo-1', 'git', 'repo-1', now(), now(), 'active', '{}'::jsonb)`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('gen-active', 'scope-1', 'snapshot', now(), now(), 'active')`,
		`UPDATE ingestion_scopes SET active_generation_id = 'gen-active' WHERE scope_id = 'scope-1'`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, superseded_at)
VALUES ('gen-old', 'scope-1', 'snapshot', now() - interval '2 days', now() - interval '2 days',
    'superseded', now() - interval '1 day')`,
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload) VALUES
('fact-file', 'scope-1', 'gen-old', 'file', 'k-file', 'git', 'k-file', now(), now(),
    '{"repo_id":"repo-1","relative_path":"main.tf"}'::jsonb),
('fact-entity', 'scope-1', 'gen-old', 'content_entity', 'k-entity', 'git', 'k-entity', now(), now(),
    '{"repo_id":"repo-1","entity_id":"entity-1"}'::jsonb)`,
		`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
VALUES ('work-1', 'scope-1', 'gen-old', 'reducer', 'domain', 'succeeded', now(), now())`,
		`INSERT INTO fact_replay_events (replay_event_id, work_item_id, scope_id, generation_id, created_at)
VALUES ('replay-1', 'work-1', 'scope-1', 'gen-old', now())`,
		`INSERT INTO shared_projection_acceptance (scope_id, acceptance_unit_id, source_run_id, generation_id, accepted_at, updated_at)
VALUES ('scope-1', 'unit-1', 'run-1', 'gen-old', now(), now())`,
		`INSERT INTO iac_reachability_rows (scope_id, generation_id, repo_id, family, artifact_path, artifact_name,
    reachability, finding, confidence, evidence, limitations, observed_at, updated_at) VALUES
('scope-1', 'gen-old', 'repo-1', 'terraform', 'a', 'a', 'used', 'f', 1, '[]'::jsonb, '[]'::jsonb, now(), now()),
('scope-1', 'gen-old', 'repo-1', 'terraform', 'b', 'b', 'unused', 'f', 1, '[]'::jsonb, '[]'::jsonb, now(), now())`,
		`INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id, payload, created_at)
VALUES ('intent-1', 'domain', 'p', 'scope-1', 'unit-1', 'repo-1', 'run-1', 'gen-old', '{}'::jsonb, now())`,
		`INSERT INTO content_files (repo_id, relative_path, content, content_hash, line_count, indexed_at)
VALUES ('repo-1', 'main.tf', 'x', 'h', 1, now())`,
		`INSERT INTO content_file_references (repo_id, relative_path, reference_kind, reference_value, indexed_at) VALUES
('repo-1', 'main.tf', 'module', 'a', now()),
('repo-1', 'main.tf', 'module', 'b', now())`,
		`INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, indexed_at)
VALUES ('entity-1', 'repo-1', 'main.tf', 'TerraformResource', 'r', 1, 2, 'x', now())`,
		`INSERT INTO infra_resource_entities (entity_id, repo_id, relative_path, label, entity_name, updated_at)
VALUES ('entity-1', 'repo-1', 'main.tf', 'TerraformResource', 'r', now())`,
		// #7695: a retained holder for the infra-mirror exclusion (folded in
		// from TestGenerationRetentionInfraMirrorCountLive, retired): gen-active
		// is active and still holds entity-3, so its content and mirror rows
		// must be neither counted against gen-old nor pruned.
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload) VALUES
('fact-entity-new', 'scope-1', 'gen-active', 'content_entity', 'k-entity-new', 'git', 'k-entity-new', now(), now(),
    '{"repo_id":"repo-1","entity_id":"entity-3"}'::jsonb)`,
		`INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, indexed_at)
VALUES ('entity-3', 'repo-1', 'main.tf', 'TerraformResource', 'r3', 1, 2, 'x', now())`,
		`INSERT INTO infra_resource_entities (entity_id, repo_id, relative_path, label, entity_name, updated_at)
VALUES ('entity-3', 'repo-1', 'main.tf', 'TerraformResource', 'r3', now())`,
		// Review P2 (#7756): a mirror-less prunable entity, restoring the
		// retired test's discrimination. gen-old holds entity-2 as a content
		// fact plus a content_entities row with no infra row, so
		// content_entities = 2 while infra_resource_entities stays 1: an
		// infra arm that regressed to counting content rows fails here.
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload) VALUES
('fact-entity-2', 'scope-1', 'gen-old', 'content_entity', 'k-entity-2', 'git', 'k-entity-2', now(), now(),
    '{"repo_id":"repo-1","entity_id":"entity-2"}'::jsonb)`,
		`INSERT INTO content_entities (entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, source_cache, indexed_at)
VALUES ('entity-2', 'repo-1', 'main.tf', 'TerraformResource', 'r2', 1, 2, 'x', now())`,
		// #7396: one row in each cascade child of scope_generations the
		// row-count query must cover. The cutover guard needs a live
		// container_image_identity work item, which the final flip retires
		// so gen-old stays prunable (the completion trigger it fires
		// writes to cross_scope_completion_events, outside retention).
		`INSERT INTO activation_obligations (generation_id, scope_id, work_item_id) VALUES ('gen-old', 'scope-1', 'work-1')`,
		`INSERT INTO producer_activation_obligations (generation_id, scope_id, work_item_id) VALUES ('gen-old', 'scope-1', 'work-1')`,
		`INSERT INTO admission_decisions (decision_id, domain, state, domain_state, scope_id, generation_id, anchor_kind, anchor_id, candidate_kind, candidate_id, confidence_score, confidence_bucket, confidence_basis, freshness_state, freshness_cause, redaction_state, redaction_reason, payload_version, decided_at, updated_at) VALUES ('dec-1', 'd', 'admitted', 'ds', 'scope-1', 'gen-old', 'ak', 'ai', 'ck', 'ci', 0.5, 'b', 'basis', 'fs', 'fc', 'rs', 'rr', 'v1', now(), now())`,
		// #7700: two evidence rows on the doomed decision (counted and
		// cascade-deleted) plus one on a retained gen-active decision
		// (neither counted nor pruned).
		`INSERT INTO admission_decisions (decision_id, domain, state, domain_state, scope_id, generation_id, anchor_kind, anchor_id, candidate_kind, candidate_id, confidence_score, confidence_bucket, confidence_basis, freshness_state, freshness_cause, redaction_state, redaction_reason, payload_version, decided_at, updated_at) VALUES ('dec-kept', 'd', 'admitted', 'ds', 'scope-1', 'gen-active', 'ak', 'ai', 'ck', 'ci', 0.5, 'b', 'basis', 'fs', 'fc', 'rs', 'rr', 'v1', now(), now())`,
		`INSERT INTO admission_decision_evidence (evidence_id, decision_id, source_handle, evidence_kind, created_at) VALUES
('ev-1', 'dec-1', 'h', 'k', now()),
('ev-2', 'dec-1', 'h', 'k', now()),
('ev-kept', 'dec-kept', 'h', 'k', now())`,
		`INSERT INTO code_reachability_repository_watermarks (scope_id, generation_id, repository_id, updated_at) VALUES ('scope-1', 'gen-old', 'repo-1', now())`,
		`INSERT INTO code_reachability_rows (scope_id, generation_id, repository_id, root_entity_id, entity_id, depth, state, confidence, min_resolution_method, evidence, root_kinds, observed_at, updated_at) VALUES ('scope-1', 'gen-old', 'repo-1', 'root-1', 'ent-1', 0, 'resolved', 1.0, 'm', '{}'::jsonb, '[]'::jsonb, now(), now()), ('scope-1', 'gen-old', 'repo-1', 'root-1', 'ent-2', 1, 'resolved', 0.5, 'm', '{}'::jsonb, '[]'::jsonb, now(), now())`,
		`INSERT INTO code_root_verdicts (scope_id, generation_id, repository_id, entity_id, root_kind, verdict, basis, observed_at, updated_at) VALUES ('scope-1', 'gen-old', 'repo-1', 'ent-1', 'rk', 'v', '{}'::jsonb, now(), now())`,
		`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, conflict_domain, conflict_key, status, attempt_count, lease_owner, claim_until, payload, container_image_identity_claim_epoch, created_at, updated_at) VALUES ('work-cii', 'scope-1', 'gen-old', 'reducer', 'container_image_identity', 'intent', 'work-cii', 'claimed', 1, 'seed', now() + interval '1 minute', '{}'::jsonb, 1, now(), now())`,
		`INSERT INTO container_image_identity_cutovers (scope_id, generation_id, activated_by_work_item_id, activated_by_claim_epoch) VALUES ('scope-1', 'gen-old', 'work-cii', 1)`,
		`UPDATE fact_work_items SET status = 'succeeded', lease_owner = NULL, claim_until = NULL WHERE work_item_id = 'work-cii'`,
		`INSERT INTO deferred_backfill_partition_memo (scope_id, generation_id, catalog_fingerprint, committed_at) VALUES ('scope-1', 'gen-old', 'fp', now())`,
		`INSERT INTO eshu_search_document_projection_state (scope_id, generation_id, projection_revision, build_fence, state, updated_at) VALUES ('scope-1', 'gen-old', 1, 1, 'ready', now())`,
		`INSERT INTO eshu_search_index_documents (scope_id, generation_id, document_id, fact_id, repo_id, source_kind, document, document_length, updated_at) VALUES ('scope-1', 'gen-old', 'doc-1', 'fact-1', 'repo-1', 'sk', '{}'::jsonb, 1, now())`,
		`INSERT INTO eshu_search_index_stats (scope_id, generation_id, document_count, average_document_length, updated_at) VALUES ('scope-1', 'gen-old', 1, 1.0, now())`,
		`INSERT INTO eshu_search_index_terms (scope_id, generation_id, document_id, term_key, term, term_frequency) VALUES ('scope-1', 'gen-old', 'doc-1', 'tk', 'term', 1)`,
		`INSERT INTO eshu_search_vector_metadata (scope_id, generation_id, document_id, embedding_model_id, embedding_dimensions, embedding_content_hash, vector_index_version, build_state, created_at, updated_at) VALUES ('scope-1', 'gen-old', 'doc-1', 'model', 3, 'h', 'v1', 'ready', now(), now())`,
		`INSERT INTO eshu_search_vector_scope_state (scope_id, generation_id, provider_profile_id, source_class, embedding_model_id, vector_index_version, projection_revision, build_fence, state, updated_at) VALUES ('scope-1', 'gen-old', 'pp', 'sc', 'model', 'v1', 1, 1, 'ready', now())`,
		`INSERT INTO eshu_search_vector_values (scope_id, generation_id, document_id, embedding_model_id, embedding_dimensions, embedding_content_hash, vector_index_version, vector_values, created_at, updated_at) VALUES ('scope-1', 'gen-old', 'doc-1', 'model', 3, 'h', 'v1', '{0.1,0.2,0.3}', now(), now())`,
		`INSERT INTO reducer_input_invalid_facts (fact_id, fact_kind, missing_field, failure_class, domain, scope_id, generation_id, decided_at) VALUES ('fact-1', 'fk', 'mf', 'fc', 'd', 'scope-1', 'gen-old', now())`,
	}
	for i, step := range steps {
		if _, err := database.ExecContext(ctx, step); err != nil {
			t.Fatalf("seed step %d: %v", i, err)
		}
	}
	// #7784 grandchildren live in their own file: this one sits at the
	// 500-line cap.
	seedGenerationRetentionGrandchildren7784(t, ctx, database)
}
