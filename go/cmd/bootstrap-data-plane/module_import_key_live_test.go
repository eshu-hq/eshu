// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// migrationLiveEnv opts in to the migration live test on a developer backend.
// The migration is global by design -- it rewrites every canonical Module in
// the database -- so a DSN alone is not consent: point it only at a disposable
// Neo4j. The live-backend CI runner gives each ledger file a fresh backend and
// exports ESHU_LIVE_GRAPH_BACKEND, which also enables the test.
const migrationLiveEnv = "ESHU_MODULE_IMPORT_KEY_MIGRATION_LIVE"

const migLivePrefix = "mig8168-"

// countingRunner is a search.Runner over one Neo4j driver that totals the
// write counters of every statement it runs, so a test can prove a rerun
// writes nothing.
type countingRunner struct {
	driver   neo4jdriver.DriverWithContext
	database string
	writes   int
}

func (r *countingRunner) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite, DatabaseName: r.database})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, err
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		return nil, err
	}
	c := summary.Counters()
	r.writes += c.NodesCreated() + c.NodesDeleted() + c.RelationshipsCreated() + c.RelationshipsDeleted() +
		c.PropertiesSet() + c.LabelsAdded() + c.LabelsRemoved()
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		rows = append(rows, record.AsMap())
	}
	return rows, nil
}

func (r *countingRunner) scalar(ctx context.Context, t *testing.T, cypher string, params map[string]any) int64 {
	t.Helper()
	rows, err := r.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("%s: %v", cypher, err)
	}
	return firstRowInt(rows, "n")
}

