// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build queryplan_profile_live

package query

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/imports"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4j "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

func methodologyAssertResponse(t *testing.T, request codemodel.ImportDependencyRequest, response map[string]any) {
	t.Helper()
	if response["query_type"] != request.EffectiveQueryType() {
		t.Fatalf("response query_type=%v", response["query_type"])
	}
	if response["source_backend"] != "graph" {
		t.Fatalf("source_backend=%v", response["source_backend"])
	}
	if _, ok := response["has_more"].(bool); !ok {
		t.Fatalf("has_more=%T", response["has_more"])
	}
	if _, ok := response["truncated"].(bool); !ok {
		t.Fatalf("truncated=%T", response["truncated"])
	}
}

// methodologyExpectedPage computes fixture answers from the declared physical
// edges, independently of the production Cypher builders and row shapers.
func methodologyExpectedPage(request codemodel.ImportDependencyRequest) (int, bool) {
	proofOnly := request.RepoID != "" || request.Access.Scoped()
	sourceOnly := request.SourceFile != "" || request.SourceModule != ""
	targetOnly := request.TargetModule != ""
	switch request.EffectiveQueryType() {
	case "file_import_cycles":
		if proofOnly {
			return 1, false
		}
		return 2, false
	case "cross_module_calls":
		return 1, false
	case "package_imports":
		perRepo := 2
		if sourceOnly || targetOnly {
			perRepo = 1
		}
		if proofOnly {
			return perRepo, false
		}
		return perRepo * 2, false
	default:
		proofEdges := 4   // three proof.py -> proof.target, one target.py -> proof.source
		otherEdges := 130 // proof.py, target.py, and 128 skew files
		if sourceOnly || targetOnly {
			proofEdges = 3
		}
		if sourceOnly {
			otherEdges = 1
		} else if targetOnly {
			otherEdges = 129
		}
		total := proofEdges
		if !proofOnly {
			total += otherEdges
		}
		if total > 10 {
			return 10, true
		}
		return total, false
	}
}

func methodologyAssertSemanticCases(t *testing.T, ctx context.Context, reader *methodologyGraphReader) {
	t.Helper()
	cases := []struct {
		name      string
		req       codemodel.ImportDependencyRequest
		wantCount int
		wantMore  bool
	}{
		{"granted proof file", codemodel.ImportDependencyRequest{QueryType: "imports_by_file", SourceFile: "src/proof.py", Access: queryplanScopedRepositoryAccess()}, 3, false},
		{"all proof file", codemodel.ImportDependencyRequest{QueryType: "imports_by_file", SourceFile: "src/proof.py", Access: querycontract.RepositoryAccessFilter{AllScopes: true}}, 4, false},
		{"empty grant", codemodel.ImportDependencyRequest{QueryType: "imports_by_file", SourceFile: "src/proof.py"}, 0, false},
		{"missing file", codemodel.ImportDependencyRequest{QueryType: "imports_by_file", SourceFile: "src/missing.py", Access: queryplanScopedRepositoryAccess()}, 0, false},
		{"scoped cycle", codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "proof-repository", Access: queryplanScopedRepositoryAccess()}, 1, false},
		{"scoped call", codemodel.ImportDependencyRequest{QueryType: "cross_module_calls", RepoID: "proof-repository", Access: queryplanScopedRepositoryAccess()}, 1, false},
		{"paged module", codemodel.ImportDependencyRequest{QueryType: "module_dependencies", SourceModule: "proof.source", Limit: 1, Access: querycontract.RepositoryAccessFilter{AllScopes: true}}, 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.req.Validate(); err != nil {
				t.Fatal(err)
			}
			rows, enumeration, err := imports.Rows(ctx, reader, tc.req)
			if err != nil {
				t.Fatal(err)
			}
			response := codemodel.ImportDependencyResponseWithCycleEnumeration(tc.req, rows, enumeration)
			if response["count"] != tc.wantCount || response["has_more"] != tc.wantMore {
				t.Fatalf("count=%v has_more=%v, want %d %v", response["count"], response["has_more"], tc.wantCount, tc.wantMore)
			}
		})
	}
	scoped := cases[0].req
	production := codemodel.DirectImportRowsCypher(scoped)
	grant := "WHERE (repo.id IN $allowed_repository_ids OR repo.id IN $allowed_scope_ids)\n"
	mutant := strings.Replace(production, grant, "", 1)
	if mutant == production {
		t.Fatal("grant mutation did not remove a production predicate")
	}
	productionRows := methodologyExecute(t, ctx, reader.driver, reader.database, production, imports.Params(scoped))
	mutantRows := methodologyExecute(t, ctx, reader.driver, reader.database, mutant, imports.Params(scoped))
	if len(productionRows) != 3 || len(mutantRows) != 4 {
		t.Fatalf("planted grant removal: production rows=%d mutant rows=%d, want 3 and 4", len(productionRows), len(mutantRows))
	}
	for _, row := range productionRows {
		if row["repo_id"] != "proof-repository" {
			t.Fatalf("granted result leaked %v", row["repo_id"])
		}
	}
	if !methodologyRowsContainRepo(mutantRows, "other-repository") {
		t.Fatal("planted grant removal did not reveal other tenant")
	}
	t.Logf("planted removed-grant RED rows=%d GREEN rows=%d", len(mutantRows), len(productionRows))
}

