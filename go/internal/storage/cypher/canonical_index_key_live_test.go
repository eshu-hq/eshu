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
	indexKeyLiveMidPrefix   = "mid-7058-indexkey:"
	indexKeyLiveUIDPrefix   = "uid-7058-indexkey:"
	// indexKeyLiveOverBytes is above both Neo4j limits measured for #7058:
	// 8164 bytes for a single string key and 8151 for (name, path, line).
	indexKeyLiveOverBytes = 9000
	// indexKeyLiveMidKeyBytes sits between the guard's bound and Neo4j's
	// composite limit: Neo4j would accept it, the guard skips it on every
	// backend so graph truth does not depend on the backend.
	indexKeyLiveMidKeyBytes = 8100
)

// indexKeyLiveSchemaLabels are the labels whose production schema the test
// applies: every label the canonical and tfstate writes below MERGE with an
// indexed value. (Function and Annotation stay listed: canonical entities
// still MERGE Functions here, and the semantic/entity leaf's writer covers
// the semantic labels against the same schema.)
var indexKeyLiveSchemaLabels = []string{
	"Module", "Function", "File", "Repository", "Directory", "Annotation",
	"TerraformStateResource", "TerraformModule",
}

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

// TestLiveCanonicalWriteSkipsOversizedIndexKeys proves, on a real Neo4j
// backend with the production schema for the touched labels, that one
// oversized indexed value no longer fails an atomic graph write (#7058).
//
// It first shows the backend rejects the raw Module row on its own (the
// failure the trial hit, and review F1's one stage later). It then drives,
// through InstrumentedExecutor as production does, the canonical write (code
// entities, Terraform state resources and modules) with 9000-byte values, a
// value between the guard bound and Neo4j's limit, and normal rows, for two
// generations. Every normal node and edge must land and no oversized or
// mid-range value may. The reducer's semantic-entity writer moved to the
// semantic/entity leaf (#6694); its oversized-index proof is the always-on
// TestSemanticEntityWriterSkipsOversizedIndexKeysInEveryWriteMode in that
// leaf, which drives the production writer (all five write modes) over the
// same oversized shapes.
//
// Gate: ESHU_CYPHER_BOLT_DSN must point at a Neo4j backend (the rejection step
// is Neo4j-only; set ESHU_CYPHER_BOLT_DATABASE=neo4j). openBoltTestRunner
// connects with no authentication, so start Neo4j with NEO4J_AUTH=none; a
// server that requires a password fails with Neo.ClientError.Security.Unauthorized.
// When ESHU_CYPHER_BOLT_DSN is unset the test skips.
func TestLiveCanonicalWriteSkipsOversizedIndexKeys(t *testing.T) {
	runner := openBoltTestRunner(t)
	t.Cleanup(func() { runner.close(context.Background()) })
	ctx := context.Background()

	cleanup := func() {
		_ = boltWriteStatement(ctx, runner,
			`MATCH (m:Module) WHERE m.name = $name OR m.name STARTS WITH $prefix OR m.name STARTS WITH $limit DETACH DELETE m`,
			map[string]any{"name": indexKeyLiveModule, "prefix": indexKeyLiveBigPrefix, "limit": indexKeyLiveLimitPrefix})
		_ = boltWriteStatement(ctx, runner,
			`MATCH (n) WHERE n.path STARTS WITH $root OR n.id = $repo OR n.uid STARTS WITH $uid DETACH DELETE n`,
			map[string]any{"root": indexKeyLiveRepoPath, "repo": indexKeyLiveRepoID, "uid": indexKeyLiveUIDPrefix})
	}
	cleanup()
	t.Cleanup(cleanup)
	applyIndexKeyLiveSchema(ctx, t, runner)

	bigModule := indexKeyLiveBigPrefix + strings.Repeat("x", indexKeyLiveBigBytes-len(indexKeyLiveBigPrefix))
	bigName := indexKeyLiveBigPrefix + strings.Repeat("f", indexKeyLiveOverBytes-len(indexKeyLiveBigPrefix))
	midName := indexKeyLiveMidPrefix + strings.Repeat("m", indexKeyLiveMidKeyBytes-len(indexKeyLiveMidPrefix)-len(indexKeyLiveFilePath))

	// Preconditions: the backend rejects the unguarded rows. Without these the
	// rest of the test could pass on a backend that never had the problem.
	rawErr := boltWriteStatement(ctx, runner, canonicalNodeModuleUpsertCypher,
		map[string]any{"rows": CanonicalModuleRows([]canonical.ModuleRow{{Name: bigModule, Language: "typescript"}})})
	if rawErr == nil || !strings.Contains(rawErr.Error(), "too large to index") {
		t.Fatalf("raw oversized Module upsert error = %v, want the backend's 'too large to index' rejection", rawErr)
	}

	exec := &InstrumentedExecutor{Inner: indexKeyAtomicExecutor{runner: runner}}
	canonicalWriter := NewCanonicalNodeWriter(exec, 500, nil)

	for gen, generation := range []string{"gen-7058-indexkey-1", "gen-7058-indexkey-2"} {
		mat := indexKeyLiveMaterialization(generation, gen == 0, bigModule, bigName, midName)
		if err := canonicalWriter.Write(ctx, mat); err != nil {
			t.Fatalf("%s canonical Write() error = %v, want nil: one oversized value must not fail the repository", generation, err)
		}
		assertIndexKeyLiveGraph(ctx, t, runner, generation)
	}
}