// TestLiveModuleImportKeyMigrationCollapsesKeysAndRepoints seeds a graph the
// way a pre-#8168 release leaves it and runs the bootstrap migration twice:
//   - three canonical duplicates of one (name, lang), each imported by its own
//     file, plus one file importing two of them;
//   - a lang-less legacy node and an empty-lang node for one name;
//   - a uid-bearing (semantic) node the old canonical MERGE adopted, with an
//     IMPORTS edge and its own CONTAINS edge;
//   - a semantic node sharing a duplicate's (name, lang), with CONTAINS;
//   - an already-keyed node beside an unkeyed one, as a writer racing an
//     interrupted run would leave.
//
// The first run must leave one keyed canonical node per key with every
// distinct IMPORTS edge and its properties, leave both semantic nodes and
// their CONTAINS edges untouched, and keep the constraint; the second run must
// write nothing.
func TestLiveModuleImportKeyMigrationCollapsesKeysAndRepoints(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DSN"))
	database := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DATABASE"))
	switch liveBackend := strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_BACKEND")); {
	case liveBackend != "":
		// The live-backend CI runner: a fresh backend per ledger file.
		if liveBackend != "neo4j" {
			t.Skipf("ESHU_LIVE_GRAPH_BACKEND = %q; the migration runs on Neo4j only", liveBackend)
		}
		dsn = strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
		database = strings.TrimSpace(os.Getenv("ESHU_LIVE_GRAPH_DATABASE"))
	case os.Getenv(migrationLiveEnv) != "1":
		t.Skipf("set %s=1 and ESHU_CYPHER_BOLT_DSN to a disposable Neo4j to run", migrationLiveEnv)
	}
	if dsn == "" {
		t.Skip("no Neo4j URI set")
	}
	if database == "" {
		database = "neo4j"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	driver, err := neo4jdriver.NewDriverWithContext(dsn, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	// Cleanups run last-registered first, so the fixture cleanup below runs
	// before the driver closes and the context is cancelled.
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	runner := &countingRunner{driver: driver, database: database}
	exec := func(cypher string, params map[string]any) {
		t.Helper()
		if _, err := runner.Run(ctx, cypher, params); err != nil {
			t.Fatalf("%s: %v", cypher, err)
		}
	}
	cleanup := func() {
		exec(`MATCH (n) WHERE n.name STARTS WITH $p OR n.path STARTS WITH $p OR n.uid STARTS WITH $p DETACH DELETE n`,
			map[string]any{"p": migLivePrefix})
	}
	cleanup()
	t.Cleanup(cleanup)

	// The graph a pre-#8168 release leaves: no import_key constraint.
	exec("DROP CONSTRAINT module_import_key IF EXISTS", nil)
	exec(`UNWIND range(1, 9) AS i CREATE (:File {path: $p + 'f' + i})`, map[string]any{"p": migLivePrefix})
	exec(`CREATE (d1:Module {name: $p + 'dup', lang: 'go', evidence_source: 'projector/canonical'}),
	             (d2:Module {name: $p + 'dup', lang: 'go', evidence_source: 'projector/canonical'}),
	             (d3:Module {name: $p + 'dup', lang: 'go', evidence_source: 'projector/canonical'}),
	             (l1:Module {name: $p + 'legacy'}),
	             (l2:Module {name: $p + 'legacy', lang: '', evidence_source: 'projector/canonical'}),
	             (a:Module {uid: $p + 'u1', name: $p + 'adopted', lang: 'typescript', evidence_source: 'projector/canonical'}),
	             (s:Module {uid: $p + 'u2', name: $p + 'dup', lang: 'go', evidence_source: 'parser/semantic-entities'}),
	             (w:Module {name: $p + 'win', lang: 'go', evidence_source: 'projector/canonical'})
	      WITH d1, d2, d3, l1, l2, a, s, w
	      MATCH (f1:File {path: $p + 'f1'}), (f2:File {path: $p + 'f2'}), (f3:File {path: $p + 'f3'}),
	            (f4:File {path: $p + 'f4'}), (f5:File {path: $p + 'f5'}), (f6:File {path: $p + 'f6'}),
	            (f7:File {path: $p + 'f7'}), (f8:File {path: $p + 'f8'}), (f9:File {path: $p + 'f9'})
	      CREATE (f1)-[:IMPORTS {line_number: 1, source_package: 'p1'}]->(d1),
	             (f1)-[:IMPORTS {line_number: 11, source_package: 'p1'}]->(d2),
	             (f2)-[:IMPORTS {line_number: 2, source_package: 'p2', type_only: true}]->(d2),
	             (f3)-[:IMPORTS {line_number: 3, source_package: 'p3'}]->(d3),
	             (f4)-[:IMPORTS {line_number: 4}]->(l1),
	             (f5)-[:IMPORTS {line_number: 5}]->(l2),
	             (f6)-[:IMPORTS {line_number: 6, imported_name: 'x'}]->(a),
	             (f7)-[:CONTAINS]->(a),
	             (f8)-[:CONTAINS]->(s),
	             (f9)-[:IMPORTS {line_number: 9}]->(w)`,
		map[string]any{"p": migLivePrefix})

	// Bootstrap order: the DDL first, then the migration. A node keyed after
	// the DDL, beside the unkeyed 'win' node, is what a racing writer leaves.
	exec("CREATE CONSTRAINT module_import_key IF NOT EXISTS FOR (m:Module) REQUIRE m.import_key IS UNIQUE", nil)
	exec(`CREATE (:Module {name: $p + 'win', lang: 'go', import_key: $p + 'win@go', evidence_source: 'projector/canonical'})`,
		map[string]any{"p": migLivePrefix})

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := migrateModuleImportKeys(ctx, runner, logger); err != nil {
		t.Fatalf("first migration: %v", err)
	}

	for _, tc := range []struct {
		name, lang string
		imports    int64
	}{
		{migLivePrefix + "dup", "go", 3},
		{migLivePrefix + "legacy", "", 2},
		{migLivePrefix + "adopted", "typescript", 1},
		{migLivePrefix + "win", "go", 1},
	} {
		key := tc.name + "@" + tc.lang
		if n := runner.scalar(ctx, t, `MATCH (m:Module {name: $name}) WHERE m.uid IS NULL AND coalesce(m.lang, '') = $lang RETURN count(m) AS n`,
			map[string]any{"name": tc.name, "lang": tc.lang}); n != 1 {
			t.Errorf("%s: %d canonical nodes, want 1", key, n)
		}
		if n := runner.scalar(ctx, t, `MATCH (m:Module {import_key: $key}) WHERE m.uid IS NULL RETURN count(m) AS n`,
			map[string]any{"key": key}); n != 1 {
			t.Errorf("%s: %d nodes carry the key, want 1", key, n)
		}
		if n := runner.scalar(ctx, t, `MATCH (:File)-[r:IMPORTS]->(m:Module {import_key: $key}) RETURN count(r) AS n`,
			map[string]any{"key": key}); n != tc.imports {
			t.Errorf("%s: %d IMPORTS edges, want %d", key, n, tc.imports)
		}
	}
	for _, tc := range []struct{ file, check string }{
		{"f2", `r.line_number = 2 AND r.source_package = 'p2' AND r.type_only = true AND m.import_key = $p + 'dup@go'`},
		{"f3", `r.line_number = 3 AND r.source_package = 'p3' AND m.import_key = $p + 'dup@go'`},
		{"f6", `r.line_number = 6 AND r.imported_name = 'x' AND m.import_key = $p + 'adopted@typescript'`},
		{"f9", `r.line_number = 9 AND m.import_key = $p + 'win@go'`},
	} {
		if n := runner.scalar(ctx, t, `MATCH (f:File {path: $path})-[r:IMPORTS]->(m:Module) WHERE `+tc.check+` RETURN count(r) AS n`,
			map[string]any{"path": migLivePrefix + tc.file, "p": migLivePrefix}); n != 1 {
			t.Errorf("%s: IMPORTS edge properties not preserved on the canonical node", tc.file)
		}
	}
	for _, uid := range []string{"u1", "u2"} {
		if n := runner.scalar(ctx, t, `MATCH (:File)-[:CONTAINS]->(m:Module {uid: $uid}) WHERE m.import_key IS NULL
		      AND NOT EXISTS { (m)<-[:IMPORTS]-() } RETURN count(m) AS n`, map[string]any{"uid": migLivePrefix + uid}); n != 1 {
			t.Errorf("semantic node %s: CONTAINS lost, keyed, or still imported", uid)
		}
	}
	if n := runner.scalar(ctx, t, `MATCH (m:Module {uid: $uid}) WHERE m.evidence_source = 'parser/semantic-entities'
	      AND m.name = $name AND m.lang = 'go' RETURN count(m) AS n`,
		map[string]any{"uid": migLivePrefix + "u2", "name": migLivePrefix + "dup"}); n != 1 {
		t.Error("semantic node u2 properties changed")
	}
	if n := runner.scalar(ctx, t, `SHOW CONSTRAINTS YIELD name WHERE name = 'module_import_key' RETURN count(*) AS n`, nil); n != 1 {
		t.Errorf("module_import_key constraint count = %d, want 1", n)
	}

	runner.writes = 0
	if err := migrateModuleImportKeys(ctx, runner, logger); err != nil {
		t.Fatalf("second migration: %v", err)
	}
	if runner.writes != 0 {
		t.Fatalf("second migration wrote %d counters, want 0: the migration must be idempotent", runner.writes)
	}
}