func methodologyRowsContainRepo(rows []map[string]any, repo string) bool {
	for _, row := range rows {
		if row["repo_id"] == repo {
			return true
		}
	}
	return false
}

func methodologySeedGraph(t *testing.T, ctx context.Context, driver neo4j.DriverWithContext, database string) {
	t.Helper()
	methodologyExecute(t, ctx, driver, database, "MATCH (n) DETACH DELETE n", nil)
	for _, legacy := range []string{"methodology_repo_id", "methodology_file_relative", "methodology_file_path", "methodology_module_name", "methodology_function_uid"} {
		methodologyExecute(t, ctx, driver, database, "DROP INDEX "+legacy+" IF EXISTS", nil)
	}
	schema, indexes := methodologyGraphProductionDDL(t)
	for _, stmt := range append(schema, indexes...) {
		methodologyExecute(t, ctx, driver, database, stmt, nil)
	}
	methodologyExecute(t, ctx, driver, database, "CALL db.awaitIndexes(120)", nil)
	methodologyCheckGraphSchema(t, ctx, driver, database)
	for _, stmt := range methodologyGraphSeedDDL {
		methodologyExecute(t, ctx, driver, database, stmt, nil)
	}
	t.Logf("fixture shape: two tenants, duplicate import edges, null import attributes, orphan history, 128 skew files")
}

func methodologyGraphProductionDDL(t *testing.T) ([]string, []string) {
	t.Helper()
	all, err := graph.SchemaStatementsForBackend(graph.SchemaBackendNeo4j)
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{"repository_id": false, "path": false, "module_name_lookup": false, "function_uid_unique": false}
	var constraints, indexes []string
	for _, stmt := range all {
		for name := range wanted {
			if strings.HasPrefix(stmt, "CREATE CONSTRAINT "+name+" ") {
				constraints = append(constraints, stmt)
				wanted[name] = true
			} else if strings.HasPrefix(stmt, "CREATE INDEX "+name+" ") {
				indexes = append(indexes, stmt)
				wanted[name] = true
			}
		}
	}
	for name, present := range wanted {
		if !present {
			t.Fatalf("production Neo4j schema missing %s", name)
		}
	}
	return constraints, indexes
}

func methodologyCheckGraphSchema(t *testing.T, ctx context.Context, driver neo4j.DriverWithContext, database string) {
	t.Helper()
	for _, name := range methodologyMissingGraphSchema(t, ctx, driver, database) {
		t.Fatalf("physical production Neo4j schema missing or offline: %s", name)
	}
}

