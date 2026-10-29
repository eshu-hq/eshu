// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9307 disposable PostgreSQL 18.6 fresh and populated ECS index proof: an observed active concurrent build admits a second fact writer, an ordinary build queues that writer to 55P03, and invalid-index restart recovery rebuilds 242/244
//eshu:readiness:env ESHU_TEST_ECS_RUNTIME_INDEX_POSTGRES_DISPOSABLE ESHU_TEST_ECS_RUNTIME_INDEX_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const (
	ecsIndexProofDSN        = "ESHU_TEST_ECS_RUNTIME_INDEX_POSTGRES_DSN"
	ecsIndexProofDisposable = "ESHU_TEST_ECS_RUNTIME_INDEX_POSTGRES_DISPOSABLE"
)

var ecsRuntimeIndexNames = []string{
	"fact_records_ecs_service_arn_idx",
	"fact_records_ecs_task_scope_cluster_group_idx",
}

// TestECSRuntimeIndexFreshBootstrapLive proves the complete ordered migration
// tree installs both indexes on a new database.
func TestECSRuntimeIndexFreshBootstrapLive(t *testing.T) {
	ctx, database := postgresproof.OpenDisposableDatabase(t,
		os.Getenv(ecsIndexProofDSN), os.Getenv(ecsIndexProofDisposable), 5*time.Minute)
	if err := ApplyBootstrap(ctx, SQLDB{DB: database}); err != nil {
		t.Fatalf("fresh ApplyBootstrap: %v", err)
	}
	for _, name := range ecsRuntimeIndexNames {
		assertECSIndexState(t, ctx, database, name, true)
	}
}

// TestECSRuntimeIndexUpgradeLive proves the shipped migration runner creates
// both indexes over populated facts while a second writer commits during an
// observed active index build, then recovers a failed build on restart.
func TestECSRuntimeIndexUpgradeLive(t *testing.T) {
	ctx, database := postgresproof.OpenDisposableDatabase(t,
		os.Getenv(ecsIndexProofDSN), os.Getenv(ecsIndexProofDisposable), 5*time.Minute)
	definitions := BootstrapDefinitions()
	boundary := -1
	for i, definition := range definitions {
		if definition.Name == "ecs_runtime_service_arn_index" {
			boundary = i
			break
		}
	}
	if boundary < 0 || boundary+1 >= len(definitions) || definitions[boundary+1].Name != "ecs_runtime_task_scope_index" {
		t.Fatalf("ECS index migration order is missing or nonadjacent: boundary=%d", boundary)
	}
	exec := SQLDB{DB: database}
	if err := applyBootstrapDefinitionsWith(ctx, exec, definitions[:boundary], slog.Default(), schemaBootstrapCoordination{}); err != nil {
		t.Fatalf("apply prior schema: %v", err)
	}
	seedECSRuntimeIndexFacts(t, ctx, database)
	for _, name := range ecsRuntimeIndexNames {
		assertECSIndexState(t, ctx, database, name, false)
	}

	held := holdECSIndexWriter(t, ctx, database, "ecs-index-held-concurrent")
	defer func() { _ = held.Rollback() }()
	applied := make(chan error, 1)
	started := time.Now()
	go func() {
		applied <- applyBootstrapDefinitionsWith(ctx, exec, definitions[boundary:boundary+2], slog.Default(), schemaBootstrapCoordination{})
	}()
	waitECSIndexBuild(t, ctx, database, true)
	if err := writeECSIndexFact(ctx, database, "ecs-index-second-concurrent", 300*time.Millisecond); err != nil {
		t.Fatalf("second fact writer during observed concurrent index build: %v", err)
	}
	select {
	case err := <-applied:
		t.Fatalf("index build finished before held writer committed: %v", err)
	default:
	}
	if err := held.Commit(); err != nil {
		t.Fatalf("commit held writer: %v", err)
	}
	upgradeErr := <-applied
	upgradeDuration := time.Since(started)
	if upgradeErr != nil {
		t.Fatalf("upgrade populated schema: %v", upgradeErr)
	}
	t.Logf("populated upgrade=%s; second writer committed while concurrent index build remained active", upgradeDuration)
	for _, name := range ecsRuntimeIndexNames {
		assertECSIndexState(t, ctx, database, name, true)
	}
	if err := ApplyBootstrap(ctx, exec); err != nil {
		t.Fatalf("restart bootstrap: %v", err)
	}

	name := ecsRuntimeIndexNames[0]
	if _, err := database.ExecContext(ctx, "DROP INDEX CONCURRENTLY "+name); err != nil {
		t.Fatalf("drop index before interrupted-build proof: %v", err)
	}
	invalidDDL := "CREATE UNIQUE INDEX CONCURRENTLY " + name +
		" ON fact_records (fact_kind) WHERE fact_kind = 'aws_resource' AND is_tombstone = FALSE"
	if _, err := database.ExecContext(ctx, invalidDDL); err == nil {
		t.Fatal("duplicate facts unexpectedly allowed unique index")
	}
	assertECSIndexValidity(t, ctx, database, name, false)
	if err := ApplyBootstrap(ctx, exec); err != nil {
		t.Fatalf("recover invalid index through actual bootstrap: %v", err)
	}
	for _, indexName := range ecsRuntimeIndexNames {
		assertECSIndexState(t, ctx, database, indexName, true)
	}
	proveECSOrdinaryIndexBlocksWriter(t, ctx, database)
}

type ecsIndexFactExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertECSIndexFact(ctx context.Context, exec ecsIndexFactExecutor, id string) error {
	_, err := exec.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind,
  stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, 'scope:ecs-index-proof', 'generation:ecs-index-proof', 'aws_resource',
  $1, 'proof', $1, clock_timestamp(), clock_timestamp(),
  '{"resource_type":"aws_ecs_task","state":"RUNNING","attributes":{"cluster_arn":"arn:aws:ecs:us-east-1:123456789012:cluster/proof","group":"service:proof"}}'::jsonb)`, id)
	return err
}

func holdECSIndexWriter(t *testing.T, ctx context.Context, database *sql.DB, id string) *sql.Tx {
	t.Helper()
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin held fact writer: %v", err)
	}
	if err := insertECSIndexFact(ctx, transaction, id); err != nil {
		_ = transaction.Rollback()
		t.Fatalf("insert held fact writer: %v", err)
	}
	return transaction
}

func writeECSIndexFact(ctx context.Context, database *sql.DB, id string, lockTimeout time.Duration) error {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin second fact writer: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	if _, err := transaction.ExecContext(ctx, "SELECT set_config('lock_timeout', $1, true)", lockTimeout.String()); err != nil {
		return fmt.Errorf("set second writer lock timeout: %w", err)
	}
	if err := insertECSIndexFact(ctx, transaction, id); err != nil {
		return fmt.Errorf("insert second fact: %w", err)
	}
	return transaction.Commit()
}

func waitECSIndexBuild(t *testing.T, ctx context.Context, database *sql.DB, concurrent bool) {
	t.Helper()
	statement := `SELECT EXISTS (SELECT 1 FROM pg_stat_progress_create_index
WHERE datid = (SELECT oid FROM pg_database WHERE datname = current_database())
  AND relid = 'fact_records'::regclass AND command = 'CREATE INDEX CONCURRENTLY')`
	if !concurrent {
		statement = `SELECT EXISTS (SELECT 1 FROM pg_locks
WHERE database = (SELECT oid FROM pg_database WHERE datname = current_database())
  AND relation = 'fact_records'::regclass AND mode = 'ShareLock' AND NOT granted)`
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		var active bool
		if err := database.QueryRowContext(ctx, statement).Scan(&active); err != nil {
			t.Fatalf("inspect index build state: %v", err)
		}
		if active {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("index build did not reach observed state (concurrent=%t)", concurrent)
}

func proveECSOrdinaryIndexBlocksWriter(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	held := holdECSIndexWriter(t, ctx, database, "ecs-index-held-ordinary")
	defer func() { _ = held.Rollback() }()
	control := Definition{
		Name: "ecs_runtime_ordinary_control",
		Path: "test/9307_ecs_runtime_ordinary_control.sql",
		SQL: strings.Replace(MigrationSQL("ecs_runtime_service_arn_index"),
			"CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_ecs_service_arn_idx",
			"CREATE INDEX IF NOT EXISTS fact_records_ecs_service_arn_red_idx", 1),
	}
	if !strings.Contains(control.SQL, "CREATE INDEX IF NOT EXISTS fact_records_ecs_service_arn_red_idx") {
		t.Fatal("ordinary control did not replace the concurrent statement")
	}
	applied := make(chan error, 1)
	go func() {
		applied <- applyBootstrapDefinitionsWith(ctx, SQLDB{DB: database}, []Definition{control}, slog.Default(), schemaBootstrapCoordination{})
	}()
	waitECSIndexBuild(t, ctx, database, false)
	err := writeECSIndexFact(ctx, database, "ecs-index-second-ordinary", 200*time.Millisecond)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("ordinary index queued writer: err=%v, want 55P03 lock timeout", err)
	}
	t.Log("ordinary CREATE INDEX held an ungranted ShareLock; second writer received SQLSTATE 55P03")
	select {
	case err := <-applied:
		t.Fatalf("ordinary index finished before held writer committed: %v", err)
	default:
	}
	if err := held.Commit(); err != nil {
		t.Fatalf("commit held ordinary writer: %v", err)
	}
	if err := <-applied; err != nil {
		t.Fatalf("complete ordinary control index: %v", err)
	}
	assertECSIndexState(t, ctx, database, "fact_records_ecs_service_arn_red_idx", true)
}

func seedECSRuntimeIndexFacts(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	_, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key,
  collector_kind, partition_key, observed_at, ingested_at, status)
VALUES ('scope:ecs-index-proof', 'aws_account', 'proof', 'proof', 'proof',
  'proof', clock_timestamp(), clock_timestamp(), 'active');
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at,
  ingested_at, status, activated_at)
VALUES ('generation:ecs-index-proof', 'scope:ecs-index-proof', 'proof',
  clock_timestamp(), clock_timestamp(), 'active', clock_timestamp());
UPDATE ingestion_scopes SET active_generation_id = 'generation:ecs-index-proof'
WHERE scope_id = 'scope:ecs-index-proof';
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind,
  stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT 'ecs-index:' || n, 'scope:ecs-index-proof', 'generation:ecs-index-proof',
  'aws_resource', 'ecs-index:' || n, 'proof', 'ecs-index:' || n,
  clock_timestamp(), clock_timestamp(),
  CASE WHEN n % 2 = 0 THEN
    jsonb_build_object('resource_type', 'aws_ecs_service', 'arn',
      'arn:aws:ecs:us-east-1:123456789012:service/proof/service-' || n)
  ELSE jsonb_build_object('resource_type', 'aws_ecs_task', 'state', 'RUNNING',
    'attributes', jsonb_build_object('cluster_arn',
      'arn:aws:ecs:us-east-1:123456789012:cluster/proof', 'group', 'service:proof')) END
FROM generate_series(1, 30000) AS n`)
	if err != nil {
		t.Fatalf("seed 30,000 ECS facts: %v", err)
	}
}

func assertECSIndexState(t *testing.T, ctx context.Context, database *sql.DB, name string, want bool) {
	t.Helper()
	var definition sql.NullString
	err := database.QueryRowContext(ctx, "SELECT pg_get_indexdef(to_regclass($1))", name).Scan(&definition)
	if err != nil || definition.Valid != want {
		t.Fatalf("index %s present=%t, want %t, err=%v", name, definition.Valid, want, err)
	}
	if want {
		assertECSIndexValidity(t, ctx, database, name, true)
		if !strings.Contains(definition.String, "fact_records") {
			t.Fatalf("index %s definition = %q", name, definition.String)
		}
	}
}

func assertECSIndexValidity(t *testing.T, ctx context.Context, database *sql.DB, name string, want bool) {
	t.Helper()
	var valid, ready bool
	err := database.QueryRowContext(ctx, `SELECT i.indisvalid, i.indisready FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid WHERE c.relname = $1`, name).Scan(&valid, &ready)
	if err != nil || valid != want || (want && !ready) {
		t.Fatalf("index %s valid=%t ready=%t, want valid=%t, err=%v", name, valid, ready, want, err)
	}
}
