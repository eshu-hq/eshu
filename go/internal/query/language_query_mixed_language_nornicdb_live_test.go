// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_language_imports_grant

// Live proof, on the pinned NornicDB build, that the language-query graph
// builders admit only the requested language when the store holds several.
//
// The grant fixture next door (language_query_grant_nornicdb_live_test.go) is
// entirely Python, so a language predicate that admits every row passes it:
// every row IS Python. That is how #6546 shipped unnoticed. Until that fix the
// File, Directory and entity builders OR-ed `f.name ENDS WITH '<ext>'` terms
// into their WHERE, and on this build ENDS WITH evaluates as true for every row
// of a multi-node MATCH -- so the language filter selected nothing. The
// bisection that established it is recorded in
// docs/public/reference/nornicdb-path-predicate-pitfalls.md and in
// docs/internal/evidence/6546-language-query-extension-filter.md.
//
// This file seeds one polyglot repository and asks each builder for one
// language at a time. A Go query must return the Go rows and nothing else; a
// typescript query must reach the file the tsx parser stamped `tsx`; a csharp
// query must reach the file the C# parser stamped `c_sharp`. Those two prove
// that dropping the extension fallback did not lose the parser spellings the
// fallback used to paper over. The Repository builder never carried the
// fallback, but its two equalities never reached those spellings either: on
// this fixture a csharp repository query answered zero repositories and a
// typescript one counted one file instead of two, so it is asked the same
// questions here.
//
// The entity builder reads the language from the entity first and from its
// File second (`e.language IN $languages OR f.language IN $languages`),
// because a few entity kinds are projected without their own `language`. One
// Function in this fixture is seeded with no `language` property at all, under
// the Python file, so the left disjunct is evaluated against a missing
// property on this build. A python entity query must reach it through the
// File's language and report that language for it; a go entity query must
// not, which is the row that would leak if a missing-property `IN` evaluated
// as true the way ENDS WITH did.
//
// The fixture is shaped to stay out of the grant proof's way, since both run
// under one build tag against one store. The grant proof's unscoped controls
// take a page of two or three rows and require every row to belong to the
// out-of-grant repository; this repository's relative paths start with
// `zz-mixed`, so under ORDER BY relative_path they sort after that
// repository's `a-src-*` paths, and its single directory holds exactly one
// Python file, so under ORDER BY file_count DESC it never outranks the
// out-of-grant directories that hold two.
//
// Run with the recipe in language_query_grant_nornicdb_live_test.go.
package query

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	liveMixedRepo   = "repo://live-mixed/polyglot"
	liveMixedMarker = "live-mixed"
	liveMixedDir    = "zz-mixed"

	// liveMixedUntaggedFile carries the Function seeded without a `language`
	// property; liveMixedUntaggedLanguage is that file's own language, the
	// only one the entity builder can read for it.
	liveMixedUntaggedFile     = "gamma.py"
	liveMixedUntaggedLanguage = "python"
	liveMixedUntaggedName     = "mixed_untagged"
)

// liveMixedFile is one seeded file: its name, and the `language` value the
// projector would have written for it, which is the parser's own spelling
// rather than the query DSL's canonical one where the two differ.
type liveMixedFile struct {
	name     string
	language string
}

// liveMixedFiles is the polyglot directory. Exactly one Python file, so the
// grant proof's Directory squeeze is undisturbed (see the file header).
var liveMixedFiles = []liveMixedFile{
	{name: "alpha.go", language: "go"},
	{name: "beta.go", language: "go"},
	{name: "gamma.py", language: "python"},
	{name: "delta.tsx", language: "tsx"},
	{name: "epsilon.cs", language: "c_sharp"},
	{name: "zeta.ts", language: "typescript"},
	{name: "eta.hcl", language: "hcl"},
}

// liveMixedCase asks one builder for one language and names the files whose
// rows must come back, in full: any other row from this repository is a leak.
type liveMixedCase struct {
	label     string
	language  string
	wantFiles []string
}

