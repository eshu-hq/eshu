// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_language_imports_grant

// Live proof for the #6541 directory language-query shape, against a real
// NornicDB build.
//
// It drives the production path end to end -- buildDirectoryCypher, the
// re-sort and truncate, and the repository-name read -- through
// Handler.directoryRowsByLanguage with a real graph reader, rather than
// asserting statement text. That matters here because two of the three steps
// exist only to compensate for measured backend behaviour, so a test that
// stopped at the statement would prove none of it.
//
// The fixture nests directories four levels deep and mixes languages, because
// the defects this shape replaced are invisible on a flat single-language one:
// the walk it replaced folded a nested directory's files into its parent and
// dropped the nested directory, and a bounded *1..N chain did the same.
//
// Run against BOTH builds. The v1.2.1 pin and v1.3.1 must agree here; the
// shapes that disagree are recorded in
// docs/internal/evidence/6541-directory-query-s2.md.
//
//	docker run -d --name eshu-6541 -e NORNICDB_EMBEDDING_ENABLED=false \
//	  -e NORNICDB_NO_AUTH=true -p 127.0.0.1:17955:7687 \
//	  timothyswt/nornicdb-cpu-bge:v1.3.1
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:17955 go test ./internal/query/language \
//	  -tags live_nornicdb_language_imports_grant \
//	  -run TestLiveNornicDBDirectoryLanguageQuery -count=1 -v
package language

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	live6541Alpha     = "repo://live6541/alpha"
	live6541Beta      = "repo://live6541/beta"
	live6541AlphaName = "alpha-svc-6541"
	live6541BetaName  = "beta-svc-6541"
)

// live6541Directory is one seeded directory and the go-file count the route
// must report for it. Files carrying another language are seeded too and must
// not be counted.
type live6541Directory struct {
	path   string
	name   string
	repoID string
	parent string
	files  map[string]string
}

// live6541Fixture is the seeded graph. alpha nests four levels deep
// (src > pkg > inner > deep); docs and cmd/tool hold no go file at all, so a
// statement that counted every file instead of the requested language would
// return them.
func live6541Fixture() []live6541Directory {
	return []live6541Directory{
		{
			path: "/live6541/alpha/src", name: "src", repoID: live6541Alpha,
			files: map[string]string{"a1.go": "go", "a2.go": "go", "a3.py": "python"},
		},
		{
			path: "/live6541/alpha/src/pkg", name: "pkg", repoID: live6541Alpha,
			parent: "/live6541/alpha/src",
			files:  map[string]string{"p1.go": "go"},
		},
		{
			path: "/live6541/alpha/src/pkg/inner", name: "inner", repoID: live6541Alpha,
			parent: "/live6541/alpha/src/pkg",
			files:  map[string]string{"i1.go": "go", "i2.go": "go", "i3.go": "go"},
		},
		{
			path: "/live6541/alpha/src/pkg/inner/deep", name: "deep", repoID: live6541Alpha,
			parent: "/live6541/alpha/src/pkg/inner",
			files:  map[string]string{"d1.go": "go", "d2.py": "python"},
		},
		{
			path: "/live6541/alpha/docs", name: "docs", repoID: live6541Alpha,
			files: map[string]string{"readme.md": "markdown"},
		},
		{
			path: "/live6541/beta/cmd", name: "cmd", repoID: live6541Beta,
			files: map[string]string{"m.go": "go", "n.go": "go"},
		},
		{
			path: "/live6541/beta/cmd/tool", name: "tool", repoID: live6541Beta,
			parent: "/live6541/beta/cmd",
			files:  map[string]string{"t.py": "python"},
		},
		{
			path: "/live6541/beta/lib", name: "lib", repoID: live6541Beta,
			files: map[string]string{"l1.go": "go", "l2.go": "go", "l3.go": "go", "l4.go": "go"},
		},
	}
}

