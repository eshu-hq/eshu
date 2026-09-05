// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_nornicdb_language_imports_grant

// Live proof, on the pinned NornicDB build, for every scoped statement shape
// #5167 batch 2a adds to POST /api/v0/code/language-query and
// POST /api/v0/code/imports/investigate.
//
// The evidence doc originally waived a live run on the argument that every
// builder binds Repository in a required MATCH, so the grant cannot null a
// projection the way the batch-1 OPTIONAL MATCH trap did. That argument is
// sound about clause attachment and says nothing about the backend: whether the
// pinned executor parses the statement the builders now emit, whether it
// applies the added predicate at all, and whether it applies it before the row
// bound rather than after. Those are the questions this file answers.
//
// Each shipped builder is exercised twice against a two-repository graph seeded
// through the same labels, relationship types and properties the canonical
// projector writes (go/internal/storage/cypher/canonical_node_cypher.go and
// semantic_entity_statements.go): once with the caller's grant and once
// unscoped. The out-of-grant repository is seeded with SIX rows to the granted
// repository's ONE, sorts first under every builder's ORDER BY, and the row
// bound is set below the out-of-grant count. So:
//
//   - the unscoped run returns only out-of-grant rows -- the page is full
//     before the granted row is reached;
//   - the scoped run returns the granted row and no out-of-grant row.
//
// That difference is only possible if the grant predicate decides membership
// while the anchoring MATCH is producing rows. A predicate applied to the
// statement's output after the bound would leave the scoped answer empty, which
// is exactly what this asserts against. It is the observable stand-in for a
// plan read, because the pinned build reports no plan: see
// TestLiveNornicDBGrantPlanShapeIsNotReportable.
//
// Run against the pinned proof image:
//
//	docker run -d --name nornic-5167-lang-imports \
//	  -e NORNICDB_EMBEDDING_ENABLED=false -e NORNICDB_NO_AUTH=true -p 17989:7687 \
//	  timothyswt/nornicdb-cpu-bge@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74
//	cd go && ESHU_NEO4J_URI=bolt://localhost:17989 go test ./internal/query \
//	  -tags live_nornicdb_language_imports_grant -run TestLiveNornicDB -count=1 -v
package query

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	// liveGrantRepo sorts AFTER liveGrantOtherRepo so the out-of-grant rows
	// come first under every builder's ORDER BY. That is what makes a row
	// bound below the out-of-grant count squeeze the granted row off the page
	// unless the grant is applied while the rows are produced.
	liveGrantRepo      = "repo://live-zeta/granted-service"
	liveGrantOtherRepo = "repo://live-alpha/other-service"

	// liveGrantOutOfGrantRows is how many files, directories, entities, import
	// edges and call edges the out-of-grant repository gets. Every bound below
	// is smaller.
	liveGrantOutOfGrantRows = 6

	liveGrantLanguage      = "python"
	liveGrantImportedModue = "live_imported_module"
	liveGrantSourceModule  = "live_source_module"
	liveGrantTargetModule  = "live_target_module"

	// liveGrantOtherMarker is woven into the id, name, path, relative path,
	// entity name and module name of every out-of-grant node, so a returned row
	// carrying it in ANY column is a leak no matter which columns that
	// builder projects. liveGrantGrantedMarker plays the same role for the
	// repository the caller was granted.
	liveGrantOtherMarker   = "live-alpha"
	liveGrantGrantedMarker = "live-zeta"
)

// liveGrantAccess is the caller: granted exactly one of the two repositories.
func liveGrantAccess() repositoryAccessFilter {
	return repositoryAccessFilter{AllowedRepositoryIDs: []string{liveGrantRepo}}
}

// liveGrantUnscopedAccess is the same request with no grant, used as the
// control that shows the out-of-grant rows really do fill the page.
func liveGrantUnscopedAccess() repositoryAccessFilter {
	return repositoryAccessFilter{AllScopes: true}
}

