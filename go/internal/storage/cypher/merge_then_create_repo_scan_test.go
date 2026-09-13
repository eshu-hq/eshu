// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// mergeOpenPattern and createClausePattern locate the MERGE and CREATE clause
// keywords hasNodeMergeThenCreate scans for. Both are case-insensitive with
// \b word boundaries so e.g. "UNMERGED" or "RECREATE" never count as the
// keyword. createClausePattern requires CREATE be immediately followed by "("
// (modulo whitespace), so it matches a real CREATE clause opening a
// node/relationship pattern and never "ON CREATE SET" -- there CREATE is
// followed by "SET", not "(".
var (
	mergeOpenPattern    = regexp.MustCompile(`(?i)\bMERGE\s*\(`)
	createClausePattern = regexp.MustCompile(`(?i)\bCREATE\s*\(`)
)

// hasNodeMergeThenCreate flags the exact statement class orneryd/NornicDB#359
// silently drops: a statement that opens a node pattern with MERGE ( and
// later, in the SAME statement, adds a real CREATE clause. NornicDB's
// executeMultipleMerges splitter (splitMultipleMerges, pkg/cypher/merge.go on
// the NornicDB side) does not treat CREATE as a clause boundary, and its loop
// has no CREATE branch, so the CREATE clause is silently never executed. The
// canonical repro:
//
//	MERGE (s:Workload {id:$s}) MERGE (t:Workload {id:$t}) CREATE (s)-[:DEPENDS_ON]->(t)
//
// reports success with 1 node written and 0 relationships, where Neo4j
// reports 2 nodes and 1 relationship. See
// docs/public/reference/nornicdb-write-shape-pitfalls.md ("Pitfall: A Node
// MERGE Followed By CREATE In One Statement Silently Drops The CREATE") for
// the full writeup and the proven-safe alternative shapes.
//
// The value is split on ';' before scanning, so a real CREATE clause in a
// DIFFERENT statement never counts -- only a CREATE that textually follows a
// MERGE( within the same statement segment does. Each Go string literal is
// also scanned independently, so a CREATE in a separate string constant
// never counts either.
//
// This is a text scan over Go string literal VALUES extracted from the AST,
// not a Cypher parser. It has two known limits:
//   - It cannot see Cypher assembled at runtime from fragments (string
//     concatenation, fmt.Sprintf, a shared clause builder) -- only Cypher that
//     is a complete literal in source.
//   - It flags the textual shape, not proven-unsafe application behavior --
//     a clean scan means "no textually-visible MERGE-then-CREATE statement,"
//     not "this package's Cypher is provably safe."
func hasNodeMergeThenCreate(value string) bool {
	for _, statement := range strings.Split(value, ";") {
		mergeLoc := mergeOpenPattern.FindStringIndex(statement)
		if mergeLoc == nil {
			continue
		}
		if createClausePattern.MatchString(statement[mergeLoc[1]:]) {
			return true
		}
	}
	return false
}

// mergeThenCreateAllowlist names "path:line" locations (relative to the go
// module root, as produced by scanForNodeMergeThenCreate) that legitimately
// contain the MERGE-then-CREATE textual shape without hitting the NornicDB
// drop -- for example a statement gated behind a backend branch that never
// runs against NornicDB. Keep this list empty if possible; add an entry only
// with a comment proving why that specific statement is safe on every
// backend Eshu runs.
var mergeThenCreateAllowlist = map[string]bool{}

// scanForNodeMergeThenCreate walks every non-_test.go file under go/cmd and
// go/internal, extracts every string literal, and returns one "path:line"
// hit per match of hasNodeMergeThenCreate not covered by
// mergeThenCreateAllowlist. scannedFiles reports how many .go files were
// inspected, so the caller can tell a clean scan from a scan that silently
// walked nothing.
func scanForNodeMergeThenCreate(t *testing.T) (hits []string, scannedFiles int) {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	// thisFile is go/internal/storage/cypher/merge_then_create_repo_scan_test.go;
	// the go module root is three directories up.
	goModuleRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))

	fset := token.NewFileSet()
	for _, top := range []string{"cmd", "internal"} {
		root := filepath.Join(goModuleRoot, top)
		walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			scannedFiles++

			file, parseErr := parser.ParseFile(fset, path, nil, 0)
			if parseErr != nil {
				return fmt.Errorf("parse %s: %w", path, parseErr)
			}
			relPath, relErr := filepath.Rel(goModuleRoot, path)
			if relErr != nil {
				return relErr
			}

			ast.Inspect(file, func(n ast.Node) bool {
				lit, isLit := n.(*ast.BasicLit)
				if !isLit || lit.Kind != token.STRING {
					return true
				}
				value, unquoteErr := strconv.Unquote(lit.Value)
				if unquoteErr != nil {
					// Raw (backtick) string literals unquote fine via
					// strconv.Unquote too; skip anything that fails rather
					// than fail the whole scan on an unrelated parse edge
					// case.
					return true
				}
				if !hasNodeMergeThenCreate(value) {
					return true
				}
				site := fmt.Sprintf("%s:%d", relPath, fset.Position(lit.Pos()).Line)
				if mergeThenCreateAllowlist[site] {
					return true
				}
				hits = append(hits, site)
				return true
			})
			return nil
		})
		if walkErr != nil {
			t.Fatalf("filepath.Walk(%s): %v", root, walkErr)
		}
	}
	return hits, scannedFiles
}

