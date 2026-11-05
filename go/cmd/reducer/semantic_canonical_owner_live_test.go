// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"os"
	"strings"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/content"
	eshugraph "github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/semantic"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// semanticOwnerCase fixes one repository holding one Function that moves
// from line 10 to line 20 between two generations, plus one semantic-only
// Variable. Content-entity ids hash the start line, so the moved Function
// gets a new uid and its prior node is stale, the #10110 delta-reader shape.
const (
	semanticOwnerRepoID   = "repository:r_10110_semantic_owner"
	semanticOwnerRepoPath = "/eshu-test/10110-semantic-owner"
	semanticOwnerRelPath  = "app/moved.go"
	semanticOwnerFunction = "deltaReaderMovedCaller"
	semanticOwnerVariable = "deltaReaderLedger"
)

func semanticOwnerFilePath() string { return semanticOwnerRepoPath + "/" + semanticOwnerRelPath }

func semanticOwnerFunctionUID(line int) string {
	return content.CanonicalEntityID(semanticOwnerRepoID, semanticOwnerRelPath, "Function", semanticOwnerFunction, line)
}

func semanticOwnerVariableUID() string {
	return content.CanonicalEntityID(semanticOwnerRepoID, semanticOwnerRelPath, "Variable", semanticOwnerVariable, 3)
}

// semanticOwnerMaterialization is one canonical generation: the repository,
// its one file, and the Function at line.
func semanticOwnerMaterialization(generation string, line int, first, delta bool) canonical.CanonicalMaterialization {
	mat := canonical.CanonicalMaterialization{
		ScopeID:         "git-repository-scope:" + semanticOwnerRepoID,
		GenerationID:    generation,
		RepoID:          semanticOwnerRepoID,
		RepoPath:        semanticOwnerRepoPath,
		FirstGeneration: first,
		Repository: &canonical.RepositoryRow{
			RepoID: semanticOwnerRepoID, Name: "10110-semantic-owner",
			Path: semanticOwnerRepoPath, LocalPath: semanticOwnerRepoPath,
		},
		Directories: []canonical.DirectoryRow{{
			Path: semanticOwnerRepoPath + "/app", Name: "app", ParentPath: semanticOwnerRepoPath,
			RepoID: semanticOwnerRepoID, Depth: 0,
		}},
		Files: []canonical.FileRow{{
			Path: semanticOwnerFilePath(), RelativePath: semanticOwnerRelPath, Name: "moved.go",
			Language: "go", RepoID: semanticOwnerRepoID, DirPath: semanticOwnerRepoPath + "/app",
		}},
		Entities: []canonical.EntityRow{{
			EntityID: semanticOwnerFunctionUID(line), Label: "Function", EntityName: semanticOwnerFunction,
			FilePath: semanticOwnerFilePath(), RelativePath: semanticOwnerRelPath,
			StartLine: line, EndLine: line + 5, Language: "go", RepoID: semanticOwnerRepoID,
		}},
	}
	if delta {
		mat.DeltaProjection = true
		mat.DeltaFilePaths = []string{semanticOwnerFilePath()}
	}
	return mat
}

// semanticOwnerWrite is the semantic materialization for the generation whose
// Function sits at line: the Function enrichment plus the semantic-only
// Variable.
func semanticOwnerWrite(line int, delta bool) semantic.EntityWrite {
	write := semantic.EntityWrite{
		RepoIDs: []string{semanticOwnerRepoID},
		Rows: []semantic.EntityRow{
			{
				RepoID: semanticOwnerRepoID, EntityID: semanticOwnerFunctionUID(line), EntityType: "Function",
				EntityName: semanticOwnerFunction, FilePath: semanticOwnerFilePath(), RelativePath: semanticOwnerRelPath,
				Language: "go", StartLine: line, EndLine: line + 5,
				Metadata: map[string]any{"docstring": "moves between generations"},
			},
			{
				RepoID: semanticOwnerRepoID, EntityID: semanticOwnerVariableUID(), EntityType: "Variable",
				EntityName: semanticOwnerVariable, FilePath: semanticOwnerFilePath(), RelativePath: semanticOwnerRelPath,
				Language: "go", StartLine: 3, EndLine: 3,
			},
		},
	}
	if delta {
		write.DeltaProjection = true
		write.DeltaFilePaths = []string{semanticOwnerFilePath()}
	}
	return write
}