func openLiveGrantDriver(ctx context.Context, t *testing.T) neo4jdriver.DriverWithContext {
	t.Helper()

	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		uri = "bolt://localhost:17989"
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

// runLiveGrantStatement executes one shipped statement and returns its rows,
// failing the test if the pinned executor cannot parse or run it.
func runLiveGrantStatement(
	ctx context.Context,
	t *testing.T,
	driver neo4jdriver.DriverWithContext,
	label string,
	cypher string,
	params map[string]any,
) []map[string]any {
	t.Helper()

	started := time.Now()
	rows, err := NewNeo4jReader(driver, "nornic").Run(ctx, cypher, params)
	if err != nil {
		t.Fatalf("%s: the pinned backend rejected the shipped statement: %v\n%s", label, err, cypher)
	}
	t.Logf("%s: %d row(s) in %s", label, len(rows), time.Since(started))
	return rows
}

// assertLiveGrantRows is the per-statement verdict: the granted repository's
// rows are present and no value anywhere in any row belongs to the repository
// the caller was not granted.
func assertLiveGrantRows(t *testing.T, label string, rows []map[string]any) {
	t.Helper()

	if len(rows) == 0 {
		t.Fatalf("%s: the scoped statement returned nothing; the granted repository's rows must survive the grant", label)
	}
	for _, row := range rows {
		for column, value := range row {
			text, ok := value.(string)
			if !ok {
				continue
			}
			if strings.Contains(text, liveGrantOtherMarker) {
				t.Fatalf("%s: column %q carried %q, a value from the repository the caller was not granted: %#v", label, column, text, row)
			}
		}
	}
}

// assertLiveGrantSqueezed is the plan-shape stand-in, and it is the assertion
// the whole argument rests on. EVERY row of the unscoped control must belong to
// the out-of-grant repository: that is what proves the page fills before the
// granted repository is reached. Given that, a grant applied to the statement's
// OUTPUT -- after the bound -- would leave the scoped run with nothing, so the
// granted rows the scoped run does return can only have come from a predicate
// that decided membership while the anchoring MATCH was producing rows.
//
// A single out-of-grant row would not be enough: a page holding one row of each
// would survive a post-bound filter too, and the scoped result would prove
// nothing about when the predicate ran.
func assertLiveGrantSqueezed(t *testing.T, label string, rows []map[string]any) {
	t.Helper()

	if len(rows) == 0 {
		t.Fatalf("%s: the unscoped control returned nothing, so the squeeze it is supposed to establish did not happen", label)
	}
	for _, row := range rows {
		outOfGrant := false
		for column, value := range row {
			text, ok := value.(string)
			if !ok {
				continue
			}
			if strings.Contains(text, liveGrantGrantedMarker) {
				t.Fatalf("%s: the unscoped page carried the granted repository in column %q (%q), so it is not full of out-of-grant rows and the scoped result proves nothing about ordering: %#v",
					label, column, text, row)
			}
			if strings.Contains(text, liveGrantOtherMarker) {
				outOfGrant = true
			}
		}
		if !outOfGrant {
			t.Fatalf("%s: an unscoped row named neither repository, so the squeeze cannot be read from it: %#v", label, row)
		}
	}
}

// liveGrantCase is one shipped statement shape under proof. build takes the
// caller's grant so the scoped and unscoped runs go through the same shipped
// builder rather than through two hand-written texts.
type liveGrantCase struct {
	name   string
	build  func(access repositoryAccessFilter) (string, map[string]any)
	params map[string]any
}

// runLiveGrantCases runs every case scoped and unscoped and reports how many
// statement shapes were proved.
func runLiveGrantCases(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext, cases []liveGrantCase) {
	t.Helper()

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			scopedCypher, scopedParams := testCase.build(liveGrantAccess())
			for key, value := range testCase.params {
				scopedParams[key] = value
			}
			scoped := runLiveGrantStatement(ctx, t, driver, testCase.name+" scoped", scopedCypher, scopedParams)
			assertLiveGrantRows(t, testCase.name+" scoped", scoped)

			unscopedCypher, unscopedParams := testCase.build(liveGrantUnscopedAccess())
			for key, value := range testCase.params {
				unscopedParams[key] = value
			}
			unscoped := runLiveGrantStatement(ctx, t, driver, testCase.name+" unscoped", unscopedCypher, unscopedParams)
			assertLiveGrantSqueezed(t, testCase.name+" unscoped", unscoped)
		})
	}
	t.Logf("proved %d statement shape(s) against the pinned backend", len(cases))
}