func methodologyMissingGraphSchema(t *testing.T, ctx context.Context, driver neo4j.DriverWithContext, database string) []string {
	t.Helper()
	indexes := map[string]bool{"repository_id": false, "path": false, "module_name_lookup": false, "function_uid_unique": false}
	constraints := map[string]bool{"repository_id": false, "path": false, "function_uid_unique": false}
	for _, row := range methodologyExecute(t, ctx, driver, database, "SHOW INDEXES YIELD name, state RETURN name, state", nil) {
		name := fmt.Sprint(row["name"])
		if _, required := indexes[name]; required && row["state"] == "ONLINE" {
			indexes[name] = true
		}
	}
	for _, row := range methodologyExecute(t, ctx, driver, database, "SHOW CONSTRAINTS YIELD name RETURN name", nil) {
		name := fmt.Sprint(row["name"])
		if _, required := constraints[name]; required {
			constraints[name] = true
		}
	}
	var missing []string
	for name, online := range indexes {
		if !online || (name != "module_name_lookup" && !constraints[name]) {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

func methodologyProveMissingGraphIndex(t *testing.T, ctx context.Context, driver neo4j.DriverWithContext, database string) {
	t.Helper()
	methodologyCheckGraphSchema(t, ctx, driver, database)
	methodologyExecute(t, ctx, driver, database, "DROP INDEX module_name_lookup IF EXISTS", nil)
	missing := methodologyMissingGraphSchema(t, ctx, driver, database)
	if !slices.Equal(missing, []string{"module_name_lookup"}) {
		t.Fatalf("planted physical index drop missing=%v, want module_name_lookup", missing)
	}
	_, indexes := methodologyGraphProductionDDL(t)
	for _, ddl := range indexes {
		methodologyExecute(t, ctx, driver, database, ddl, nil)
	}
	methodologyExecute(t, ctx, driver, database, "CALL db.awaitIndexes(120)", nil)
	methodologyCheckGraphSchema(t, ctx, driver, database)
	t.Log("planted missing production module_name_lookup index RED, restored physical index GREEN")
}

var methodologyGraphSeedDDL = []string{
	`CREATE (r:Repository {id:'proof-repository', name:'proof', scope_id:'proof-scope'})
 CREATE (other:Repository {id:'other-repository', name:'other', scope_id:'other-scope'})
 CREATE (a:File {path:'/proof/src/proof.py', relative_path:'src/proof.py', name:'proof.source.py', language:'python'})
 CREATE (b:File {path:'/proof/src/target.py', relative_path:'src/target.py', name:'proof.target.py', language:'python'})
 CREATE (x:File {path:'/other/src/proof.py', relative_path:'src/proof.py', name:'proof.source.py', language:'python'})
 CREATE (y:File {path:'/other/src/target.py', relative_path:'src/target.py', name:'proof.target.py', language:'python'})
 CREATE (m:Module {name:'proof.source', lang:'python'})
 CREATE (n:Module {name:'proof.target', lang:'python'})
 CREATE (om:Module {name:'proof.source', lang:'python'})
 CREATE (on:Module {name:'proof.target', lang:'python'})
 CREATE (r)-[:REPO_CONTAINS]->(a), (r)-[:REPO_CONTAINS]->(b), (other)-[:REPO_CONTAINS]->(x), (other)-[:REPO_CONTAINS]->(y)
 CREATE (a)-[:CONTAINS]->(m), (b)-[:CONTAINS]->(n), (x)-[:CONTAINS]->(om), (y)-[:CONTAINS]->(on)
 CREATE (a)-[:IMPORTS {imported_name:'proof.target', line_number:1}]->(n)
 CREATE (b)-[:IMPORTS {imported_name:'proof.source', line_number:2}]->(m)
 CREATE (x)-[:IMPORTS {imported_name:'proof.target', line_number:1}]->(on)
 CREATE (y)-[:IMPORTS {imported_name:'proof.source', line_number:2}]->(om)
 CREATE (f:Function {uid:'fn-proof', id:'fn-proof', name:'proof'})
 CREATE (g:Function {uid:'fn-target', id:'fn-target', name:'target'})
 CREATE (h:Function {uid:'fn-other', id:'fn-other', name:'other'})
 CREATE (a)-[:CONTAINS]->(f), (b)-[:CONTAINS]->(g), (x)-[:CONTAINS]->(h)
 CREATE (f)-[:CALLS {call_kind:'direct'}]->(g)
 CREATE (h)-[:CALLS {call_kind:'direct'}]->(g)`,
	`MATCH (r:Repository {id:'proof-repository'})-[:REPO_CONTAINS]->(a:File {relative_path:'src/proof.py'}), (b:File {path:'/proof/src/target.py'})-[:CONTAINS]->(m:Module {name:'proof.target'})
         CREATE (a)-[:IMPORTS {imported_name:'proof.target', line_number:3}]->(m)`,
	`MATCH (r:Repository {id:'other-repository'}), (m:Module {name:'proof.target'})<-[:IMPORTS]-(f:File)<-[:REPO_CONTAINS]-(r)
         WITH r,m LIMIT 1
         UNWIND range(1, 128) AS i
         CREATE (f:File {path:'/other/src/skew-' + toString(i) + '.py', relative_path:'src/skew-' + toString(i) + '.py', name:'skew.py', language:'python'})
         CREATE (r)-[:REPO_CONTAINS]->(f)
         CREATE (f)-[:IMPORTS {line_number:i}]->(m)`,
	`CREATE (:File {path:'/history/src/proof.py', relative_path:'src/proof.py', name:'proof.py', language:'python', version:'history'})`,
	`MATCH (a:File {path:'/proof/src/proof.py'}), (b:File {path:'/proof/src/target.py'})-[:CONTAINS]->(n:Module {name:'proof.target'})
         CREATE (a)-[:IMPORTS {line_number:4}]->(n)`,
}

var _ querycontract.GraphQuery = (*methodologyGraphReader)(nil)