// openLive6541Driver connects to the backend under test.
func openLive6541Driver(ctx context.Context, t *testing.T) neo4jdriver.DriverWithContext {
	t.Helper()

	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		uri = "bolt://127.0.0.1:17955"
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

// seedLive6541Graph writes the fixture through the canonical projector's own
// write shapes: Repository upsert, the Directory node phase, then the depth-0
// (Repository-[:CONTAINS]->) and depth-N (Directory-[:CONTAINS]->) edge phases,
// then files carrying both REPO_CONTAINS and CONTAINS. Seeding by hand in a
// different shape would prove the statement against a graph production never
// writes.
func seedLive6541Graph(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext) {
	t.Helper()

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: "nornic",
		AccessMode:   neo4jdriver.AccessModeWrite,
	})
	defer func() { _ = session.Close(ctx) }()

	statements := []string{
		fmt.Sprintf(`MERGE (r:Repository {id:%q}) SET r.name=%q, r.path=%q, r.local_path=%q, r.evidence_source='projector/canonical'`,
			live6541Alpha, live6541AlphaName, "/live6541/alpha", "/live6541/alpha"),
		fmt.Sprintf(`MERGE (r:Repository {id:%q}) SET r.name=%q, r.path=%q, r.local_path=%q, r.evidence_source='projector/canonical'`,
			live6541Beta, live6541BetaName, "/live6541/beta", "/live6541/beta"),
	}
	for _, dir := range live6541Fixture() {
		statements = append(statements, fmt.Sprintf(
			`MERGE (d:Directory {path:%q}) SET d.name=%q, d.repo_id=%q, d.scope_id=%q, d.generation_id='g6541', d.evidence_source='projector/canonical'`,
			dir.path, dir.name, dir.repoID, dir.repoID))
	}
	for _, dir := range live6541Fixture() {
		if dir.parent == "" {
			statements = append(statements, fmt.Sprintf(
				`MATCH (r:Repository {id:%q}) MATCH (d:Directory {path:%q}) MERGE (r)-[:CONTAINS]->(d)`,
				dir.repoID, dir.path))
			continue
		}
		statements = append(statements, fmt.Sprintf(
			`MATCH (p:Directory {path:%q}) MATCH (d:Directory {path:%q}) MERGE (p)-[:CONTAINS]->(d)`,
			dir.parent, dir.path))
	}
	for _, dir := range live6541Fixture() {
		for file, language := range dir.files {
			path := dir.path + "/" + file
			statements = append(statements, fmt.Sprintf(
				`MATCH (r:Repository {id:%q}) MATCH (d:Directory {path:%q}) MERGE (f:File {path:%q}) `+
					`SET f.name=%q, f.relative_path=%q, f.language=%q, f.lang=%q, f.repo_id=%q, f.evidence_source='projector/canonical' `+
					`MERGE (r)-[:REPO_CONTAINS]->(f) MERGE (d)-[:CONTAINS]->(f)`,
				dir.repoID, dir.path, path, file, file, language, language, dir.repoID))
		}
	}
	for _, statement := range statements {
		if _, err := session.Run(ctx, statement, nil); err != nil {
			t.Fatalf("seed statement %q: %v", statement, err)
		}
	}
}

// live6541Reader is a minimal querycontract.GraphQuery over a Bolt session.
//
// Package query's production Neo4jReader cannot be used here: package query
// imports this package, so a test in this package importing it back would be an
// import cycle. This adapter runs the statement the handler built, verbatim,
// against the same driver, which is what the proof needs -- it adds no
// retry, deadline or rewriting of its own.
type live6541Reader struct {
	driver neo4jdriver.DriverWithContext
}

func (r live6541Reader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	session := r.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: "nornic",
		AccessMode:   neo4jdriver.AccessModeRead,
	})
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

func (r live6541Reader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	rows, err := r.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// live6541Handler is the production handler bound to the live backend.
func live6541Handler(driver neo4jdriver.DriverWithContext) *Handler {
	return &Handler{Neo4j: live6541Reader{driver: driver}}
}

// live6541Grant grants exactly the named repositories.
func live6541Grant(repoIDs ...string) codequery.LanguageQueryGrant {
	return codequery.LanguageQueryGrant{Access: querycontract.RepositoryAccessFilter{
		AllowedRepositoryIDs: repoIDs,
	}}
}

// live6541Counts indexes a result page by directory name.
func live6541Counts(rows []map[string]any) map[string]int {
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[querycontract.StringVal(row, "name")] = querycontract.IntVal(row, "file_count")
	}
	return counts
}