// TestLiveNornicDBLanguageQueryGrantBindsEveryBuilder covers all four dispatch
// branches of buildLanguageCypherWithSemanticFilter that reach the graph,
// Directory included -- the single-clause rewrite is what lets that branch
// answer on this build at all. The shape it replaced, which still answers
// nothing here, is pinned in
// TestLiveNornicDBLanguageQueryDirectoryTwoClauseShapeReturnsNothing.
func TestLiveNornicDBLanguageQueryGrantBindsEveryBuilder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	driver := openLiveGrantDriver(ctx, t)
	defer func() { _ = driver.Close(context.Background()) }()
	seedLiveGrantGraph(ctx, t, driver)

	build := func(label string, limit int) func(repositoryAccessFilter) (string, map[string]any) {
		return func(access repositoryAccessFilter) (string, map[string]any) {
			return buildLanguageCypherWithSemanticFilter(
				liveGrantLanguage, label, "", "", limit, "", "", access,
			)
		}
	}
	runLiveGrantCases(ctx, t, driver, []liveGrantCase{
		// Repository: ORDER BY file_count DESC. The out-of-grant repository has
		// six files to the granted one's one, so a bound of 1 admits only it.
		{name: "buildRepositoryCypher", build: build("Repository", 1)},
		// File and entity: ORDER BY relative_path, and the out-of-grant paths
		// sort first.
		// Directory: ORDER BY file_count DESC. Each out-of-grant directory holds
		// two files; the granted repository's two hold one each, and the second
		// of them is nested a level down, so this also proves the rewritten
		// builder still walks the depth-N CONTAINS chain the projector writes.
		{name: "buildDirectoryCypher", build: build("Directory", 3)},
		{name: "buildFileCypher", build: build("File", 2)},
		{name: "buildEntityCypherWithSemanticFilter", build: build("Function", 2)},
		{
			name: "buildEntityCypherWithSemanticFilter guard",
			build: func(access repositoryAccessFilter) (string, map[string]any) {
				return buildLanguageCypherWithSemanticFilter(
					liveGrantLanguage, "Function", "", "", 2, "semantic_kind", "guard", access,
				)
			},
		},
	})
}

// seedLiveGrantGraph writes the two-repository fixture every case above and in
// the imports file reads.
//
// The shapes mirror what the canonical projector writes: Repository {id, name,
// path}, Directory {path, name, repo_id} reached by Repository-[:CONTAINS]->,
// File {path, relative_path, name, language, lang, repo_id} reached by
// Repository-[:REPO_CONTAINS]-> and Directory-[:CONTAINS]->, semantic entities
// MERGE'd on uid and reached by File-[:CONTAINS]->, canonical import targets as
// Module {name, lang} reached by File-[:IMPORTS]->, and Function-[:CALLS]->
// Function for the cross-module call path. MERGE keeps a repeated run against a
// retained store idempotent.
func seedLiveGrantGraph(ctx context.Context, t *testing.T, driver neo4jdriver.DriverWithContext) {
	t.Helper()

	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{
		DatabaseName: "nornic",
		AccessMode:   neo4jdriver.AccessModeWrite,
	})
	defer func() { _ = session.Close(ctx) }()

	statements := []string{
		`MERGE (m:Module {name:"` + liveGrantImportedModue + `", lang:"` + liveGrantLanguage + `"})`,
	}
	statements = append(statements, liveGrantRepositoryStatements(liveGrantRepo, "z-src", liveGrantGrantedMarker, 1)...)
	statements = append(statements, liveGrantNestedDirectoryStatements()...)
	statements = append(statements, liveGrantRepositoryStatements(liveGrantOtherRepo, "a-src", liveGrantOtherMarker, liveGrantOutOfGrantRows)...)
	for _, stmt := range statements {
		if _, err := session.Run(ctx, stmt, nil); err != nil {
			t.Fatalf("seed statement %q: %v", stmt, err)
		}
	}
}

