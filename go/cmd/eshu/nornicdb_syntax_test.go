package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/eshulocal"
	"github.com/eshu-hq/eshu/go/internal/graph"
)

func TestNornicDBSyntaxVerification(t *testing.T) {
	withNornicDBSyntaxDriver(t, func(ctx context.Context, driver neo4jdriver.DriverWithContext) {
		tests := []struct {
			name   string
			cypher string
		}{
			// Keep this list to syntax Eshu sends to NornicDB directly. The
			// schema adapter verifies the backend-specific fulltext fallback.
			{
				name:   "fulltext procedure fallback",
				cypher: "CALL db.index.fulltext.createNodeIndex('eshu_syntax_fulltext_proc', ['EshuSyntaxFunction', 'EshuSyntaxClass', 'EshuSyntaxVariable'], ['name', 'source', 'docstring'])",
			},
			{
				name:   "collect distinct map literal",
				cypher: "WITH 1 AS ignored RETURN COLLECT(DISTINCT {id: 'f1', name: 'parse_config', path: 'src/config.go'}) AS items",
			},
		}
		runNornicDBSyntaxCases(t, ctx, driver, tests)
	})
}

func TestNornicDBCompatibilityWorkarounds(t *testing.T) {
	withNornicDBSyntaxDriver(t, func(ctx context.Context, driver neo4jdriver.DriverWithContext) {
		tests := []struct {
			name   string
			cypher string
		}{
			{
				name:   "composite node key alternative",
				cypher: "CREATE CONSTRAINT eshu_syntax_function_node_key IF NOT EXISTS FOR (f:EshuSyntaxFunction) REQUIRE (f.name, f.path, f.line_number) IS NODE KEY",
			},
			{
				name:   "multi label fulltext procedure",
				cypher: "CALL db.index.fulltext.createNodeIndex('eshu_syntax_workaround_fulltext', ['EshuSyntaxFunction', 'EshuSyntaxClass', 'EshuSyntaxVariable'], ['name', 'source', 'docstring'])",
			},
			{
				name:   "file path merge lookup index",
				cypher: "CREATE INDEX eshu_syntax_file_path_lookup IF NOT EXISTS FOR (f:File) ON (f.path)",
			},
			{
				name:   "directory path merge lookup index",
				cypher: "CREATE INDEX eshu_syntax_directory_path_lookup IF NOT EXISTS FOR (d:Directory) ON (d.path)",
			},
			{
				name:   "repository id merge lookup index",
				cypher: "CREATE INDEX eshu_syntax_repository_id_lookup IF NOT EXISTS FOR (r:Repository) ON (r.id)",
			},
			{
				name:   "workload id merge lookup index",
				cypher: "CREATE INDEX eshu_syntax_workload_id_lookup IF NOT EXISTS FOR (w:Workload) ON (w.id)",
			},
			{
				name:   "uid merge lookup index",
				cypher: "CREATE INDEX eshu_syntax_function_uid_lookup IF NOT EXISTS FOR (f:EshuSyntaxFunction) ON (f.uid)",
			},
		}
		runNornicDBSyntaxCases(t, ctx, driver, tests)
	})
}

