// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_language_imports_grant

// Live proof that a page of directory rows is determined by the data rather
// than by which rows the backend happened to keep (#6541 review, F1).
//
// This is a separate fixture from directory_nornicdb_live_test.go's on purpose.
// That one mixes languages and nesting depths and every directory has a
// different file count, so ties never straddle the row bound and the defect
// this file is about is invisible on it. Here EVERY directory holds exactly one
// go file, which is the shape the corpus recipe seeds (ten files per directory,
// all the same language) and therefore the shape production is most likely to
// meet at scale.
//
// The mechanism: the pinned builds apply `ORDER BY ... LIMIT` once per UNWOUND
// id, so the statement's own ORDER BY decides which rows each repository's
// group KEEPS. sortAndTruncateDirectoryRows cannot recover a row a group
// dropped -- no re-sort returns a row the backend never sent -- so unless the
// statement orders on the handler's whole total order, page MEMBERSHIP is
// backend-arbitrary among ties. Measured, at limit 4 over five tied
// directories, `ORDER BY file_count DESC` alone kept a5,a3,a2,a1 on the v1.2.1
// pin and a1,a2,a4,a3 on v1.3.1: two different pages, same statement, same
// data.
//
//	docker run -d --name eshu-6541fix -e NORNICDB_EMBEDDING_ENABLED=false \
//	  -e NORNICDB_NO_AUTH=true -p 127.0.0.1:17925:7687 \
//	  eshu-nornicdb-pr290:3722b483c02c
//	cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:17925 go test ./internal/query/language \
//	  -tags live_nornicdb_language_imports_grant \
//	  -run TestLiveNornicDBDirectoryLanguageQueryBreaksTiesDeterministically \
//	  -count=1 -v
package language

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const (
	live6541TieAlpha     = "repo://live6541tie/alpha"
	live6541TieBeta      = "repo://live6541tie/beta"
	live6541TieAlphaName = "alpha-tie-6541"
	live6541TieBetaName  = "beta-tie-6541"
	// live6541TieWidth directories per repository, all tied. It is wider than
	// the limit the test pages at, so each group's bound has to choose, and
	// wide enough that an arbitrary choice is very unlikely to coincide with
	// the intended one.
	live6541TieWidth = 8
	live6541TieLimit = 4
)

// live6541TieNames returns the directory names of one repository in the total
// order the route promises: every count ties, so name decides.
func live6541TieNames(prefix string) []string {
	names := make([]string, 0, live6541TieWidth)
	for i := 1; i <= live6541TieWidth; i++ {
		names = append(names, fmt.Sprintf("%s%02d", prefix, i))
	}
	return names
}

// seedLive6541TieGraph writes two repositories whose directories all hold
// exactly one go file, through the canonical projector's write shapes and with
// the production directory_repo_id index in place.
func seedLive6541TieGraph(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext) {
	t.Helper()

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: "nornic",
		AccessMode:   neo4jdriver.AccessModeWrite,
	})
	defer func() { _ = session.Close(ctx) }()

	statements := []string{
		`CREATE INDEX directory_repo_id IF NOT EXISTS FOR (d:Directory) ON (d.repo_id)`,
		fmt.Sprintf(`MERGE (r:Repository {id:%q}) SET r.name=%q, r.path=%q, r.local_path=%q, r.evidence_source='projector/canonical'`,
			live6541TieAlpha, live6541TieAlphaName, "/live6541tie/alpha", "/live6541tie/alpha"),
		fmt.Sprintf(`MERGE (r:Repository {id:%q}) SET r.name=%q, r.path=%q, r.local_path=%q, r.evidence_source='projector/canonical'`,
			live6541TieBeta, live6541TieBetaName, "/live6541tie/beta", "/live6541tie/beta"),
	}
	for _, repo := range []struct{ id, prefix, root string }{
		{live6541TieAlpha, "a", "/live6541tie/alpha"},
		{live6541TieBeta, "b", "/live6541tie/beta"},
	} {
		// Seeded in reverse of the intended order, so a backend that returned
		// insertion order would not accidentally look sorted.
		names := live6541TieNames(repo.prefix)
		for i := len(names) - 1; i >= 0; i-- {
			name := names[i]
			path := repo.root + "/" + name
			statements = append(statements,
				fmt.Sprintf(`MERGE (d:Directory {path:%q}) SET d.name=%q, d.repo_id=%q, d.scope_id=%q, d.generation_id='g6541tie', d.evidence_source='projector/canonical'`,
					path, name, repo.id, repo.id),
				fmt.Sprintf(`MATCH (r:Repository {id:%q}) MATCH (d:Directory {path:%q}) MERGE (r)-[:CONTAINS]->(d)`,
					repo.id, path),
				fmt.Sprintf(`MATCH (r:Repository {id:%q}) MATCH (d:Directory {path:%q}) MERGE (f:File {path:%q}) `+
					`SET f.name='one.go', f.relative_path='one.go', f.language='go', f.lang='go', f.repo_id=%q, f.evidence_source='projector/canonical' `+
					`MERGE (r)-[:REPO_CONTAINS]->(f) MERGE (d)-[:CONTAINS]->(f)`,
					repo.id, path, path+"/one.go", repo.id),
			)
		}
	}
	for _, statement := range statements {
		if _, err := session.Run(ctx, statement, nil); err != nil {
			t.Fatalf("seed statement %q: %v", statement, err)
		}
	}
}

