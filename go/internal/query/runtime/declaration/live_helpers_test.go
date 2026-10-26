// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_answer_truth

package declaration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/writer"
)

// The live proofs in this package run the production handler over the
// production RuntimeDeclaration schema and upsert Cypher (#8816) on the pinned
// Neo4j (#8817). Locally:
//
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:<port> ESHU_LIVE_GRAPH_BACKEND=neo4j \
//	  go test -tags live_nornicdb_answer_truth ./internal/query/runtime/declaration \
//	  -run '^TestLive' -count=1 -v
//
// The reader below stands in for the API's Neo4jReader: one read session per
// statement, records mapped to row maps. It adds no read policy of its own.

type liveReader struct{ driver neo4jdriver.DriverWithContext }

func (r liveReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{DatabaseName: "neo4j", AccessMode: neo4jdriver.AccessModeRead})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	records, err := result.Collect(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(records))
	for _, record := range records {
		row := make(map[string]any, len(record.Keys))
		for i, key := range record.Keys {
			row[key] = record.Values[i]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (r liveReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// liveStore holds the driver for seeding and the reader the handler uses.
type liveStore struct {
	driver neo4jdriver.DriverWithContext
	reader liveReader
}

func openLiveStore(t *testing.T) liveStore {
	t.Helper()
	uri := os.Getenv("ESHU_NEO4J_URI")
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI must point to a disposable Neo4j store")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify connectivity: %v", err)
	}
	store := liveStore{driver: driver, reader: liveReader{driver: driver}}
	stmts, err := graph.SchemaStatementsForBackend(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("schema statements: %v", err)
	}
	for _, stmt := range stmts {
		if strings.Contains(stmt, ":RuntimeDeclaration)") || strings.Contains(stmt, "CONSTRAINT repository_id ") {
			store.write(t, stmt, nil)
		}
	}
	return store
}

// write runs one auto-commit write statement.
func (s liveStore) write(t *testing.T, cypher string, params map[string]any) {
	t.Helper()
	ctx := context.Background()
	session := s.driver.NewSession(ctx, neo4jdriver.SessionConfig{DatabaseName: "neo4j", AccessMode: neo4jdriver.AccessModeWrite})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("write %.60q: %v", cypher, err)
	}
	if _, err := result.Consume(ctx); err != nil {
		t.Fatalf("write %.60q: %v", cypher, err)
	}
}

// seed writes rows through the production upsert statement, in batches.
func (s liveStore) seed(t *testing.T, rows []map[string]any, batch int) {
	t.Helper()
	for start := 0; start < len(rows); start += batch {
		end := min(start+batch, len(rows))
		chunk := make([]map[string]any, 0, end-start)
		for _, row := range rows[start:end] {
			stamped := map[string]any{"generation_id": "live-gen", "evidence_source": "live8817"}
			for k, v := range row {
				stamped[k] = v
			}
			chunk = append(chunk, stamped)
		}
		s.write(t, writer.RuntimeDeclarationUpsertCypher, map[string]any{"rows": chunk})
	}
}

// reset removes every node the live tests create, in small batches: one
// transaction deleting a full perf corpus exceeds the store's transaction
// memory budget (the 250,000-node leftover that proved it).
func (s liveStore) reset(t *testing.T) {
	t.Helper()
	s.deleteBatches(t, `MATCH (d:RuntimeDeclaration {evidence_source: 'live8817'}) WITH d LIMIT 5000 DETACH DELETE d`)
	s.deleteBatches(t, `MATCH (r:Repository) WHERE r.id STARTS WITH 'repository:live8817-' WITH r LIMIT 5000 DETACH DELETE r`)
}

// deleteBatches repeats a LIMIT-bounded delete until it removes nothing. It
// runs on a write session: the reader only opens read sessions.
func (s liveStore) deleteBatches(t *testing.T, cypher string) {
	t.Helper()
	for {
		if s.writeCount(t, cypher+` RETURN count(*) AS deleted`) == 0 {
			return
		}
	}
}

// writeCount runs one auto-commit write statement and returns its deleted
// count.
func (s liveStore) writeCount(t *testing.T, cypher string) int64 {
	t.Helper()
	ctx := context.Background()
	session := s.driver.NewSession(ctx, neo4jdriver.SessionConfig{DatabaseName: "neo4j", AccessMode: neo4jdriver.AccessModeWrite})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		t.Fatalf("write %.60q: %v", cypher, err)
	}
	records, err := result.Collect(ctx)
	if err != nil {
		t.Fatalf("write %.60q: %v", cypher, err)
	}
	if len(records) == 0 {
		return 0
	}
	return deletedCount(records[0].Values[0])
}

func deletedCount(value any) int64 {
	switch n := value.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}

// liveRow builds one upsert row. A nil major leaves every version part null,
// the way the projection does for alias, unresolved and range specs.
func liveRow(repo, path string, line, order int, runtime, sourceKind, raw, specKind, scopeDir string, parts ...int) map[string]any {
	row := map[string]any{
		"repo_id": repo, "relative_path": path, "line_number": line, "order_index": order,
		"runtime": runtime, "source_kind": sourceKind, "raw_spec": raw, "spec_kind": specKind, "scope_dir": scopeDir,
		"tool_name": nil, "version_major": nil, "version_minor": nil, "version_patch": nil, "distribution": nil,
		"range_lower": nil, "range_upper": nil, "range_lower_major": nil, "range_upper_major": nil,
		"range_lower_inclusive": nil, "range_upper_inclusive": nil,
	}
	for i, key := range []string{"version_major", "version_minor", "version_patch"} {
		if i < len(parts) {
			row[key] = parts[i]
		}
	}
	return row
}

func liveRepoID(n int) string { return fmt.Sprintf("repository:live8817-%05d", n) }
