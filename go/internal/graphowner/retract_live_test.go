// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graphowner

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// boltTestExecutor runs cypher Statements through one Bolt session per call:
// the retract path's sequential auto-commit dispatch shape.
type boltTestExecutor struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (e *boltTestExecutor) Execute(ctx context.Context, stmt cypher.Statement) error {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.database,
	})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}

// TestLiveCloudRetractEndToEnd is the #6887 production-shape proof over a
// real Postgres ledger and a real NornicDB graph: with uid-shared admitted in
// scope B's current generation and uid-dead only in scope A's superseded
// generation, the retract deletes exactly uid-dead's node, releases exactly
// uid-dead's ledger row, keeps uid-shared's node and ledger row, and replays
// to the same outcome with zero further deletes.
//
// Skipped by default; set ESHU_CLOUD_RETRACT_PROVE_LIVE=1,
// ESHU_CLOUD_RETRACT_PG_DSN, and the ESHU_NEO4J_URI/USERNAME/PASSWORD/DATABASE
// env the shared driver loader reads.
func TestLiveCloudRetractEndToEnd(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_CLOUD_RETRACT_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_CLOUD_RETRACT_PROVE_LIVE=1 (+ PG DSN + NornicDB env) to run the cloud-retract end-to-end proof")
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_CLOUD_RETRACT_PG_DSN"))
	if dsn == "" {
		t.Fatal("ESHU_CLOUD_RETRACT_PG_DSN is required")
	}
	ctx := context.Background()

	rawDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = rawDB.Close() }()
	sqldb := postgres.SQLDB{DB: rawDB}
	if err := postgres.NewGraphNodeOwnerStore().EnsureSchema(ctx, sqldb); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, os.Getenv)
	if err != nil {
		t.Fatalf("open Bolt driver: %v", err)
	}
	defer func() {
		cc, ccl := context.WithTimeout(context.Background(), 5*time.Second)
		defer ccl()
		_ = driver.Close(cc)
	}()

	prefix := fmt.Sprintf("e2e-retract-%d", time.Now().UnixNano())
	now := time.Now().UTC().Truncate(time.Millisecond)
	uidShared, uidDead := prefix+"-shared", prefix+"-dead"
	scopeA, scopeB := prefix+"-a", prefix+"-b"
	genA1, genA2, genB1 := prefix+"-a1", prefix+"-a2", prefix+"-b1"

	execSQL := func(query string, args ...any) {
		t.Helper()
		if _, err := rawDB.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}
	seedScope := func(scopeID, activeGen string) {
		t.Helper()
		var activeGenAny any
		if activeGen != "" {
			activeGenAny = activeGen
		}
		execSQL(`
INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind,
   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'aws', 'aws', $1, 'aws', $1, $2, $2, 'active', $3, '{}'::jsonb)
ON CONFLICT (scope_id) DO UPDATE SET active_generation_id = EXCLUDED.active_generation_id`,
			scopeID, now, activeGenAny)
	}
	seedGeneration := func(genID, scopeID, status string) {
		t.Helper()
		var activatedAt any
		if status == "active" {
			activatedAt = now
		}
		execSQL(`
INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ($1, $2, 'manual', $3, $3, $4, $5)
ON CONFLICT (generation_id) DO UPDATE SET status = EXCLUDED.status, activated_at = EXCLUDED.activated_at`,
			genID, scopeID, now, status, activatedAt)
	}
	seedAdmission := func(scopeID, genID, key, uid string, tombstone bool) {
		t.Helper()
		execSQL(`
INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key,
   observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, 'reducer_cloud_resource_identity', $4, 'aws', $4, $5, $5, $6,
  ('{"cloud_resource_uid": "' || $7 || '"}')::jsonb)`,
			prefix+"-fact-"+key, scopeID, genID, key, now, tombstone, uid)
	}

	seedScope(scopeA, genA2)
	seedScope(scopeB, genB1)
	seedGeneration(genA1, scopeA, "superseded")
	seedGeneration(genA2, scopeA, "active")
	seedGeneration(genB1, scopeB, "active")
	// uid-shared: dead in A's history, live in B's present. uid-dead: A1 only.
	seedAdmission(scopeA, genA1, "k-shared-a1", uidShared, false)
	seedAdmission(scopeB, genB1, "k-shared-b1", uidShared, false)
	seedAdmission(scopeA, genA1, "k-dead", uidDead, false)

	store := postgres.NewGraphNodeOwnerStore()
	beginWork := func() postgres.SQLTx {
		t.Helper()
		tx, err := rawDB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		return postgres.SQLTx{Tx: tx}
	}
	resolve := func(q postgres.SQLTx, uid string) {
		t.Helper()
		if _, _, err := store.ResolveOwnedUIDs(ctx, q, []postgres.GraphNodeOwnerEntry{
			{UID: uid, SourceOrderKey: "9999-z", WinningRow: []byte(`{}`)},
		}, now); err != nil {
			t.Fatalf("resolve %s: %v", uid, err)
		}
	}
	seedTx := beginWork()
	resolve(seedTx, uidShared)
	resolve(seedTx, uidDead)
	if err := seedTx.Commit(); err != nil {
		t.Fatalf("commit seed ledger: %v", err)
	}

	graphExec := func(cypherText string, params map[string]any) {
		t.Helper()
		if err := graphOwnerProbeExec(ctx, driver, cfg.DatabaseName, cypherText, params); err != nil {
			t.Fatalf("graph exec: %v", err)
		}
	}
	graphCount := func(uid string) int64 {
		t.Helper()
		session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
			AccessMode:   neo4jdriver.AccessModeRead,
			DatabaseName: cfg.DatabaseName,
		})
		defer func() { _ = session.Close(ctx) }()
		result, err := session.Run(ctx,
			`MATCH (n:CloudResource {uid: $uid}) RETURN count(n) AS c`, map[string]any{"uid": uid})
		if err != nil {
			t.Fatalf("graph count: %v", err)
		}
		record, err := result.Single(ctx)
		if err != nil {
			t.Fatalf("graph count single: %v", err)
		}
		c, _ := record.Get("c")
		n, _ := c.(int64)
		return n
	}
	graphExec(`CREATE (n:CloudResource {uid: $uid})`, map[string]any{"uid": uidShared})
	graphExec(`CREATE (n:CloudResource {uid: $uid})`, map[string]any{"uid": uidDead})

	cleanup := func() {
		graphExec(`MATCH (n:CloudResource {uid: $uid}) DETACH DELETE n`, map[string]any{"uid": uidShared})
		graphExec(`MATCH (n:CloudResource {uid: $uid}) DETACH DELETE n`, map[string]any{"uid": uidDead})
		execSQL(`DELETE FROM fact_records WHERE fact_id LIKE '` + prefix + `-fact-%'`)
		execSQL(`DELETE FROM scope_generations WHERE generation_id LIKE '` + prefix + `-%'`)
		execSQL(`DELETE FROM ingestion_scopes WHERE scope_id LIKE '` + prefix + `-%'`)
		execSQL(`DELETE FROM graph_node_owner WHERE uid IN ($1, $2)`, uidShared, uidDead)
	}
	defer cleanup()

	gate := NewGate(sqldb)
	rawWriter := cypher.NewCloudResourceNodeWriter(&boltTestExecutor{driver: driver, database: cfg.DatabaseName}, 0)
	retracter := NewCloudResourceRetracter(gate, rawWriter.RetractCloudResourceNodes)

	// Candidates arrive unsorted on purpose: the gate sorts them.
	start := time.Now()
	retracted, err := retracter.RetractDeadCloudResourceNodes(
		ctx, []string{uidDead, uidShared}, "reducer/aws-resources",
	)
	firstDuration := time.Since(start)
	if err != nil {
		t.Fatalf("retract: %v", err)
	}
	if retracted != 1 {
		t.Fatalf("retracted = %d, want exactly 1 (uid-dead)", retracted)
	}
	if got := graphCount(uidShared); got != 1 {
		t.Fatalf("shared node count = %d, want 1 (no over-delete across scopes)", got)
	}
	if got := graphCount(uidDead); got != 0 {
		t.Fatalf("dead node count = %d, want 0", got)
	}
	ledgerCount := func(uid string) int {
		var n int
		if err := rawDB.QueryRowContext(ctx,
			`SELECT count(*) FROM graph_node_owner WHERE uid = $1`, uid).Scan(&n); err != nil {
			t.Fatalf("ledger count: %v", err)
		}
		return n
	}
	if got := ledgerCount(uidShared); got != 1 {
		t.Fatalf("shared ledger rows = %d, want 1", got)
	}
	if got := ledgerCount(uidDead); got != 0 {
		t.Fatalf("dead ledger rows = %d, want 0 (released)", got)
	}

	// Replay reconverges: the same candidates re-prove dead (uid-dead's facts
	// are still history-only) and the delete re-issues harmlessly against the
	// already-absent node, so the count repeats while the end state holds.
	replayed, err := retracter.RetractDeadCloudResourceNodes(
		ctx, []string{uidShared, uidDead}, "reducer/aws-resources",
	)
	if err != nil {
		t.Fatalf("replay retract: %v", err)
	}
	if replayed != 1 {
		t.Fatalf("replay retracted = %d, want 1 (same deterministic decision)", replayed)
	}
	if got := graphCount(uidShared); got != 1 {
		t.Fatalf("shared node count after replay = %d, want 1", got)
	}
	t.Logf("first retract duration=%s (2 candidates, 1 delete, battery graph+pg)", firstDuration)
}