// TestNoNodeMergeThenCreateCyphersAcrossRepo is the repo-wide static guard
// for orneryd/NornicDB#359. It parses every non-test .go file under go/cmd
// and go/internal, collects every string literal's value, and fails if any
// of them contain a node MERGE( pattern followed by a real CREATE( clause in
// the same statement -- the exact shape NornicDB silently drops. See
// hasNodeMergeThenCreate's doc comment for what this scan can and cannot
// see, and docs/public/reference/nornicdb-write-shape-pitfalls.md for the
// full writeup.
func TestNoNodeMergeThenCreateCyphersAcrossRepo(t *testing.T) {
	t.Parallel()

	hits, scannedFiles := scanForNodeMergeThenCreate(t)
	if scannedFiles == 0 {
		t.Fatal("scanned zero .go files under go/cmd and go/internal -- the scan itself is broken, not proof there is nothing to check")
	}
	if len(hits) > 0 {
		t.Fatalf(
			"found %d Cypher string(s) with a node MERGE( pattern followed by a CREATE( "+
				"clause in the same statement (orneryd/NornicDB#359: NornicDB silently drops "+
				"the CREATE clause, reporting success with fewer nodes/relationships than "+
				"requested). Never write a node MERGE followed by CREATE in one statement -- "+
				"use a relationship MERGE instead of CREATE, MATCH ... MATCH ... CREATE, a "+
				"comma-pattern CREATE, or two separate statements. Violations:\n  %s",
			len(hits), strings.Join(hits, "\n  "),
		)
	}
}

// TestHasNodeMergeThenCreate is the unit-level proof of hasNodeMergeThenCreate,
// including the two mandatory false-positive exclusions: "ON CREATE SET" (a
// normal MERGE action, not a CREATE clause) and a CREATE in a different
// statement.
func TestHasNodeMergeThenCreate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{
			name:  "canonical repro: node MERGE, node MERGE, relationship CREATE",
			value: `MERGE (s:Workload {id:$s}) MERGE (t:Workload {id:$t}) CREATE (s)-[:DEPENDS_ON]->(t)`,
			want:  true,
		},
		{
			name: "SET between the MERGEs and the CREATE does not change the result",
			value: `MERGE (s:Workload {id:$s})
MERGE (t:Workload {id:$t})
SET s.updated_at = $now
CREATE (s)-[:DEPENDS_ON]->(t)`,
			want: true,
		},
		{
			name: "a second CREATE after the first still counts",
			value: `MERGE (s:Workload {id:$s})
MERGE (t:Workload {id:$t})
CREATE (s)-[:DEPENDS_ON]->(t)
CREATE (t)-[:USED_BY]->(s)`,
			want: true,
		},
		{
			name:  "single MERGE followed directly by CREATE",
			value: `MERGE (n:Repository {id:$id}) CREATE (n)-[:HAS_TAG]->(:Tag {name:$tag})`,
			want:  true,
		},
		{
			name:  "ON CREATE SET is a normal MERGE action, not a CREATE clause",
			value: `MERGE (n:Repository {id:$id}) ON CREATE SET n.first_seen = $now`,
			want:  false,
		},
		{
			name:  "ON MATCH SET is a normal MERGE action too",
			value: `MERGE (n:Repository {id:$id}) ON MATCH SET n.last_seen = $now`,
			want:  false,
		},
		{
			name: "ON CREATE SET beside a real CREATE clause still flags the real clause",
			value: `MERGE (n:Repository {id:$id}) ON CREATE SET n.first_seen = $now
CREATE (n)-[:HAS_TAG]->(:Tag {name:$tag})`,
			want: true,
		},
		{
			name:  "CREATE in a different statement (after a semicolon) does not count",
			value: `MERGE (n:Repository {id:$id}) SET n.last_seen = $now; CREATE (:AuditEvent {id:$eventId})`,
			want:  false,
		},
		{
			name:  "CREATE before the MERGE, same statement, does not count",
			value: `CREATE (:AuditEvent {id:$eventId}) MERGE (n:Repository {id:$id})`,
			want:  false,
		},
		{
			name: "relationship MERGE instead of CREATE is the safe shape",
			value: `MERGE (s:Workload {id:$s})
MERGE (t:Workload {id:$t})
MERGE (s)-[:DEPENDS_ON]->(t)`,
			want: false,
		},
		{
			name:  "MATCH ... MATCH ... CREATE (no MERGE at all) is the safe shape",
			value: `MATCH (s:Workload {id:$s}) MATCH (t:Workload {id:$t}) CREATE (s)-[:DEPENDS_ON]->(t)`,
			want:  false,
		},
		{
			name:  "comma-pattern CREATE with no MERGE is the safe shape",
			value: `MATCH (s:Workload {id:$s}) MATCH (t:Workload {id:$t}) CREATE (s)-[:DEPENDS_ON]->(t), (t)-[:USED_BY]->(s)`,
			want:  false,
		},
		{
			name:  "no MERGE at all",
			value: `MATCH (n:Repository {id:$id}) RETURN n`,
			want:  false,
		},
		{
			name:  "MERGE with no CREATE anywhere",
			value: `MERGE (n:Repository {id:$id}) RETURN n`,
			want:  false,
		},
		{
			name:  "keyword-like substrings never match (UNMERGED / RECREATE)",
			value: `// UNMERGED (n) RECREATE (m)`,
			want:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hasNodeMergeThenCreate(tc.value); got != tc.want {
				t.Fatalf("hasNodeMergeThenCreate(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
