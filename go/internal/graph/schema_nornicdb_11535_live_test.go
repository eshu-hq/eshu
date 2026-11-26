// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestLiveNornicDBStrictSchemaApplyIsClean is the #11535 regression: on
// NornicDB 1.5.x the dialect must emit one schema object per (label, uid) —
// the uid uniqueness constraint, whose 1.5.x backing index serves uid reads —
// plus a DROP INDEX guard for the retired nornicdb_<t>_uid_lookup indexes.
// The pre-D dialect emitted a plain uid index and then the constraint on the
// same (label, uid), and the 1.5.x constraint now throws IndexAlreadyExists,
// so strict apply died with 120 failed statements.
//
// It runs EnsureSchemaWithBackendStrict, the production bootstrap entry
// point, against a live 1.5.x backend and requires zero failures, then
// asserts the end state: every <t>_uid_unique constraint exists, its
// constraint-named backing index is the only uid index, and no
// nornicdb_<t>_uid_lookup index remains.
//
// Gate: ESHU_SCHEMA_NORNICDB_11535_LIVE=1 with ESHU_NEO4J_URI pointing at a
// disposable NornicDB 1.5.x container (fresh volume: the companion test
// creates legacy lookup indexes to prove the upgrade path).
//
//	ESHU_SCHEMA_NORNICDB_11535_LIVE=1 ESHU_NEO4J_URI=bolt://127.0.0.1:19688 \
//	go test ./internal/graph -run TestLiveNornicDBStrictSchemaApply -count=1 -v -timeout 20m
const nornicDBStrictSchemaLiveEnv = "ESHU_SCHEMA_NORNICDB_11535_LIVE"

// nornicDBStrictLiveExecutor runs schema DDL against the live NornicDB
// database through the production driver call the scale proofs use.
type nornicDBStrictLiveExecutor struct {
	driver neo4jdriver.DriverWithContext
}

func (e nornicDBStrictLiveExecutor) ExecuteCypher(ctx context.Context, stmt CypherStatement) error {
	_, err := neo4jdriver.ExecuteQuery(ctx, e.driver, stmt.Cypher, stmt.Parameters,
		neo4jdriver.EagerResultTransformer, neo4jdriver.ExecuteQueryWithDatabase("nornic"))
	return err
}

// filteredNornicDBStrictLiveExecutor mirrors the production adoption contract
// (missingGraphSchemaExecutor in cmd/bootstrap-data-plane): statements whose
// schema object already exists are skipped, DROP guards always run. An
// unconditional second apply is not the production path — the fulltext
// procedure form has no IF NOT EXISTS and fails with
// EquivalentSchemaRuleAlreadyExists when the index exists (measured on
// 1.5.x) — so the idempotency proof runs through this filter, exactly as
// bootstrap does.
type filteredNornicDBStrictLiveExecutor struct {
	nornicDBStrictLiveExecutor
	existing map[string]struct{}
}

func (e filteredNornicDBStrictLiveExecutor) ExecuteCypher(ctx context.Context, stmt CypherStatement) error {
	if name, ok := nornicDBStrictLiveObjectName(stmt.Cypher); ok {
		if _, exists := e.existing[name]; exists {
			return nil
		}
	}
	return e.nornicDBStrictLiveExecutor.ExecuteCypher(ctx, stmt)
}

// nornicDBStrictLiveObjectName declares the schema object a statement
// creates, mirroring graphSchemaObjectName: CREATE names the object, CALL
// names the procedure index, DROP always executes.
func nornicDBStrictLiveObjectName(cypher string) (string, bool) {
	fields := strings.Fields(cypher)
	if len(fields) == 0 {
		return "", false
	}
	if strings.EqualFold(fields[0], "CALL") {
		start := strings.Index(cypher, "'")
		if start < 0 {
			return "", false
		}
		rest := cypher[start+1:]
		end := strings.Index(rest, "'")
		if end < 0 {
			return "", false
		}
		return rest[:end], true
	}
	if len(fields) < 3 || !strings.EqualFold(fields[0], "CREATE") {
		return "", false
	}
	return fields[2], true
}