// live6541TiePage returns the directory names of one page, in the order the
// caller received them.
func live6541TiePage(rows []map[string]any) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, querycontract.StringVal(row, "name"))
	}
	return names
}

// TestLiveNornicDBDirectoryLanguageQueryBreaksTiesDeterministically is the F1
// pin: when every row ties on file_count, the page must still be the total
// order's top-L, and it must be the same page every time.
func TestLiveNornicDBDirectoryLanguageQueryBreaksTiesDeterministically(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver := openLive6541Driver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLive6541TieGraph(ctx, t, driver)

	handler := live6541Handler(driver)
	grant := live6541Grant(live6541TieAlpha, live6541TieBeta)

	// Every count ties, so the total order is repo_id then name, and
	// live6541TieAlpha sorts before live6541TieBeta.
	wantPage := live6541TieNames("a")[:live6541TieLimit]

	page := func(limit int) []string {
		t.Helper()
		rows, err := handler.directoryRowsByLanguage(ctx, "go", "", "", limit, grant)
		if err != nil {
			t.Fatalf("directoryRowsByLanguage(limit=%d) error = %v, want nil", limit, err)
		}
		for _, row := range rows {
			if got := querycontract.IntVal(row, "file_count"); got != 1 {
				t.Fatalf("directory %q counted %d file(s), want 1; this fixture is only a tie fixture if every count is equal",
					querycontract.StringVal(row, "name"), got)
			}
		}
		return live6541TiePage(rows)
	}

	first := page(live6541TieLimit)
	if strings.Join(first, ",") != strings.Join(wantPage, ",") {
		t.Fatalf("page = %v, want %v.\n"+
			"Every directory holds one file, so the page is decided entirely by the tie-break. A page that is "+
			"neither this nor a stable permutation means the statement's per-group bound dropped tied rows the "+
			"handler cannot recover -- which is what ordering the statement on the handler's whole total order fixes.",
			first, wantPage)
	}

	// Defeat the build's last-result cache with a differently-shaped read
	// between the two timed-identical ones, then require the same page again.
	if got := page(live6541TieLimit + 1); len(got) != live6541TieLimit+1 {
		t.Fatalf("page at limit %d returned %d row(s): %v", live6541TieLimit+1, len(got), got)
	}
	second := page(live6541TieLimit)
	if strings.Join(second, ",") != strings.Join(first, ",") {
		t.Fatalf("two identical requests returned different pages: %v then %v", first, second)
	}

	// The other repository's group must be bounded by the same order, which a
	// page drawn only from alpha never exercises.
	betaRows, err := handler.directoryRowsByLanguage(ctx, "go", "", "", live6541TieLimit,
		live6541Grant(live6541TieBeta))
	if err != nil {
		t.Fatalf("directoryRowsByLanguage(beta) error = %v, want nil", err)
	}
	wantBeta := live6541TieNames("b")[:live6541TieLimit]
	if got := live6541TiePage(betaRows); strings.Join(got, ",") != strings.Join(wantBeta, ",") {
		t.Fatalf("beta page = %v, want %v; beta's group bound kept a different set of tied rows", got, wantBeta)
	}
	for _, row := range betaRows {
		if got := querycontract.StringVal(row, "repo_name"); got != live6541TieBetaName {
			t.Fatalf("row %q repo_name = %q, want %q", querycontract.StringVal(row, "name"), got, live6541TieBetaName)
		}
	}
}