func TestNornicDBBatchedEntityContainmentHotPathCompatibility(t *testing.T) {
	withNornicDBSyntaxDriver(t, func(ctx context.Context, driver neo4jdriver.DriverWithContext) {
		setup := []string{
			"CREATE CONSTRAINT eshu_syntax_function_uid IF NOT EXISTS FOR (n:Function) REQUIRE n.uid IS UNIQUE",
			"CREATE CONSTRAINT eshu_syntax_file_path IF NOT EXISTS FOR (f:File) REQUIRE f.path IS UNIQUE",
			"MERGE (:File {path: '/tmp/eshu-nornicdb-batch/main.go'})",
		}
		runNornicDBSyntaxSequence(t, ctx, driver, setup)

		nodeQuery := `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MERGE (n:Function {uid: row.entity_id})
SET n += row.props
MERGE (f)-[:CONTAINS]->(n)
RETURN count(*) AS processed_rows`

		rows := []map[string]any{
			{
				"file_path":  "/tmp/eshu-nornicdb-batch/main.go",
				"entity_id":  "fn:one",
				"entityID":   "fn:one",
				"primaryKey": "fn:one",
				"props": map[string]any{
					"id":            "fn:one",
					"name":          "handleRelationships",
					"path":          "/tmp/eshu-nornicdb-batch/main.go",
					"relative_path": "main.go",
					"line_number":   10,
					"start_line":    10,
					"end_line":      20,
					"repo_id":       "repo-batch",
					"language":      "go",
					"lang":          "go",
					"scope_id":      "scope-batch",
					"generation_id": "gen-batch",
				},
			},
			{
				"file_path":  "/tmp/eshu-nornicdb-batch/main.go",
				"entity_id":  "fn:two",
				"entityID":   "fn:two",
				"primaryKey": "fn:two",
				"props": map[string]any{
					"id":            "fn:two",
					"name":          "transitiveRelationshipsGraphResponse",
					"path":          "/tmp/eshu-nornicdb-batch/main.go",
					"relative_path": "main.go",
					"line_number":   30,
					"start_line":    30,
					"end_line":      40,
					"repo_id":       "repo-batch",
					"language":      "go",
					"lang":          "go",
					"scope_id":      "scope-batch",
					"generation_id": "gen-batch",
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

		result, err := session.Run(ctx, nodeQuery, map[string]any{
			"rows": rows,
		})
		if err != nil {
			t.Fatalf("batched canonical entity query error = %v, want nil", err)
		}
		if _, err := result.Consume(ctx); err != nil {
			t.Fatalf("batched canonical entity consume error = %v, want nil", err)
		}
		nodeCount, err := nornicDBReadCount(ctx, driver, `
MATCH (f:Function)
RETURN count(f) AS count`, nil)
		if err != nil {
			t.Fatalf("count batched canonical functions before containment error = %v, want nil", err)
		}
		if nodeCount != 2 {
			t.Fatalf("batched canonical function node count = %d, want 2", nodeCount)
		}
		nodeCountWithRepo, err := nornicDBReadCount(ctx, driver, `
MATCH (f:Function)
WHERE f.repo_id = $repo_id
RETURN count(f) AS count`, map[string]any{
			"repo_id": "repo-batch",
		})
		if err != nil {
			t.Fatalf("count batched canonical functions with repo before containment error = %v, want nil", err)
		}
		if nodeCountWithRepo != 2 {
			t.Fatalf("batched canonical function node count with repo = %d, want 2", nodeCountWithRepo)
		}
		count, err := nornicDBReadCount(ctx, driver, `
MATCH (:File {path: $file_path})-[:CONTAINS]->(f:Function)
WHERE f.repo_id = $repo_id
RETURN count(f) AS count`, map[string]any{
			"file_path": "/tmp/eshu-nornicdb-batch/main.go",
			"repo_id":   "repo-batch",
		})
		if err != nil {
			t.Fatalf("count batched canonical functions error = %v, want nil", err)
		}
		if count != 2 {
			t.Fatalf("batched canonical function count = %d, want 2", count)
		}
	})
}

func TestNornicDBBatchedTerraformVariableBraceMetadataCompatibility(t *testing.T) {
	withNornicDBSyntaxDriver(t, func(ctx context.Context, driver neo4jdriver.DriverWithContext) {
		setup := []string{
			"CREATE CONSTRAINT eshu_syntax_tf_variable_uid IF NOT EXISTS FOR (n:TerraformVariable) REQUIRE n.uid IS UNIQUE",
			"CREATE CONSTRAINT eshu_syntax_tf_file_path IF NOT EXISTS FOR (f:File) REQUIRE f.path IS UNIQUE",
			"MERGE (:File {path: '/tmp/eshu-nornicdb-batch/variables.tf'})",
		}
		runNornicDBSyntaxSequence(t, ctx, driver, setup)

		query := `UNWIND $rows AS row
MATCH (f:File {path: $file_path})
MERGE (n:TerraformVariable {uid: row.entity_id})
SET n += row.props
MERGE (f)-[rel:CONTAINS]->(n)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = row.generation_id
RETURN count(*) AS processed_rows`

		description := `symbols from the following set: ^$*.[]{}()?"!@#%&/\\,><':;|_~`
		rows := []map[string]any{
			{
				"entity_id":     "tf-var:empty-map",
				"generation_id": "gen-batch",
				"props": map[string]any{
					"id":            "tf-var:empty-map",
					"name":          "environment_vars",
					"path":          "/tmp/eshu-nornicdb-batch/variables.tf",
					"relative_path": "variables.tf",
					"line_number":   12,
					"start_line":    12,
					"end_line":      16,
					"repo_id":       "repo-batch",
					"language":      "hcl",
					"lang":          "hcl",
					"scope_id":      "scope-batch",
					"generation_id": "gen-batch",
					"default":       "{}",
					"var_type":      "map(object({ name = string }))",
				},
			},
			{
				"entity_id":     "tf-var:description-braces",
				"generation_id": "gen-batch",
				"props": map[string]any{
					"id":            "tf-var:description-braces",
					"name":          "passwords_require_symbols",
					"path":          "/tmp/eshu-nornicdb-batch/variables.tf",
					"relative_path": "variables.tf",
					"line_number":   24,
					"start_line":    24,
					"end_line":      28,
					"repo_id":       "repo-batch",
					"language":      "hcl",
					"lang":          "hcl",
					"scope_id":      "scope-batch",
					"generation_id": "gen-batch",
					"default":       "cty.True",
					"var_type":      "bool",
					"description":   description,
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

		result, err := session.Run(ctx, query, map[string]any{
			"file_path": "/tmp/eshu-nornicdb-batch/variables.tf",
			"rows":      rows,
		})
		if err != nil {
			t.Fatalf("batched TerraformVariable brace metadata query error = %v, want nil", err)
		}
		if _, err := result.Consume(ctx); err != nil {
			t.Fatalf("batched TerraformVariable brace metadata consume error = %v, want nil", err)
		}

		count, err := nornicDBReadCount(ctx, driver, `
MATCH (:File {path: $file_path})-[:CONTAINS]->(v:TerraformVariable)
WHERE v.repo_id = $repo_id
RETURN count(v) AS count`, map[string]any{
			"file_path": "/tmp/eshu-nornicdb-batch/variables.tf",
			"repo_id":   "repo-batch",
		})
		if err != nil {
			t.Fatalf("count batched TerraformVariable containment error = %v, want nil", err)
		}
		if count != 2 {
			t.Fatalf("batched TerraformVariable containment count = %d, want 2", count)
		}

		result, err = session.Run(ctx, `
MATCH (:File {path: $file_path})-[:CONTAINS]->(v:TerraformVariable)
RETURN v.name AS name, v.default AS default_value, v.var_type AS var_type, v.description AS description`, map[string]any{
			"file_path": "/tmp/eshu-nornicdb-batch/variables.tf",
		})
		if err != nil {
			t.Fatalf("read batched TerraformVariable metadata error = %v, want nil", err)
		}
		metadataByName := make(map[string][]any)
		for result.Next(ctx) {
			record := result.Record()
			metadataByName[record.Values[0].(string)] = record.Values
		}
		if err := result.Err(); err != nil {
			t.Fatalf("iterate batched TerraformVariable metadata error = %v, want nil", err)
		}
		if got := metadataByName["environment_vars"]; got[1] != "{}" || got[2] != "map(object({ name = string }))" {
			t.Fatalf("environment_vars metadata = %#v, want brace-bearing default and var_type", got)
		}
		if got := metadataByName["passwords_require_symbols"]; got[1] != "cty.True" || got[2] != "bool" || got[3] != description {
			t.Fatalf("passwords_require_symbols metadata = %#v, want description %#v", got, description)
		}
	})
}

func TestNornicDBCanonicalEntitySingletonFallbackForShortestPathValues(t *testing.T) {
	withNornicDBSyntaxDriver(t, func(ctx context.Context, driver neo4jdriver.DriverWithContext) {
		setup := []string{
			"MERGE (:File {path: '/tmp/eshu-nornicdb-batch/main.go'})",
		}
		runNornicDBSyntaxSequence(t, ctx, driver, setup)

		batchedQuery := `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MERGE (n:Function {uid: row.entity_id})
SET n += row.props
MERGE (f)-[:CONTAINS]->(n)
RETURN count(*) AS processed_rows`

		normalRows := []map[string]any{
			{
				"file_path": "/tmp/eshu-nornicdb-batch/main.go",
				"entity_id": "fn:one",
				"props": map[string]any{
					"id":            "fn:one",
					"name":          "handleRelationships",
					"path":          "/tmp/eshu-nornicdb-batch/main.go",
					"relative_path": "main.go",
					"line_number":   10,
					"start_line":    10,
					"end_line":      20,
					"repo_id":       "repo-batch",
					"language":      "go",
					"lang":          "go",
					"scope_id":      "scope-batch",
					"generation_id": "gen-batch",
				},
			},
		}
		singletonQuery := `MATCH (f:File {path: $file_path})
MERGE (n:Function {uid: $entity_id})
SET n += $props
MERGE (f)-[:CONTAINS]->(n)
RETURN count(*) AS processed_rows`
		shortestPathRow := map[string]any{
			"file_path": "/tmp/eshu-nornicdb-batch/main.go",
			"entity_id": "fn:two",
			"props": map[string]any{
				"id":            "fn:two",
				"name":          "TestHandleCallChainReturnsShortestPath",
				"path":          "/tmp/eshu-nornicdb-batch/main.go",
				"relative_path": "main.go",
				"line_number":   30,
				"start_line":    30,
				"end_line":      40,
				"repo_id":       "repo-batch",
				"language":      "go",
				"lang":          "go",
				"scope_id":      "scope-batch",
				"generation_id": "gen-batch",
			},
		}

		session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
			AccessMode:   neo4jdriver.AccessModeWrite,
			DatabaseName: localNornicDBDefaultDatabase,
		})
		defer func() {
			_ = session.Close(ctx)
		}()

		result, err := session.Run(ctx, batchedQuery, map[string]any{"rows": normalRows})
		if err != nil {
			t.Fatalf("batched canonical entity query error = %v, want nil", err)
		}
		if _, err := result.Consume(ctx); err != nil {
			t.Fatalf("batched canonical entity consume error = %v, want nil", err)
		}

		result, err = session.Run(ctx, singletonQuery, shortestPathRow)
		if err != nil {
			t.Fatalf("singleton canonical entity query error = %v, want nil", err)
		}
		if _, err := result.Consume(ctx); err != nil {
			t.Fatalf("singleton canonical entity consume error = %v, want nil", err)
		}

		count, err := nornicDBReadCount(ctx, driver, `
MATCH (:File {path: $file_path})-[:CONTAINS]->(f:Function)
WHERE f.repo_id = $repo_id
RETURN count(f) AS count`, map[string]any{
			"file_path": "/tmp/eshu-nornicdb-batch/main.go",
			"repo_id":   "repo-batch",
		})
		if err != nil {
			t.Fatalf("count fallback canonical functions error = %v, want nil", err)
		}
		if count != 2 {
			t.Fatalf("fallback canonical function count = %d, want 2", count)
		}
	})
}

func TestNornicDBSharedEdgeWriteCompatibilityWorkarounds(t *testing.T) {
	withNornicDBSyntaxDriver(t, func(ctx context.Context, driver neo4jdriver.DriverWithContext) {
		setup := []string{
			"MERGE (:EshuSyntaxFunction {uid: 'caller'})",
			"MERGE (:EshuSyntaxFunction {uid: 'callee'})",
			"MERGE (:EshuSyntaxClass {uid: 'child'})",
			"MERGE (:EshuSyntaxClass {uid: 'parent'})",
			"MERGE (:EshuSyntaxSqlTable {uid: 'table'})",
			"MERGE (:EshuSyntaxSqlColumn {uid: 'column'})",
		}
		runNornicDBSyntaxSequence(t, ctx, driver, setup)

		tests := []struct {
			name   string
			cypher string
		}{
			{
				name: `shared code-call write`,
				cypher: `MATCH (source:EshuSyntaxFunction {uid: 'caller'})
MATCH (target:EshuSyntaxFunction {uid: 'callee'})
MERGE (source)-[rel:CALLS]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'Parser and symbol analysis resolved a code call edge',
    rel.evidence_source = 'parser/code-calls'`,
			},
			{
				name: `shared inheritance write`,
				cypher: `MATCH (child:EshuSyntaxClass {uid: 'child'})
MATCH (parent:EshuSyntaxClass {uid: 'parent'})
MERGE (child)-[rel:INHERITS]->(parent)
SET rel.confidence = 0.95,
    rel.reason = 'Parser entity bases metadata resolved an inheritance edge',
    rel.evidence_source = 'reducer/inheritance',
    rel.relationship_type = 'INHERITS'`,
			},
			{
				name: `shared sql write`,
				cypher: `MATCH (source:EshuSyntaxSqlTable {uid: 'table'})
MATCH (target:EshuSyntaxSqlColumn {uid: 'column'})
MERGE (source)-[rel:HAS_COLUMN]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'SQL entity metadata resolved a table-column containment edge',
    rel.evidence_source = 'reducer/sql-relationships'`,
			},
		}
		runNornicDBSyntaxCases(t, ctx, driver, tests)
	})
}

func TestNornicDBSchemaAdapterVerification(t *testing.T) {
	withNornicDBSyntaxDriver(t, func(ctx context.Context, driver neo4jdriver.DriverWithContext) {
		stmts, err := graph.SchemaStatementsForBackend(graph.SchemaBackendNornicDB)
		if err != nil {
			t.Fatalf("SchemaStatementsForBackend(%q) error = %v, want nil", graph.SchemaBackendNornicDB, err)
		}
		for _, stmt := range stmts {
			if err := runNornicDBSyntaxCypher(ctx, driver, stmt); err != nil {
				t.Fatalf("run adapted schema statement %q error = %v", stmt, err)
			}
		}
	})
}

func withNornicDBSyntaxDriver(t *testing.T, fn func(context.Context, neo4jdriver.DriverWithContext)) {
	t.Helper()

	if strings.TrimSpace(os.Getenv("ESHU_NORNICDB_BINARY")) == "" {
		t.Skip("set ESHU_NORNICDB_BINARY to run the NornicDB syntax compatibility gate")
	}

	binaryPath, err := resolveNornicDBBinary()
	if err != nil {
		t.Fatalf("resolveNornicDBBinary() error = %v", err)
	}
	t.Logf("using NornicDB binary %s", binaryPath)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	root := t.TempDir()
	graph, err := startManagedLocalNornicDB(ctx, eshulocal.Layout{
		GraphDir: filepath.Join(root, "graph"),
		LogsDir:  filepath.Join(root, "logs"),
	})
	if err != nil {
		t.Fatalf("startManagedLocalNornicDB() error = %v", err)
	}
	t.Cleanup(func() {
		if err := stopManagedLocalGraph(graph, localGraphShutdownTimeout); err != nil {
			t.Errorf("stopManagedLocalGraph() error = %v, want nil", err)
		}
	})

	driver, err := neo4jdriver.NewDriverWithContext(
		"bolt://"+graph.Address+":"+strconv.Itoa(graph.BoltPort),
		neo4jdriver.BasicAuth(graph.Username, graph.Password, ""),
	)
	if err != nil {
		t.Fatalf("NewDriverWithContext() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		_ = driver.Close(context.Background())
	})
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("VerifyConnectivity() error = %v, want nil", err)
	}

	fn(ctx, driver)
}

func runNornicDBSyntaxCases(t *testing.T, ctx context.Context, driver neo4jdriver.DriverWithContext, tests []struct {
	name   string
	cypher string
}) {
	t.Helper()

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if err := runNornicDBSyntaxCypher(ctx, driver, tt.cypher); err != nil {
				t.Fatalf("run syntax probe %q error = %v", tt.name, err)
			}
		})
	}
}

func runNornicDBSyntaxSequence(t *testing.T, ctx context.Context, driver neo4jdriver.DriverWithContext, statements []string) {
	t.Helper()

	for _, stmt := range statements {
		if err := runNornicDBSyntaxCypher(ctx, driver, stmt); err != nil {
			t.Fatalf("run setup statement %q error = %v", stmt, err)
		}
	}
}

func runNornicDBSyntaxCypher(ctx context.Context, driver neo4jdriver.DriverWithContext, cypher string) error {
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: localNornicDBDefaultDatabase,
	})
	defer func() {
		_ = session.Close(ctx)
	}()

	result, err := session.Run(ctx, cypher, map[string]any{})
	if err != nil {
		return err
	}
	_, err = result.Consume(ctx)
	return err
}
