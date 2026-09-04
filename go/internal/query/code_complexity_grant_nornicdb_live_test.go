// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_complexity_grant

// Live two-tenant proof for the #5167 code-family complexity list branch.
//
// listMostComplexFunctions used to attach the caller's grant to an
// OPTIONAL MATCH's WHERE. A predicate in that position constrains the optional
// pattern, not the row set, so a scoped caller who supplied no repo_id got
// every Function in the corpus back -- name, language, line span, docstring and
// complexity -- with only the repository columns nulled. A text-capture test
// cannot see that, because the predicate string is present either way. Only a
// real backend settles it, which is what this file runs.
//
// Run against the pinned replay-tier proof image:
//
//	docker run -d --name nornic-5167-p0 -e NORNICDB_EMBEDDING_ENABLED=false \
//	  -e NORNICDB_NO_AUTH=true -p 17687:7687 \
//	  timothyswt/nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74
//	cd go && go test ./internal/query -tags live_nornicdb_complexity_grant \
//	  -run TestLiveNornicDBComplexityList -count=1 -v
package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	liveComplexityGrantedFunction   = "LiveGrantedComplexityProbe"
	liveComplexityUngrantedFunction = "LiveUngrantedComplexityProbe"
	liveComplexityOrphanFunction    = "LiveOrphanComplexityProbe"
)

// TestLiveNornicDBComplexityListFiltersUngrantedFunctions drives the route end
// to end. Two repositories are seeded, the caller is granted one, and the
// response body must never name the other tenant's function. The orphan
// function -- a Function attached to no repository at all -- pins the
// fail-closed half, because that is exactly the row an OPTIONAL MATCH-attached
// predicate keeps.
func TestLiveNornicDBComplexityListFiltersUngrantedFunctions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	driver := openLiveComplexityGrantDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLiveComplexityGrantGraph(ctx, t, driver)

	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := runLiveComplexityListRequest(t, driver, &auth)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, liveComplexityGrantedFunction) {
		t.Fatalf("granted tenant's function %q is missing from the response: %s", liveComplexityGrantedFunction, body)
	}
	for _, leaked := range []string{liveComplexityUngrantedFunction, codeGrantOtherRepo, liveComplexityOrphanFunction} {
		if strings.Contains(body, leaked) {
			t.Fatalf("scoped complexity list leaked %q; the corpus-wide read reached an ungranted row: %s", leaked, body)
		}
	}
}

// TestLiveNornicDBComplexityListKeepsTheUnscopedAnswer pins the other direction
// on the same seeded graph: a shared-key caller still sees both tenants, so the
// fix narrows the scoped answer only.
func TestLiveNornicDBComplexityListKeepsTheUnscopedAnswer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	driver := openLiveComplexityGrantDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLiveComplexityGrantGraph(ctx, t, driver)

	rec := runLiveComplexityListRequest(t, driver, nil)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{liveComplexityGrantedFunction, liveComplexityUngrantedFunction} {
		if !strings.Contains(body, want) {
			t.Fatalf("unscoped complexity list lost %q: %s", want, body)
		}
	}
}

func runLiveComplexityListRequest(
	t *testing.T,
	driver neo4jdriver.DriverWithContext,
	auth *AuthContext,
) *httptest.ResponseRecorder {
	t.Helper()

	handler := &CodeHandler{
		Profile:      ProfileLocalAuthoritative,
		GraphBackend: GraphBackendNornicDB,
		Neo4j:        NewNeo4jReader(driver, "nornic"),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := newCodeGrantRouteRequest(t, "/api/v0/code/complexity", map[string]any{"limit": 50}, auth)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func openLiveComplexityGrantDriver(ctx context.Context, t *testing.T) neo4jdriver.DriverWithContext {
	t.Helper()

	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		uri = "bolt://localhost:17687"
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		t.Fatalf("verify graph connectivity: %v", err)
	}
	return driver
}

// seedLiveComplexityGrantGraph writes the two-tenant fixture: one repository
// the caller is granted, one it is not, and one Function with no repository
// path at all. MERGE keeps repeated runs against a retained store idempotent.
func seedLiveComplexityGrantGraph(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext) {
	t.Helper()

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: "nornic",
		AccessMode:   neo4jdriver.AccessModeWrite,
	})
	defer func() { _ = session.Close(ctx) }()

	statements := []string{
		`MERGE (r:Repository {id:"` + codeGrantGrantedRepo + `"}) SET r.name="granted-service"`,
		`MERGE (r:Repository {id:"` + codeGrantOtherRepo + `"}) SET r.name="other-service"`,
		`MERGE (f:File {uid:"file:granted-session"}) SET f.id="file:granted-session", f.relative_path="internal/auth/session.go", f.language="go"`,
		`MERGE (f:File {uid:"file:other-session"}) SET f.id="file:other-session", f.relative_path="internal/auth/session.go", f.language="go"`,
		`MERGE (e:Function {uid:"fn:granted"}) SET e.id="fn:granted", e.name="` + liveComplexityGrantedFunction +
			`", e.language="go", e.start_line=10, e.end_line=40, e.cyclomatic_complexity=7`,
		`MERGE (e:Function {uid:"fn:ungranted"}) SET e.id="fn:ungranted", e.name="` + liveComplexityUngrantedFunction +
			`", e.language="go", e.start_line=10, e.end_line=40, e.cyclomatic_complexity=9`,
		`MERGE (e:Function {uid:"fn:orphan"}) SET e.id="fn:orphan", e.name="` + liveComplexityOrphanFunction +
			`", e.language="go", e.start_line=1, e.end_line=4, e.cyclomatic_complexity=11`,
		`MATCH (f:File {uid:"file:granted-session"}), (e:Function {uid:"fn:granted"}) MERGE (f)-[:CONTAINS]->(e)`,
		`MATCH (f:File {uid:"file:other-session"}), (e:Function {uid:"fn:ungranted"}) MERGE (f)-[:CONTAINS]->(e)`,
		`MATCH (r:Repository {id:"` + codeGrantGrantedRepo + `"}), (f:File {uid:"file:granted-session"}) MERGE (r)-[:REPO_CONTAINS]->(f)`,
		`MATCH (r:Repository {id:"` + codeGrantOtherRepo + `"}), (f:File {uid:"file:other-session"}) MERGE (r)-[:REPO_CONTAINS]->(f)`,
	}
	for _, stmt := range statements {
		if _, err := session.Run(ctx, stmt, nil); err != nil {
			t.Fatalf("seed statement %q: %v", stmt, err)
		}
	}
}