// TestLiveNornicDBDirectoryLanguageQueryCountsNestedDirectories is the
// correctness proof. Every count is hand-computed from live6541Fixture, and
// nesting is the point: deep's file must stay in deep, inner's three must stay
// in inner, and neither may be folded into a parent.
func TestLiveNornicDBDirectoryLanguageQueryCountsNestedDirectories(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver := openLive6541Driver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLive6541Graph(ctx, t, driver)

	rows, err := live6541Handler(driver).directoryRowsByLanguage(ctx, "go", "", "", 50,
		live6541Grant(live6541Alpha, live6541Beta))
	if err != nil {
		t.Fatalf("directoryRowsByLanguage() error = %v, want nil", err)
	}

	counts := live6541Counts(rows)
	want := map[string]int{"lib": 4, "inner": 3, "src": 2, "cmd": 2, "pkg": 1, "deep": 1}
	if len(rows) != len(want) {
		t.Fatalf("rows = %d, want %d: %#v", len(rows), len(want), counts)
	}
	for name, wantCount := range want {
		got, ok := counts[name]
		if !ok {
			t.Fatalf("directory %q missing; a shape that drops a nested directory looks like this: %#v", name, counts)
		}
		if got != wantCount {
			t.Fatalf("directory %q counted %d file(s), want %d; a shape that folds a nested directory into its parent looks like this: %#v",
				name, got, wantCount, counts)
		}
	}
	for _, name := range []string{"docs", "tool"} {
		if _, ok := counts[name]; ok {
			t.Fatalf("directory %q has no go file but was returned: %#v", name, counts)
		}
	}

	if got := querycontract.StringVal(rows[0], "name"); got != "lib" {
		t.Fatalf("first row = %q, want \"lib\" (ORDER BY file_count DESC)", got)
	}
	for _, row := range rows {
		wantName := live6541AlphaName
		if querycontract.StringVal(row, "repo_id") == live6541Beta {
			wantName = live6541BetaName
		}
		if got := querycontract.StringVal(row, "repo_name"); got != wantName {
			t.Fatalf("row %q repo_name = %q, want %q; the second read is what fills this column",
				querycontract.StringVal(row, "name"), got, wantName)
		}
		// Pre-existing gap, recorded so a later fix is visible here: the
		// canonical projector writes neither d.id nor d.relative_path, so both
		// columns have always been empty for directory rows.
		if got := querycontract.StringVal(row, "entity_id"); got != "" {
			t.Fatalf("row %q entity_id = %q; the projector started writing d.id, so #6541's note is stale",
				querycontract.StringVal(row, "name"), got)
		}
	}
}

// TestLiveNornicDBDirectoryLanguageQueryTruncatesToTheGlobalTopN proves the
// half of the row bound the statement cannot do on this build, which applies
// ORDER BY/LIMIT once per unwound repository id. With two repositories and a
// limit of 2 the backend returns four rows; the caller must still get the two
// largest directories across both.
func TestLiveNornicDBDirectoryLanguageQueryTruncatesToTheGlobalTopN(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver := openLive6541Driver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLive6541Graph(ctx, t, driver)

	rows, err := live6541Handler(driver).directoryRowsByLanguage(ctx, "go", "", "", 2,
		live6541Grant(live6541Alpha, live6541Beta))
	if err != nil {
		t.Fatalf("directoryRowsByLanguage() error = %v, want nil", err)
	}

	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2; the per-repository superset reached the caller: %#v", len(rows), live6541Counts(rows))
	}
	if got := querycontract.StringVal(rows[0], "name"); got != "lib" {
		t.Fatalf("first row = %q, want \"lib\"", got)
	}
	if got := querycontract.StringVal(rows[1], "name"); got != "inner" {
		t.Fatalf("second row = %q, want \"inner\"", got)
	}
}