func snapshotNornicDBStrictLiveObjects(t *testing.T, ctx context.Context, driver neo4jdriver.DriverWithContext) map[string]struct{} {
	t.Helper()
	existing := nornicDBStrictLiveNames(runNornicDBStrictLiveCypher(t, ctx, driver,
		fmt.Sprintf("SHOW CONSTRAINTS /* %d */", time.Now().UnixNano()), nil))
	for name := range nornicDBStrictLiveNames(runNornicDBStrictLiveCypher(t, ctx, driver,
		fmt.Sprintf("SHOW INDEXES /* %d */", time.Now().UnixNano()), nil)) {
		existing[name] = struct{}{}
	}
	return existing
}

func openNornicDBStrictLiveDriver(t *testing.T) neo4jdriver.DriverWithContext {
	t.Helper()
	if strings.TrimSpace(os.Getenv(nornicDBStrictSchemaLiveEnv)) != "1" {
		t.Skipf("set %s=1 (and ESHU_NEO4J_URI) to run the NornicDB strict-schema live proof", nornicDBStrictSchemaLiveEnv)
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open NornicDB driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	return driver
}

// runNornicDBStrictLiveCypher runs one statement and returns its rows. A
// repeated SHOW needs a unique comment because NornicDB answers an identical
// repeated SHOW from its result cache.
func runNornicDBStrictLiveCypher(t *testing.T, ctx context.Context, driver neo4jdriver.DriverWithContext, cypher string, params map[string]any) []map[string]any {
	t.Helper()
	rows, err := neo4jdriver.ExecuteQuery(ctx, driver, cypher, params,
		neo4jdriver.EagerResultTransformer, neo4jdriver.ExecuteQueryWithDatabase("nornic"))
	if err != nil {
		t.Fatalf("live NornicDB %q: %v", cypher, err)
	}
	out := make([]map[string]any, 0, len(rows.Records))
	for _, record := range rows.Records {
		row := make(map[string]any, len(record.Keys))
		for i, key := range record.Keys {
			row[key] = record.Values[i]
		}
		out = append(out, row)
	}
	return out
}

func nornicDBStrictLiveNames(rows []map[string]any) map[string]struct{} {
	names := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if name, ok := row["name"].(string); ok {
			names[name] = struct{}{}
		}
	}
	return names
}

// requireNornicDBStrictLiveEndState asserts the D end state: one schema
// object per (label, uid), no retired lookup index left behind.
func requireNornicDBStrictLiveEndState(t *testing.T, ctx context.Context, driver neo4jdriver.DriverWithContext) {
	t.Helper()
	constraints := nornicDBStrictLiveNames(runNornicDBStrictLiveCypher(t, ctx, driver,
		fmt.Sprintf("SHOW CONSTRAINTS /* %d */", time.Now().UnixNano()), nil))
	indexes := nornicDBStrictLiveNames(runNornicDBStrictLiveCypher(t, ctx, driver,
		fmt.Sprintf("SHOW INDEXES /* %d */", time.Now().UnixNano()), nil))
	for _, label := range uidConstraintLabels {
		want := labelToSnake(label) + "_uid_unique"
		if _, ok := constraints[want]; !ok {
			t.Errorf("SHOW CONSTRAINTS lacks %s", want)
		}
		if _, ok := indexes[want]; !ok {
			t.Errorf("SHOW INDEXES lacks constraint-backed index %s", want)
		}
	}
	for name := range indexes {
		if strings.HasPrefix(name, "nornicdb_") && strings.HasSuffix(name, "_uid_lookup") {
			t.Errorf("SHOW INDEXES still carries retired lookup index %s", name)
		}
	}
}

