// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
)

const (
	indexKeyLiveRepoID      = "repo-7058-indexkey"
	indexKeyLiveRepoPath    = "/eshu-test/7058-indexkey"
	indexKeyLiveFilePath    = "/eshu-test/7058-indexkey/a.ts"
	indexKeyLiveModule      = "react-7058-indexkey"
	indexKeyLiveBigPrefix   = "oversized-7058-indexkey:"
	indexKeyLiveBigBytes    = 32473 // the Module.name size from the #7058 trial
	indexKeyLiveFunctionOK  = "render-7058-indexkey"
	indexKeyLiveLimitPrefix = "atlimit-7058-indexkey:"
)

// indexKeyAtomicExecutor runs a whole statement group in ONE managed write
// transaction, the same shape as cmd/ingester's ingesterNeo4jExecutor
// ExecuteGroup. The boltTestExecutor used by other live tests commits each
// statement separately, which would hide the atomic all-or-nothing failure
// this test exists to exercise.
type indexKeyAtomicExecutor struct {
	runner *boltRetractTestRunner
}

func (e indexKeyAtomicExecutor) Execute(ctx context.Context, stmt Statement) error {
	return e.ExecuteGroup(ctx, []Statement{stmt})
}

func (e indexKeyAtomicExecutor) ExecuteGroup(ctx context.Context, stmts []Statement) error {
	session := e.runner.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.runner.databaseName,
	})
	defer func() { _ = session.Close(ctx) }()
	_, err := session.ExecuteWrite(ctx, func(tx neo4jdriver.ManagedTransaction) (any, error) {
		for _, stmt := range stmts {
			result, err := tx.Run(ctx, stmt.Cypher, stmt.Parameters)
			if err != nil {
				return nil, err
			}
			if _, err := result.Consume(ctx); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	return err
}

// TestLiveCanonicalWriteSkipsOversizedIndexKeys proves, on a real backend with
// the production schema for the touched labels, that one oversized indexed
// value no longer fails the repository's atomic canonical write (#7058).
//
// It first shows the backend rejects the raw Module row on its own (the
// failure the trial hit), then drives CanonicalNodeWriter.Write through one
// atomic transaction with that row, an oversized Function name, and normal
// rows, and checks every normal node and edge landed while neither oversized
// value did.
//
// Gate: ESHU_CYPHER_BOLT_DSN must point at a Neo4j backend (the rejection step
// is Neo4j-only; set ESHU_CYPHER_BOLT_DATABASE=neo4j). When unset the test
// skips.
func TestLiveCanonicalWriteSkipsOversizedIndexKeys(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	cleanup := func() {
		_ = boltWriteStatement(ctx, runner,
			`MATCH (m:Module) WHERE m.name = $name OR m.name STARTS WITH $prefix OR m.name STARTS WITH $limit DETACH DELETE m`,
			map[string]any{"name": indexKeyLiveModule, "prefix": indexKeyLiveBigPrefix, "limit": indexKeyLiveLimitPrefix})
		_ = boltWriteStatement(ctx, runner,
			`MATCH (n) WHERE n.path STARTS WITH $root OR n.id = $repo DETACH DELETE n`,
			map[string]any{"root": indexKeyLiveRepoPath, "repo": indexKeyLiveRepoID})
	}
	cleanup()
	t.Cleanup(cleanup)

	statements, err := graph.SchemaStatementsForBackend(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("schema statements: %v", err)
	}
	for _, stmt := range statements {
		if strings.HasPrefix(stmt, "CALL") {
			continue // fulltext procedure form; not needed for this proof
		}
		for _, label := range []string{"(m:Module)", "(f:Function)", "(f:File)", "(r:Repository)", "(n:Function)"} {
			if strings.Contains(stmt, label) {
				if err := boltWriteStatement(ctx, runner, stmt, nil); err != nil {
					t.Fatalf("apply schema %q: %v", stmt, err)
				}
			}
		}
	}
	if err := boltWriteStatement(ctx, runner, "CALL db.awaitIndexes(60)", nil); err != nil {
		t.Fatalf("await indexes: %v", err)
	}

	bigModule := indexKeyLiveBigPrefix + strings.Repeat("x", indexKeyLiveBigBytes-len(indexKeyLiveBigPrefix))
	bigFunction := indexKeyLiveBigPrefix + strings.Repeat("f", canonical.MaxIndexedKeyBytes)
	// Rows exactly at the bound must still be written: the guard's limit sits
	// under the backend's, for a single-string key and for the
	// (name, path, line_number) node key.
	limitModule := indexKeyLiveLimitPrefix + strings.Repeat("m", canonical.MaxIndexedKeyBytes-len(indexKeyLiveLimitPrefix))
	limitFunction := indexKeyLiveLimitPrefix +
		strings.Repeat("l", canonical.MaxIndexedKeyBytes-len(indexKeyLiveLimitPrefix)-len(indexKeyLiveFilePath))

	// Precondition: the backend rejects the unguarded Module row. Without this
	// the rest of the test could pass on a backend that never had the problem.
	rawErr := boltWriteStatement(ctx, runner, canonicalNodeModuleUpsertCypher,
		map[string]any{"rows": []map[string]any{{"name": bigModule, "language": "typescript"}}})
	if rawErr == nil || !strings.Contains(rawErr.Error(), "too large to index") {
		t.Fatalf("raw oversized Module upsert error = %v, want the backend's 'too large to index' rejection", rawErr)
	}

	writer := NewCanonicalNodeWriter(indexKeyAtomicExecutor{runner: runner}, 500, nil)
	err = writer.Write(ctx, canonical.CanonicalMaterialization{
		ScopeID:         "scope-7058-indexkey",
		GenerationID:    "gen-7058-indexkey",
		RepoID:          indexKeyLiveRepoID,
		RepoPath:        indexKeyLiveRepoPath,
		FirstGeneration: true,
		Repository:      &canonical.RepositoryRow{RepoID: indexKeyLiveRepoID, Name: "indexkey", Path: indexKeyLiveRepoPath},
		Files: []canonical.FileRow{{
			Path: indexKeyLiveFilePath, RelativePath: "a.ts", Name: "a.ts",
			Language: "typescript", RepoID: indexKeyLiveRepoID, DirPath: indexKeyLiveRepoPath,
		}},
		Entities: []canonical.EntityRow{
			{
				EntityID: "content-entity:e_7058000000a1", Label: "Function", EntityName: indexKeyLiveFunctionOK,
				FilePath: indexKeyLiveFilePath, RelativePath: "a.ts", StartLine: 3, Language: "typescript", RepoID: indexKeyLiveRepoID,
			},
			{
				EntityID: "content-entity:e_7058000000a3", Label: "Function", EntityName: limitFunction,
				FilePath: indexKeyLiveFilePath, RelativePath: "a.ts", StartLine: 20, Language: "typescript", RepoID: indexKeyLiveRepoID,
			},
			{
				EntityID: "content-entity:e_7058000000a2", Label: "Function", EntityName: bigFunction,
				FilePath: indexKeyLiveFilePath, RelativePath: "a.ts", StartLine: 9, Language: "typescript", RepoID: indexKeyLiveRepoID,
			},
		},
		Modules: []canonical.ModuleRow{
			{Name: indexKeyLiveModule, Language: "typescript"},
			{Name: bigModule, Language: "typescript"},
			{Name: limitModule, Language: "typescript"},
		},
		Imports: []canonical.ImportRow{
			{FilePath: indexKeyLiveFilePath, ModuleName: indexKeyLiveModule, ModuleLanguage: "typescript", LineNumber: 1},
			{FilePath: indexKeyLiveFilePath, ModuleName: bigModule, ModuleLanguage: "typescript", LineNumber: 48},
		},
	})
	if err != nil {
		t.Fatalf("Write() error = %v, want nil: one oversized value must not fail the repository", err)
	}

	for _, check := range []struct {
		name   string
		cypher string
		want   int64
	}{
		{"repository", `MATCH (r:Repository {id: $repo}) RETURN count(r) AS count`, 1},
		{"file", `MATCH (f:File {path: $file}) RETURN count(f) AS count`, 1},
		{"normal function", `MATCH (fn:Function {name: $fn}) RETURN count(fn) AS count`, 1},
		{"normal import edge", `MATCH (:File {path: $file})-[:IMPORTS]->(m:Module {name: $module}) RETURN count(m) AS count`, 1},
		{"at-limit module", `MATCH (m:Module) WHERE m.name STARTS WITH $limit RETURN count(m) AS count`, 1},
		{"at-limit function", `MATCH (fn:Function) WHERE fn.name STARTS WITH $limit RETURN count(fn) AS count`, 1},
		{"oversized module", `MATCH (m:Module) WHERE m.name STARTS WITH $prefix RETURN count(m) AS count`, 0},
		{"oversized function", `MATCH (fn:Function) WHERE fn.name STARTS WITH $prefix RETURN count(fn) AS count`, 0},
	} {
		got, err := boltCount(ctx, runner, check.cypher, map[string]any{
			"repo": indexKeyLiveRepoID, "file": indexKeyLiveFilePath, "fn": indexKeyLiveFunctionOK,
			"module": indexKeyLiveModule, "prefix": indexKeyLiveBigPrefix, "limit": indexKeyLiveLimitPrefix,
		})
		if err != nil {
			t.Fatalf("%s count: %v", check.name, err)
		}
		if got != check.want {
			t.Fatalf("%s count = %d, want %d", check.name, got, check.want)
		}
	}
}