// TestLiveNornicDBDirectoryLanguageQueryRepeatsRowsForARepeatedID is the
// duplicate-id control, and it is why the resolved list must be deduplicated.
//
// It asserts the HAZARD directly against the backend, and the hazard is not
// what a Neo4j reading of this statement predicts. A single global aggregation
// would see each file twice through a repeated id and DOUBLE that directory's
// file_count. This build instead runs everything after the UNWIND once per id,
// so each group aggregates correctly and the repeat duplicates whole ROWS:
// eight rows for four directories, every file_count unchanged. The page is
// still corrupt -- a caller reading it sees each directory twice, and the limit
// is spent on repeats -- but it is corrupt in a different way, so a caller must
// not rely on either shape.
//
// Then it asserts the production path is immune, with a grant that names the
// same repository in BOTH grant lists, the shape RepositorySearchIDs
// deduplicates.
func TestLiveNornicDBDirectoryLanguageQueryRepeatsRowsForARepeatedID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver := openLive6541Driver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLive6541Graph(ctx, t, driver)

	handler := live6541Handler(driver)

	// The hazard, straight from the builder with a list production would never
	// hand it.
	cypher, params := buildDirectoryCypher("go", "", []string{live6541Alpha, live6541Alpha}, map[string]any{"limit": 50})
	repeated, err := handler.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("repeated-id statement: %v", err)
	}
	if len(repeated) != 8 {
		t.Fatalf("a repeated id returned %d row(s), want 8 (alpha's four go directories, twice): %#v\n"+
			"If this build stopped repeating rows, the dedup below is no longer load-bearing here and this control must be re-measured.",
			len(repeated), live6541Counts(repeated))
	}
	if got := live6541Counts(repeated)["inner"]; got != 3 {
		t.Fatalf("a repeated id counted inner as %d, want 3; this build has started aggregating the whole result at once, "+
			"which is the shape that would double a count instead of repeating a row", got)
	}

	// The production path, with one repository granted twice over.
	grant := codequery.LanguageQueryGrant{Access: querycontract.RepositoryAccessFilter{
		AllowedRepositoryIDs: []string{live6541Alpha},
		AllowedScopeIDs:      []string{live6541Alpha},
		Allowed:              map[string]struct{}{live6541Alpha: {}},
	}}
	rows, err := handler.directoryRowsByLanguage(ctx, "go", "", "", 50, grant)
	if err != nil {
		t.Fatalf("directoryRowsByLanguage() error = %v, want nil", err)
	}
	counts := live6541Counts(rows)
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4 (alpha's go directories, each once): %#v", len(rows), counts)
	}
	for name, want := range map[string]int{"inner": 3, "src": 2, "pkg": 1, "deep": 1} {
		if got := counts[name]; got != want {
			t.Fatalf("directory %q counted %d file(s), want %d; the grant's id list reached the statement with a duplicate: %#v",
				name, got, want, counts)
		}
	}
}

// TestLiveNornicDBDirectoryLanguageQueryHonoursTheGrant is the negative control
// pair: a grant of one repository must not leak the other, and a grant that
// names a repository which does not exist must return nothing rather than
// everything.
func TestLiveNornicDBDirectoryLanguageQueryHonoursTheGrant(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver := openLive6541Driver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLive6541Graph(ctx, t, driver)

	handler := live6541Handler(driver)

	scoped, err := handler.directoryRowsByLanguage(ctx, "go", "", "", 50, live6541Grant(live6541Alpha))
	if err != nil {
		t.Fatalf("directoryRowsByLanguage() error = %v, want nil", err)
	}
	counts := live6541Counts(scoped)
	if len(scoped) != 4 {
		t.Fatalf("rows = %d, want 4 (alpha's go directories): %#v", len(scoped), counts)
	}
	for _, row := range scoped {
		if got := querycontract.StringVal(row, "repo_id"); got != live6541Alpha {
			t.Fatalf("a caller granted only alpha received a row from %q: %#v", got, row)
		}
	}

	impossible, err := handler.directoryRowsByLanguage(ctx, "go", "", "", 50,
		live6541Grant("repo://live6541/does-not-exist"))
	if err != nil {
		t.Fatalf("directoryRowsByLanguage() error = %v, want nil", err)
	}
	if len(impossible) != 0 {
		t.Fatalf("a grant matching nothing returned %d row(s): %#v", len(impossible), live6541Counts(impossible))
	}
}