// TestLiveNornicDBLanguageQueryAdmitsOnlyTheRequestedLanguage is the mixed
// language proof for #6546. It fails on the pre-fix builders with every
// non-Go file of the fixture in the Go answer.
func TestLiveNornicDBLanguageQueryAdmitsOnlyTheRequestedLanguage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver := openLiveGrantDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLiveMixedGraph(ctx, t, driver)

	for _, testCase := range []liveMixedCase{
		{label: "File", language: "go", wantFiles: []string{"alpha.go", "beta.go"}},
		{label: "Function", language: "go", wantFiles: []string{"alpha.go", "beta.go"}},
		{label: "File", language: "python", wantFiles: []string{"gamma.py"}},
		// typescript reaches both the typescript and the tsx spelling; csharp
		// reaches the parser's c_sharp. The extension fallback used to be the
		// only thing admitting these on a backend that honours ENDS WITH.
		{label: "File", language: "typescript", wantFiles: []string{"delta.tsx", "zeta.ts"}},
		{label: "Function", language: "typescript", wantFiles: []string{"delta.tsx", "zeta.ts"}},
		{label: "File", language: "csharp", wantFiles: []string{"epsilon.cs"}},
		{label: "File", language: "hcl", wantFiles: []string{"eta.hcl"}},
	} {
		name := testCase.label + " " + testCase.language
		t.Run(name, func(t *testing.T) {
			cypher, params := buildLanguageCypherWithSemanticFilter(
				testCase.language, testCase.label, "", liveMixedRepo, 50, "", "", liveGrantUnscopedAccess(),
			)
			rows := runLiveGrantStatement(ctx, t, driver, name, cypher, params)
			got := liveMixedFileNames(rows)
			if !slices.Equal(got, testCase.wantFiles) {
				t.Fatalf("%s returned files %v, want exactly %v; a language filter the backend does not apply looks like every file of the repository", name, got, testCase.wantFiles)
			}
		})
	}

	// The Repository builder aggregates, so the answer is one row per
	// repository with a file_count rather than one row per file.
	for _, testCase := range []struct {
		language  string
		wantCount int
	}{
		{language: "go", wantCount: 2},
		{language: "csharp", wantCount: 1},
		{language: "typescript", wantCount: 2},
	} {
		name := "Repository " + testCase.language
		t.Run(name, func(t *testing.T) {
			cypher, params := buildLanguageCypherWithSemanticFilter(
				testCase.language, "Repository", "", liveMixedRepo, 50, "", "", liveGrantUnscopedAccess(),
			)
			rows := runLiveGrantStatement(ctx, t, driver, name, cypher, params)
			if len(rows) != 1 {
				t.Fatalf("%s returned %d row(s), want the one seeded repository; zero means the builder missed the parser's spelling: %#v", name, len(rows), rows)
			}
			if got := IntVal(rows[0], "file_count"); got != testCase.wantCount {
				t.Fatalf("%s counted %d file(s), want %d", name, got, testCase.wantCount)
			}
		})
	}

	t.Run("Directory go", func(t *testing.T) {
		cypher, params := buildLanguageCypherWithSemanticFilter(
			"go", "Directory", "", liveMixedRepo, 50, "", "", liveGrantUnscopedAccess(),
		)
		rows := runLiveGrantStatement(ctx, t, driver, "Directory go", cypher, params)
		if len(rows) != 1 {
			t.Fatalf("Directory go returned %d row(s), want the one seeded directory: %#v", len(rows), rows)
		}
		if got, want := IntVal(rows[0], "file_count"), 2; got != want {
			t.Fatalf("Directory go counted %d file(s), want %d; a count of %d means every file was admitted regardless of language", got, want, len(liveMixedFiles))
		}
	})

	// The Function without a `language` property answers through its File's
	// language and nothing else. Both cases run the shipped entity text, so
	// the missing-property `IN` is executed on this build in both the
	// admitting and the excluding position.
	untaggedID := liveMixedUntaggedFunctionID()
	t.Run("Function python reaches the untagged function through its file", func(t *testing.T) {
		name := "Function " + liveMixedUntaggedLanguage
		cypher, params := buildLanguageCypherWithSemanticFilter(
			liveMixedUntaggedLanguage, "Function", "", liveMixedRepo, 50, "", "", liveGrantUnscopedAccess(),
		)
		rows := runLiveGrantStatement(ctx, t, driver, name, cypher, params)
		if got, want := liveMixedEntityNames(rows), []string{"mixed_" + liveMixedUntaggedLanguage, liveMixedUntaggedName}; !slices.Equal(got, want) {
			t.Fatalf("%s returned entities %v, want exactly %v; the untagged one is missing when the builder does not fall back to f.language", name, got, want)
		}
		for _, row := range rows {
			if StringVal(row, "entity_id") != untaggedID {
				continue
			}
			if got := StringVal(row, "language"); got != liveMixedUntaggedLanguage {
				t.Fatalf("%s reported language %q for the untagged function, want its file's %q", name, got, liveMixedUntaggedLanguage)
			}
			if got := StringVal(row, "file_path"); !strings.HasSuffix(got, "/"+liveMixedUntaggedFile) {
				t.Fatalf("%s placed the untagged function in %q, want %s", name, got, liveMixedUntaggedFile)
			}
		}
	})
	t.Run("Function go excludes the untagged function", func(t *testing.T) {
		cypher, params := buildLanguageCypherWithSemanticFilter(
			"go", "Function", "", liveMixedRepo, 50, "", "", liveGrantUnscopedAccess(),
		)
		rows := runLiveGrantStatement(ctx, t, driver, "Function go", cypher, params)
		for _, row := range rows {
			if StringVal(row, "entity_id") == untaggedID {
				t.Fatalf("Function go returned the untagged function %s under %s; a missing e.language must not satisfy IN $languages", untaggedID, StringVal(row, "file_path"))
			}
		}
		if got, want := liveMixedFileNames(rows), []string{"alpha.go", "beta.go"}; !slices.Equal(got, want) {
			t.Fatalf("Function go returned files %v, want exactly %v", got, want)
		}
	})
}