func applyIndexKeyLiveSchema(ctx context.Context, t *testing.T, runner *boltRetractTestRunner) {
	t.Helper()
	statements, err := graph.SchemaStatementsForBackend(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("schema statements: %v", err)
	}
	for _, stmt := range statements {
		if strings.HasPrefix(stmt, "CALL") {
			continue // fulltext procedure form; not needed for this proof
		}
		compact := strings.ReplaceAll(stmt, " ", "")
		for _, label := range indexKeyLiveSchemaLabels {
			if strings.Contains(compact, ":"+label+")") {
				if err := boltWriteStatement(ctx, runner, stmt, nil); err != nil {
					t.Fatalf("apply schema %q: %v", stmt, err)
				}
				break
			}
		}
	}
	if err := boltWriteStatement(ctx, runner, "CALL db.awaitIndexes(60)", nil); err != nil {
		t.Fatalf("await indexes: %v", err)
	}
}

func indexKeyLiveMaterialization(generation string, first bool, bigModule, bigName, midName string) canonical.CanonicalMaterialization {
	// Exactly at the bound for the Module import_key, name + "@typescript".
	limitModule := indexKeyLiveLimitPrefix +
		strings.Repeat("m", canonical.MaxIndexedKeyBytes-len(indexKeyLiveLimitPrefix)-len("@typescript"))
	// Exactly at the bound for the (name, path, line_number) node key.
	limitFunction := indexKeyLiveLimitPrefix +
		strings.Repeat("l", canonical.MaxIndexedKeyBytes-len(indexKeyLiveLimitPrefix)-len(indexKeyLiveFilePath))
	entity := func(id, name string, line int) canonical.EntityRow {
		return canonical.EntityRow{
			EntityID: id, Label: "Function", EntityName: name, FilePath: indexKeyLiveFilePath,
			RelativePath: "a.ts", StartLine: line, Language: "typescript", RepoID: indexKeyLiveRepoID,
		}
	}
	tfResource := func(uid, address string) canonical.TerraformStateResourceRow {
		return canonical.TerraformStateResourceRow{
			UID: indexKeyLiveUIDPrefix + uid, Address: address, Mode: "managed", ResourceType: "aws_s3_bucket",
			Name: "logs", Lineage: "lineage-7058", Serial: 1, BackendKind: "s3", StatePath: "tfstate://s3/7058",
		}
	}
	return canonical.CanonicalMaterialization{
		ScopeID:         "scope-7058-indexkey",
		GenerationID:    generation,
		RepoID:          indexKeyLiveRepoID,
		RepoPath:        indexKeyLiveRepoPath,
		FirstGeneration: first,
		Repository:      &canonical.RepositoryRow{RepoID: indexKeyLiveRepoID, Name: "indexkey", Path: indexKeyLiveRepoPath},
		Files: []canonical.FileRow{{
			Path: indexKeyLiveFilePath, RelativePath: "a.ts", Name: "a.ts",
			Language: "typescript", RepoID: indexKeyLiveRepoID, DirPath: indexKeyLiveRepoPath,
		}},
		Entities: []canonical.EntityRow{
			entity("content-entity:e_7058000000a1", indexKeyLiveFunctionOK, 3),
			entity("content-entity:e_7058000000a3", limitFunction, 20),
			entity("content-entity:e_7058000000a2", bigName, 9),
			entity("content-entity:e_7058000000a4", midName, 30),
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
		TerraformStateResources: []canonical.TerraformStateResourceRow{
			tfResource("tf-ok", "aws_s3_bucket.logs"),
			tfResource("tf-big", indexKeyLiveBigPrefix+strings.Repeat("a", indexKeyLiveOverBytes)),
		},
		TerraformStateModules: []canonical.TerraformStateModuleRow{
			{UID: indexKeyLiveUIDPrefix + "mod-ok", ModuleAddress: "module.app", Lineage: "lineage-7058", StatePath: "tfstate://s3/7058"},
			{UID: indexKeyLiveUIDPrefix + "mod-big", ModuleAddress: indexKeyLiveBigPrefix + strings.Repeat("a", indexKeyLiveOverBytes), Lineage: "lineage-7058", StatePath: "tfstate://s3/7058"},
		},
	}
}

func assertIndexKeyLiveGraph(ctx context.Context, t *testing.T, runner *boltRetractTestRunner, generation string) {
	t.Helper()
	for _, check := range []struct {
		name   string
		cypher string
		want   int64
	}{
		{"repository", `MATCH (r:Repository {id: $repo}) RETURN count(r) AS count`, 1},
		{"file", `MATCH (f:File {path: $file}) RETURN count(f) AS count`, 1},
		{"normal function", `MATCH (fn:Function {name: $fn}) RETURN count(fn) AS count`, 1},
		{"normal function generation", `MATCH (fn:Function {name: $fn}) WHERE fn.generation_id = $gen RETURN count(fn) AS count`, 1},
		{"normal import edge", `MATCH (:File {path: $file})-[:IMPORTS]->(m:Module {name: $module}) RETURN count(m) AS count`, 1},
		{"at-limit module", `MATCH (m:Module) WHERE m.name STARTS WITH $limit RETURN count(m) AS count`, 1},
		{"at-limit function", `MATCH (fn:Function) WHERE fn.name STARTS WITH $limit RETURN count(fn) AS count`, 1},
		{"normal tfstate resource", `MATCH (r:TerraformStateResource {uid: $uid + 'tf-ok'}) RETURN count(r) AS count`, 1},
		{"normal tfstate module", `MATCH (m:TerraformModule {uid: $uid + 'mod-ok'}) RETURN count(m) AS count`, 1},
		{"oversized module", `MATCH (m:Module) WHERE m.name STARTS WITH $prefix RETURN count(m) AS count`, 0},
		{"oversized or mid function", `MATCH (fn:Function) WHERE fn.name STARTS WITH $prefix OR fn.name STARTS WITH $mid RETURN count(fn) AS count`, 0},
		{"oversized tfstate", `MATCH (n) WHERE n.uid IN [$uid + 'tf-big', $uid + 'mod-big'] RETURN count(n) AS count`, 0},
	} {
		got, err := boltCount(ctx, runner, check.cypher, map[string]any{
			"repo": indexKeyLiveRepoID, "file": indexKeyLiveFilePath, "fn": indexKeyLiveFunctionOK,
			"module": indexKeyLiveModule, "prefix": indexKeyLiveBigPrefix, "limit": indexKeyLiveLimitPrefix,
			"mid": indexKeyLiveMidPrefix, "uid": indexKeyLiveUIDPrefix, "gen": generation,
		})
		if err != nil {
			t.Fatalf("%s: %s count: %v", generation, check.name, err)
		}
		if got != check.want {
			t.Fatalf("%s: %s count = %d, want %d", generation, check.name, got, check.want)
		}
	}
}
