package main

import (
	"context"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/projector"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestNornicDBBatchedFunctionRootKindsCompatibility(t *testing.T) {
	withNornicDBSyntaxDriver(t, func(ctx context.Context, driver neo4jdriver.DriverWithContext) {
		setup := []string{
			"CREATE CONSTRAINT eshu_syntax_root_kind_function_uid IF NOT EXISTS FOR (n:Function) REQUIRE n.uid IS UNIQUE",
			"CREATE CONSTRAINT eshu_syntax_root_kind_file_path IF NOT EXISTS FOR (f:File) REQUIRE f.path IS UNIQUE",
			"MERGE (:File {path: '/tmp/eshu-nornicdb-root-kinds/main.go'})",
		}
		runNornicDBSyntaxSequence(t, ctx, driver, setup)

		query := `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MERGE (n:Function {uid: row.entity_id})
SET n += row.props
MERGE (f)-[rel:CONTAINS]->(n)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = row.generation_id
RETURN count(*) AS processed_rows`

		rows := []map[string]any{
			{
				"file_path":     "/tmp/eshu-nornicdb-root-kinds/main.go",
				"entity_id":     "fn:rooted",
				"generation_id": "gen-root-kinds",
				"props": map[string]any{
					"id":                   "fn:rooted",
					"name":                 "ExecuteGroup",
					"path":                 "/tmp/eshu-nornicdb-root-kinds/main.go",
					"relative_path":        "main.go",
					"line_number":          10,
					"start_line":           10,
					"end_line":             20,
					"repo_id":              "repo-root-kinds",
					"language":             "go",
					"lang":                 "go",
					"scope_id":             "scope-root-kinds",
					"generation_id":        "gen-root-kinds",
					"dead_code_root_kinds": []string{"go.interface_method_implementation"},
					"source": "MATCH (n:Function)\n" +
						"WHERE n.repo_id IN $repo_ids\n" +
						"REMOVE n.impl_context, n.docstring",
				},
			},
		}

		session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
			AccessMode:   neo4jdriver.AccessModeWrite,
			DatabaseName: localNornicDBDefaultDatabase,
		})
		defer func() {
			_ = session.Close(ctx)
		}()

		result, err := session.Run(ctx, query, map[string]any{"rows": rows})
		if err != nil {
			t.Fatalf("batched function root-kind query error = %v, want nil", err)
		}
		if _, err := result.Consume(ctx); err != nil {
			t.Fatalf("batched function root-kind consume error = %v, want nil", err)
		}
	})
}

func TestNornicDBCanonicalWriterFunctionSourceRemoveCompatibility(t *testing.T) {
	withNornicDBSyntaxDriver(t, func(ctx context.Context, driver neo4jdriver.DriverWithContext) {
		executor := nornicDBConformanceExecutor{
			driver:       driver,
			databaseName: localNornicDBDefaultDatabase,
			txTimeout:    15 * time.Second,
		}
		writer := sourcecypher.NewCanonicalNodeWriter(executor, 5, nil).
			WithEntityContainmentInEntityUpsert().
			WithEntityLabelBatchSize("Function", 5)

		mat := groupedWriteMaterialization("eshu-nornicdb-function-source-remove")
		filePath := mat.Files[0].Path
		mat.Entities = make([]projector.EntityRow, 0, 5)
		for i := range 5 {
			metadata := map[string]any{}
			if i == 4 {
				metadata["source"] = "MATCH (n:Function)\n" +
					"WHERE n.repo_id IN $repo_ids\n" +
					"REMOVE n.impl_context, n.docstring"
			}
			mat.Entities = append(mat.Entities, projector.EntityRow{
				EntityID:     "entity:remove-source:function:" + string(rune('a'+i)),
				Label:        "Function",
				EntityName:   "functionWithRemoveSource",
				FilePath:     filePath,
				RelativePath: "src/main.go",
				StartLine:    i + 1,
				EndLine:      i + 2,
				Language:     "go",
				RepoID:       mat.RepoID,
				Metadata:     metadata,
			})
		}

		if err := writer.Write(ctx, mat); err != nil {
			t.Fatalf("Write() error = %v, want nil", err)
		}
	})
}