// semanticOwnerSchemaExecutor adapts the production session runner to
// graph.CypherExecutor so the test applies the real Neo4j schema.
type semanticOwnerSchemaExecutor struct{ runner neo4jSessionRunner }

func (e semanticOwnerSchemaExecutor) ExecuteCypher(ctx context.Context, stmt eshugraph.CypherStatement) error {
	return e.runner.RunCypher(ctx, stmt.Cypher, stmt.Parameters)
}

// TestLiveNeo4jSemanticWriterKeepsCanonicalFunctionOwnership is the #10110
// regression. The canonical projector owns Function nodes and their File
// CONTAINS edges and retracts a moved function's prior node by
// evidence_source = 'projector/canonical'. The reducer's Neo4j-wired
// semantic writer must enrich those nodes without re-stamping them, so the
// next generation's entity retract still deletes the stale node; on NornicDB
// the canonical-node-rows writer already did. It drives the production
// canonical writer and the production Neo4j semantic writer wiring over a
// delta that moves the function and over a full regeneration that re-keys it.
//
// Gate: ESHU_CYPHER_BOLT_DSN pointing at a disposable Neo4j (auth disabled)
// and ESHU_CYPHER_BOLT_DATABASE=neo4j, the #7095 live-test gate.
func TestLiveNeo4jSemanticWriterKeepsCanonicalFunctionOwnership(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DATABASE")) != "neo4j" {
		t.Skip("ESHU_CYPHER_BOLT_DATABASE=neo4j required; this proof is Neo4j-only")
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_CYPHER_BOLT_DSN"))
	if dsn == "" {
		t.Skip("ESHU_CYPHER_BOLT_DSN not set")
	}
	ctx := context.Background()
	driver, err := neo4jdriver.NewDriverWithContext(dsn, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	runner := neo4jSessionRunner{Driver: driver, DatabaseName: "neo4j"}
	if err := eshugraph.EnsureSchemaWithBackend(ctx, semanticOwnerSchemaExecutor{runner: runner}, nil, eshugraph.SchemaBackendNeo4j); err != nil {
		t.Fatalf("apply neo4j schema: %v", err)
	}

	executor := newReducerNeo4jExecutor(runner, nil)
	canonicalWriter := sourcecypher.NewCanonicalNodeWriter(executor, 100, nil)
	semanticWriter, err := semanticEntityWriterForGraphBackend(
		semanticEntityExecutorForGraphBackend(executor, runtimecfg.GraphBackendNeo4j, 0, false),
		100, runtimecfg.GraphBackendNeo4j, func(string) string { return "" }, nil,
	)
	if err != nil {
		t.Fatalf("semanticEntityWriterForGraphBackend(neo4j): %v", err)
	}

	tests := []struct {
		name  string
		delta bool
	}{
		{name: "delta moves the function", delta: true},
		{name: "full regeneration re-keys the function", delta: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := func() {
				_ = runner.RunCypher(ctx,
					`MATCH (n) WHERE n.repo_id = $repo_id OR n.id = $repo_id OR n.path STARTS WITH $root DETACH DELETE n`,
					map[string]any{"repo_id": semanticOwnerRepoID, "root": semanticOwnerRepoPath})
			}
			cleanup()
			t.Cleanup(cleanup)

			if err := canonicalWriter.Write(ctx, semanticOwnerMaterialization("gen-10110-1", 10, true, false)); err != nil {
				t.Fatalf("generation 1 canonical write: %v", err)
			}
			// A foreign edge on the moved caller, as the code-call projection
			// leaves it: it keeps a stale node from being reaped as edgeless,
			// which is how the stale node outlived the delta in CI run
			// 41953841855 and kept the extra CALLS edge.
			if err := runner.RunCypher(ctx, `MATCH (fn:Function {uid: $uid})
MERGE (callee:SemanticOwnerCallee {uid: $callee})
SET callee.repo_id = $repo_id
MERGE (fn)-[:CALLS]->(callee)`, map[string]any{
				"uid": semanticOwnerFunctionUID(10), "callee": semanticOwnerRepoID + ":callee", "repo_id": semanticOwnerRepoID,
			}); err != nil {
				t.Fatalf("seed CALLS edge: %v", err)
			}
			if _, err := semanticWriter.WriteSemanticEntities(ctx, semanticOwnerWrite(10, false)); err != nil {
				t.Fatalf("generation 1 semantic write: %v", err)
			}
			if err := canonicalWriter.Write(ctx, semanticOwnerMaterialization("gen-10110-2", 20, false, tt.delta)); err != nil {
				t.Fatalf("generation 2 canonical write: %v", err)
			}
			// The node read the #10110 ruling names: the predicate that
			// decides the canonical entity retract is the stale node's
			// evidence_source.
			afterCanonical := readSemanticOwnerFunctions(ctx, t, driver)
			for _, row := range afterCanonical {
				t.Logf("after generation 2 canonical write: Function %v", row)
			}
			if len(afterCanonical) != 1 || afterCanonical[0]["uid"] != semanticOwnerFunctionUID(20) {
				t.Fatalf("Function nodes after the generation 2 canonical write: got %d (%v), want only uid %s",
					len(afterCanonical), afterCanonical, semanticOwnerFunctionUID(20))
			}
			if _, err := semanticWriter.WriteSemanticEntities(ctx, semanticOwnerWrite(20, tt.delta)); err != nil {
				t.Fatalf("generation 2 semantic write: %v", err)
			}

			functions := readSemanticOwnerFunctions(ctx, t, driver)
			if len(functions) != 1 {
				t.Fatalf("Function nodes after generation 2: got %d (%v), want exactly 1", len(functions), functions)
			}
			fn := functions[0]
			if fn["uid"] != semanticOwnerFunctionUID(20) || fn["gen"] != "gen-10110-2" ||
				fn["evidence_source"] != "projector/canonical" {
				t.Fatalf("Function node = %v, want uid=%s gen=gen-10110-2 evidence_source=projector/canonical",
					fn, semanticOwnerFunctionUID(20))
			}
			if fn["docstring"] != "moves between generations" {
				t.Fatalf("Function docstring = %v, want the semantic enrichment", fn["docstring"])
			}
			if fn["contains_source"] != "projector/canonical" {
				t.Fatalf("File-CONTAINS->Function evidence_source = %v, want the canonical edge", fn["contains_source"])
			}

			variables := runSemanticOwnerRead(ctx, t, driver,
				`MATCH (n:Variable) WHERE n.repo_id = $repo_id
OPTIONAL MATCH (:File {path: $path})-[rel:CONTAINS]->(n)
RETURN n.uid AS uid, n.evidence_source AS evidence_source, rel.evidence_source AS contains_source`)
			if len(variables) != 1 || variables[0]["uid"] != semanticOwnerVariableUID() ||
				variables[0]["evidence_source"] != "parser/semantic-entities" ||
				variables[0]["contains_source"] != "parser/semantic-entities" {
				t.Fatalf("Variable nodes = %v, want one semantic-owned Variable with its semantic CONTAINS edge", variables)
			}
		})
	}
}

func readSemanticOwnerFunctions(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext) []map[string]any {
	t.Helper()
	return runSemanticOwnerRead(ctx, t, driver,
		`MATCH (n:Function) WHERE n.repo_id = $repo_id
OPTIONAL MATCH (:File {path: $path})-[rel:CONTAINS]->(n)
RETURN n.uid AS uid, n.generation_id AS gen, n.evidence_source AS evidence_source,
       n.path AS path, n.docstring AS docstring, rel.evidence_source AS contains_source
ORDER BY uid`)
}

func runSemanticOwnerRead(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext, cypher string) []map[string]any {
	t.Helper()
	result, err := neo4jdriver.ExecuteQuery(ctx, driver, cypher,
		map[string]any{"repo_id": semanticOwnerRepoID, "path": semanticOwnerFilePath()},
		neo4jdriver.EagerResultTransformer, neo4jdriver.ExecuteQueryWithDatabase("neo4j"))
	if err != nil {
		t.Fatalf("read %q: %v", cypher, err)
	}
	rows := make([]map[string]any, 0, len(result.Records))
	for _, record := range result.Records {
		rows = append(rows, record.AsMap())
	}
	return rows
}