// liveGrantRepositoryStatements writes one repository with fileCount files. The
// out-of-grant repository is called with a directory prefix that sorts first
// and a file count larger than every bound the cases use; the granted one gets
// one file under a prefix that sorts last.
//
// Each out-of-grant directory holds two files so it outranks the granted
// directory under buildDirectoryCypher's ORDER BY file_count DESC; the granted
// directory holds its single file.
func liveGrantRepositoryStatements(repoID, dirPrefix, marker string, fileCount int) []string {
	repoPath := "/live/" + marker
	statements := []string{
		fmt.Sprintf(`MERGE (r:Repository {id:%q}) SET r.name=%q, r.path=%q, r.local_path=%q`,
			repoID, marker+"-service", repoPath, repoPath),
	}
	directories := 1
	if fileCount > 1 {
		directories = fileCount / 2
	}
	for dir := 0; dir < directories; dir++ {
		dirPath := fmt.Sprintf("%s/%s-%d", repoPath, dirPrefix, dir)
		statements = append(statements,
			fmt.Sprintf(`MERGE (d:Directory {path:%q}) SET d.name=%q, d.repo_id=%q`,
				dirPath, fmt.Sprintf("%s-%d", dirPrefix, dir), repoID),
			fmt.Sprintf(`MATCH (r:Repository {id:%q}),(d:Directory {path:%q}) MERGE (r)-[:CONTAINS]->(d)`,
				repoID, dirPath),
		)
	}
	for index := 0; index < fileCount; index++ {
		dirPath := fmt.Sprintf("%s/%s-%d", repoPath, dirPrefix, index%directories)
		relativePath := fmt.Sprintf("%s-%d/%s-%d.py", dirPrefix, index%directories, marker, index)
		filePath := repoPath + "/" + relativePath
		statements = append(statements, liveGrantFileStatements(repoID, dirPath, filePath, relativePath, marker, index)...)
	}
	return statements
}

// liveGrantFileStatements writes one file, its repository and directory edges,
// the semantic entities the entity and module builders anchor on, its import
// edge, and the call pair the cross-module builder reads.
func liveGrantFileStatements(repoID, dirPath, filePath, relativePath, marker string, index int) []string {
	name := fmt.Sprintf("%s-%d.py", marker, index)
	caller := fmt.Sprintf("fn:%s-caller-%d", marker, index)
	callee := fmt.Sprintf("fn:%s-callee-%d", marker, index)
	return []string{
		fmt.Sprintf(`MERGE (f:File {path:%q}) SET f.name=%q, f.relative_path=%q, f.uid=%q, f.language=%q, f.lang=%q, f.repo_id=%q`,
			filePath, name, relativePath, "file:"+filePath, liveGrantLanguage, liveGrantLanguage, repoID),
		fmt.Sprintf(`MATCH (r:Repository {id:%q}),(f:File {path:%q}) MERGE (r)-[:REPO_CONTAINS]->(f)`, repoID, filePath),
		fmt.Sprintf(`MATCH (d:Directory {path:%q}),(f:File {path:%q}) MERGE (d)-[:CONTAINS]->(f)`, dirPath, filePath),
		fmt.Sprintf(`MATCH (f:File {path:%q}) MERGE (n:Function {uid:%q}) SET n.id=%q, n.name=%q, n.language=%q, n.lang=%q, n.repo_id=%q, n.start_line=10, n.end_line=20, n.semantic_kind="guard" MERGE (f)-[:CONTAINS]->(n)`,
			filePath, caller, caller, fmt.Sprintf("%sCaller%d", marker, index), liveGrantLanguage, liveGrantLanguage, repoID),
		fmt.Sprintf(`MATCH (f:File {path:%q}) MERGE (n:Function {uid:%q}) SET n.id=%q, n.name=%q, n.language=%q, n.lang=%q, n.repo_id=%q, n.start_line=30, n.end_line=40, n.semantic_kind="guard" MERGE (f)-[:CONTAINS]->(n)`,
			filePath, callee, callee, fmt.Sprintf("%sCallee%d", marker, index), liveGrantLanguage, liveGrantLanguage, repoID),
		fmt.Sprintf(`MATCH (a:Function {uid:%q}),(b:Function {uid:%q}) MERGE (a)-[c:CALLS]->(b) SET c.call_kind="direct", c.reason="live grant proof"`, caller, callee),
		fmt.Sprintf(`MATCH (f:File {path:%q}) MERGE (m:Module {uid:%q}) SET m.name=%q, m.lang=%q, m.language=%q, m.path=%q MERGE (f)-[:CONTAINS]->(m)`,
			filePath, "mod:src:"+filePath, liveGrantSourceModule, liveGrantLanguage, liveGrantLanguage, filePath),
		fmt.Sprintf(`MATCH (f:File {path:%q}) MERGE (m:Module {uid:%q}) SET m.name=%q, m.lang=%q, m.language=%q, m.path=%q MERGE (f)-[:CONTAINS]->(m)`,
			filePath, "mod:tgt:"+filePath, liveGrantTargetModule, liveGrantLanguage, liveGrantLanguage, filePath),
		fmt.Sprintf(`MATCH (f:File {path:%q}),(m:Module {name:%q, lang:%q}) MERGE (f)-[r:IMPORTS]->(m) SET r.imported_name=%q, r.alias=%q, r.line_number=%d`,
			filePath, liveGrantImportedModue, liveGrantLanguage, liveGrantImportedModue, marker, index+1),
		// A second import target unique to this file. The shared module above
		// gives the importers query many edges to page; these give the
		// package-imports query, which RETURNs DISTINCT logical modules, many
		// DISTINCT out-of-grant rows to fill its page with.
		fmt.Sprintf(`MERGE (m:Module {name:%q, lang:%q})`, liveGrantOwnModule(marker, index), liveGrantLanguage),
		fmt.Sprintf(`MATCH (f:File {path:%q}),(m:Module {name:%q, lang:%q}) MERGE (f)-[r:IMPORTS]->(m) SET r.imported_name=%q, r.alias=%q, r.line_number=%d`,
			filePath, liveGrantOwnModule(marker, index), liveGrantLanguage, liveGrantOwnModule(marker, index), marker, index+2),
	}
}