// liveMixedUntaggedFunctionID is the uid and id of the Function seeded without
// a `language` property. It differs from the per-file `fn:` id so the two
// Functions under the same file stay distinct rows.
func liveMixedUntaggedFunctionID() string {
	return "fn-untagged:/live/" + liveMixedMarker + "/" + liveMixedDir + "/" + liveMixedUntaggedFile
}

// liveMixedEntityNames reduces an entity page to its sorted entity names.
func liveMixedEntityNames(rows []map[string]any) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, StringVal(row, "name"))
	}
	sort.Strings(names)
	return names
}

// liveMixedFileNames reduces a page to the sorted base names of the files it
// came from, so a case can compare the whole answer at once.
func liveMixedFileNames(rows []map[string]any) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		filePath := StringVal(row, "file_path")
		if at := strings.LastIndex(filePath, "/"); at >= 0 {
			filePath = filePath[at+1:]
		}
		names = append(names, filePath)
	}
	sort.Strings(names)
	return names
}

// seedLiveMixedGraph writes the polyglot repository through the same shapes
// the canonical projector writes (see seedLiveGrantGraph); MERGE keeps a
// repeated run against a retained store idempotent.
func seedLiveMixedGraph(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext) {
	t.Helper()

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: "nornic",
		AccessMode:   neo4jdriver.AccessModeWrite,
	})
	defer func() { _ = session.Close(ctx) }()

	repoPath := "/live/" + liveMixedMarker
	dirPath := repoPath + "/" + liveMixedDir
	statements := []string{
		fmt.Sprintf(`MERGE (r:Repository {id:%q}) SET r.name=%q, r.path=%q, r.local_path=%q`,
			liveMixedRepo, liveMixedMarker+"-service", repoPath, repoPath),
		fmt.Sprintf(`MERGE (d:Directory {path:%q}) SET d.name=%q, d.repo_id=%q`, dirPath, liveMixedDir, liveMixedRepo),
		fmt.Sprintf(`MATCH (r:Repository {id:%q}),(d:Directory {path:%q}) MERGE (r)-[:CONTAINS]->(d)`, liveMixedRepo, dirPath),
	}
	for _, file := range liveMixedFiles {
		relativePath := liveMixedDir + "/" + file.name
		filePath := dirPath + "/" + file.name
		function := "fn:" + filePath
		statements = append(statements,
			fmt.Sprintf(`MERGE (f:File {path:%q}) SET f.name=%q, f.relative_path=%q, f.uid=%q, f.language=%q, f.lang=%q, f.repo_id=%q`,
				filePath, file.name, relativePath, "file:"+filePath, file.language, file.language, liveMixedRepo),
			fmt.Sprintf(`MATCH (r:Repository {id:%q}),(f:File {path:%q}) MERGE (r)-[:REPO_CONTAINS]->(f)`, liveMixedRepo, filePath),
			fmt.Sprintf(`MATCH (d:Directory {path:%q}),(f:File {path:%q}) MERGE (d)-[:CONTAINS]->(f)`, dirPath, filePath),
			fmt.Sprintf(`MATCH (f:File {path:%q}) MERGE (n:Function {uid:%q}) SET n.id=%q, n.name=%q, n.language=%q, n.lang=%q, n.repo_id=%q, n.start_line=1, n.end_line=2 MERGE (f)-[:CONTAINS]->(n)`,
				filePath, function, function, "mixed_"+file.language, file.language, file.language, liveMixedRepo),
		)
	}
	// One Function with no `language` (and no `lang`) property at all, under
	// the Python file: the builder can only reach it through f.language.
	untaggedPath := dirPath + "/" + liveMixedUntaggedFile
	untagged := liveMixedUntaggedFunctionID()
	statements = append(statements,
		fmt.Sprintf(`MATCH (f:File {path:%q}) MERGE (n:Function {uid:%q}) SET n.id=%q, n.name=%q, n.repo_id=%q, n.start_line=1, n.end_line=2 MERGE (f)-[:CONTAINS]->(n)`,
			untaggedPath, untagged, untagged, liveMixedUntaggedName, liveMixedRepo),
	)
	for _, stmt := range statements {
		if _, err := session.Run(ctx, stmt, nil); err != nil {
			t.Fatalf("seed statement %q: %v", stmt, err)
		}
	}
}
