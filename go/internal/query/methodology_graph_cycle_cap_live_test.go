// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build queryplan_profile_live

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/imports"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	neo4j "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// methodologyAssertCappedCyclePage plants an 8-file complete import digraph.
// Its >1,000 simple cycles stop at the production enumeration cap; the final
// public page must advertise partial truth without another page cursor.
func methodologyAssertCappedCyclePage(t *testing.T, ctx context.Context, driver neo4j.DriverWithContext, database string) {
	t.Helper()
	methodologyExecute(t, ctx, driver, database, `CREATE (:Repository {id:'cycle-cap-repository',name:'cap'})`, nil)
	methodologyExecute(t, ctx, driver, database, `MATCH (r:Repository {id:'cycle-cap-repository'})
 UNWIND range(0,7) AS i
 CREATE (f:File {path:'/cap/cap'+toString(i)+'.py',relative_path:'cap'+toString(i)+'.py',name:'cap'+toString(i)+'.py',language:'python'})
 CREATE (m:Module {name:'cap'+toString(i),lang:'python'})
 CREATE (r)-[:REPO_CONTAINS]->(f)
 CREATE (f)-[:CONTAINS]->(m)`, nil)
	methodologyExecute(t, ctx, driver, database, `MATCH (r:Repository {id:'cycle-cap-repository'})-[:REPO_CONTAINS]->(f:File),
       (r)-[:REPO_CONTAINS]->(:File)-[:CONTAINS]->(m:Module)
 WHERE f.name <> m.name+'.py'
 CREATE (f)-[:IMPORTS {line_number:1}]->(m)`, nil)
	req := codemodel.ImportDependencyRequest{QueryType: "file_import_cycles", RepoID: "cycle-cap-repository", Offset: 800, Limit: 200, Access: querycontract.RepositoryAccessFilter{AllScopes: true}}
	if err := req.Validate(); err != nil {
		t.Fatal(err)
	}
	reader := &methodologyGraphReader{driver: driver, database: database}
	rows, enumeration, err := imports.Rows(ctx, reader, req)
	if err != nil {
		t.Fatal(err)
	}
	response := codemodel.ImportDependencyResponseWithCycleEnumeration(req, rows, enumeration)
	if !enumeration.Truncated || enumeration.StopReason != codemodel.CycleStopCycleCap {
		t.Fatalf("cycle enumeration=%+v, want cap", enumeration)
	}
	if response["count"] != 200 || response["has_more"] != false || response["truncated"] != true || response["next_offset"] != nil {
		t.Fatalf("terminal capped cycle response count=%v has_more=%v truncated=%v next_offset=%v", response["count"], response["has_more"], response["truncated"], response["next_offset"])
	}
	t.Logf("terminal capped cycle page count=%v truncated=%v has_more=%v reason=%s", response["count"], response["truncated"], response["has_more"], enumeration.StopReason)
	handler := &codequery.CodeHandler{Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)
	httpRequest := httptest.NewRequest(http.MethodPost, "/api/v0/code/imports/investigate",
		strings.NewReader(`{"query_type":"file_import_cycles","repo_id":"cycle-cap-repository","offset":800,"limit":200}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httpRequest)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP capped cycle status=%d body=%s", w.Code, w.Body.String())
	}
	var httpResponse map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &httpResponse); err != nil {
		t.Fatal(err)
	}
	if httpResponse["count"] != float64(200) || httpResponse["truncated"] != true || httpResponse["has_more"] != false || httpResponse["next_offset"] != nil {
		t.Fatalf("HTTP capped cycle response count=%v truncated=%v has_more=%v next_offset=%v", httpResponse["count"], httpResponse["truncated"], httpResponse["has_more"], httpResponse["next_offset"])
	}
	t.Log("HTTP capped cycle route preserved terminal truncation and paging")
}