// liveGrantNestedDirectoryStatements adds one directory a level below the
// granted repository's own, holding one file.
//
// The projector reaches a depth-0 directory by Repository-[:CONTAINS]-> and a
// depth-N one by Directory-[:CONTAINS]->, which is why buildDirectoryCypher
// walks a variable-length REPO_CONTAINS|CONTAINS chain. Seeding only depth-0
// directories would let a rewrite that quietly stops walking that chain, or one
// that folds the file into the parent's count, pass anyway.
func liveGrantNestedDirectoryStatements() []string {
	repoPath := "/live/" + liveGrantGrantedMarker
	parent := repoPath + "/z-src-0"
	nested := parent + "/nested"
	filePath := nested + "/deep.py"
	return []string{
		fmt.Sprintf(`MERGE (d:Directory {path:%q}) SET d.name="nested", d.repo_id=%q`, nested, liveGrantRepo),
		fmt.Sprintf(`MATCH (p:Directory {path:%q}),(d:Directory {path:%q}) MERGE (p)-[:CONTAINS]->(d)`, parent, nested),
		fmt.Sprintf(`MERGE (f:File {path:%q}) SET f.name="deep.py", f.relative_path="z-src-0/nested/deep.py", f.language=%q, f.lang=%q, f.repo_id=%q`,
			filePath, liveGrantLanguage, liveGrantLanguage, liveGrantRepo),
		fmt.Sprintf(`MATCH (r:Repository {id:%q}),(f:File {path:%q}) MERGE (r)-[:REPO_CONTAINS]->(f)`, liveGrantRepo, filePath),
		fmt.Sprintf(`MATCH (d:Directory {path:%q}),(f:File {path:%q}) MERGE (d)-[:CONTAINS]->(f)`, nested, filePath),
	}
}

// liveGrantOtherFilePath is the path of one out-of-grant file, built the same
// way liveGrantRepositoryStatements builds it so the two cannot drift.
func liveGrantOtherFilePath(index int) string {
	repoPath := "/live/" + liveGrantOtherMarker
	return fmt.Sprintf("%s/a-src-%d/%s-%d.py", repoPath, index%(liveGrantOutOfGrantRows/2), liveGrantOtherMarker, index)
}

// liveGrantOwnModule names the import target only one file has. The
// out-of-grant repository owns six of them, so the DISTINCT page of
// packageImportRowsCypher is full before the granted repository's single module
// is reached.
func liveGrantOwnModule(marker string, index int) string {
	return fmt.Sprintf("%s_%s_%d", liveGrantImportedModue, marker, index)
}