func TestLiveNornicDBStrictSchemaApplyIsClean(t *testing.T) {
	driver := openNornicDBStrictLiveDriver(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	if err := EnsureSchemaWithBackendStrict(ctx, nornicDBStrictLiveExecutor{driver: driver}, logger, SchemaBackendNornicDB); err != nil {
		t.Fatalf("strict NornicDB schema apply: %v", err)
	}
	requireNornicDBStrictLiveEndState(t, ctx, driver)

	// Seed before the second apply so the idempotency proof also guards
	// result duplication: every seeded row must still read exactly once
	// after the re-apply.
	prefix := fmt.Sprintf("11535-%d-", time.Now().UnixNano())
	var uids []string
	for i := 0; i < 3; i++ {
		uids = append(uids, fmt.Sprintf("%s%d", prefix, i))
	}
	for _, uid := range uids {
		runNornicDBStrictLiveCypher(t, ctx, driver,
			`CREATE (:CloudResource {uid: $uid})`, map[string]any{"uid": uid})
	}

	// Idempotent second apply through the production adoption filter:
	// existing objects are skipped, DROP INDEX ... IF EXISTS guards always
	// run.
	filtered := filteredNornicDBStrictLiveExecutor{
		nornicDBStrictLiveExecutor: nornicDBStrictLiveExecutor{driver: driver},
		existing:                   snapshotNornicDBStrictLiveObjects(t, ctx, driver),
	}
	if err := EnsureSchemaWithBackendStrict(ctx, filtered, logger, SchemaBackendNornicDB); err != nil {
		t.Fatalf("strict NornicDB schema second apply: %v", err)
	}
	requireNornicDBStrictLiveEndState(t, ctx, driver)

	for _, uid := range uids {
		rows := runNornicDBStrictLiveCypher(t, ctx, driver,
			`MATCH (n:CloudResource {uid: $uid}) RETURN n.uid AS uid`, map[string]any{"uid": uid})
		if len(rows) != 1 {
			t.Fatalf("equality lookup for %q returned %d rows, want 1", uid, len(rows))
		}
	}
	t.Cleanup(func() {
		runNornicDBStrictLiveCypher(t, context.Background(), driver,
			`MATCH (n:CloudResource) WHERE n.uid STARTS WITH $prefix DETACH DELETE n`,
			map[string]any{"prefix": prefix})
	})
}

// TestLiveNornicDBStrictSchemaApplyRetiresLegacyLookupIndexes proves the
// upgrade path: a store whose pre-D schema apply died mid-way still carries
// the plain nornicdb_<t>_uid_lookup indexes without their constraints, and
// the post-D strict apply drops them before creating the uniqueness
// constraints instead of dying on IndexAlreadyExists.
func TestLiveNornicDBStrictSchemaApplyRetiresLegacyLookupIndexes(t *testing.T) {
	driver := openNornicDBStrictLiveDriver(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Drop the constraints first so the seed models a pre-D store even when
	// this test shares a volume with a previous full apply: on 1.5.x the
	// plain index and the constraint cannot coexist, which is the defect.
	seeded := []string{"Function", "Class", "CloudResource"}
	for _, label := range seeded {
		runNornicDBStrictLiveCypher(t, ctx, driver,
			fmt.Sprintf("DROP CONSTRAINT %s_uid_unique IF EXISTS", labelToSnake(label)), nil)
		legacy := fmt.Sprintf(
			"CREATE INDEX nornicdb_%s_uid_lookup IF NOT EXISTS FOR (n:%s) ON (n.uid)",
			labelToSnake(label), label,
		)
		runNornicDBStrictLiveCypher(t, ctx, driver, legacy, nil)
	}
	before := nornicDBStrictLiveNames(runNornicDBStrictLiveCypher(t, ctx, driver,
		fmt.Sprintf("SHOW INDEXES /* %d */", time.Now().UnixNano()), nil))
	for _, label := range seeded {
		if _, ok := before["nornicdb_"+labelToSnake(label)+"_uid_lookup"]; !ok {
			t.Fatalf("legacy seed lacks nornicdb_%s_uid_lookup", labelToSnake(label))
		}
	}

	// Apply through the production adoption filter so the proof is
	// order-independent: a companion run may already have created the
	// fulltext indexes, which the procedure form cannot re-create.
	filtered := filteredNornicDBStrictLiveExecutor{
		nornicDBStrictLiveExecutor: nornicDBStrictLiveExecutor{driver: driver},
		existing:                   snapshotNornicDBStrictLiveObjects(t, ctx, driver),
	}
	if err := EnsureSchemaWithBackendStrict(ctx, filtered, logger, SchemaBackendNornicDB); err != nil {
		t.Fatalf("strict NornicDB schema apply over legacy indexes: %v", err)
	}
	requireNornicDBStrictLiveEndState(t, ctx, driver)
}
