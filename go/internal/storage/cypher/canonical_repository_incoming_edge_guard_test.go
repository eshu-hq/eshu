// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// #7324 contract A, static half. Re-projecting a Repository keeps the node
// (MERGE + SET), so every edge another writer put on it survives, and no
// general re-arm signal exists to rebuild edges a delete dropped. The
// contract therefore forbids the deletes that would drop them:
//
//   - Rule 1: the only statement that may delete a Repository node is
//     canonicalNodeRepositoryPathCleanupCypher, the path-conflict retirement
//     the repository_path UNIQUE constraint requires. Identity is the
//     constant itself (whitespace-normalised), not a string allowlist.
//   - Rule 2: a relationship whose arrow-head endpoint is a Repository may be
//     deleted only by its own writer: the statement must scope the delete by
//     `rel.evidence_source =` (source-anchored, evidence-scoped retracts).
//
// This scans string constant expressions under go/internal and go/cmd
// (non-test), like TestRepositoryPropertyWritersStayInsideTheProjectorUpsert,
// and reuses its repositoryBindings (node patterns, `WHERE v:Repository`,
// `WITH v AS alias` chains). It does not see:
//
//   - Cypher assembled at run time (fmt.Sprintf, strings.Join, a variable):
//     same-package constant concatenations ARE folded, nothing else is;
//   - a label filled at run time (`fmt.Sprintf("(r:%s)", label)`), a
//     Repository reached with no label (`MATCH (n {id: $repo_id})`), or a
//     variable rebound through UNWIND, collect or a list comprehension;
//   - deletes through procedures (`CALL apoc.*`) or statements built outside
//     go/internal and go/cmd, in tests, or in testdata;
//   - an undirected relationship pattern is treated as incoming on both
//     sides (conservative), and rule 2 only checks that the literal carries
//     `rel.evidence_source =`, not that the value names the right writer.
//
// The per-write check in canonical_node_writer_repository_test.go
// (repositoryIDDetachDelete over the statements Write actually emits) is
// kept: it covers statements this literal scan cannot see.

var (
	cypherDeletePattern     = regexp.MustCompile(`(?i)\b(DETACH\s+)?DELETE\s+(\w+(?:\s*,\s*\w+)*)`)
	cypherRelationshipRegex = regexp.MustCompile(`(<)?-\[\s*(\w*)[^\]]*\]-(>)?`)
	cypherNodeHeadPattern   = regexp.MustCompile(`^\(\s*(\w*)\s*((?::\s*\w+\s*)*)`)
	repositoryLabelPattern  = regexp.MustCompile(`:\s*Repository\b`)
)

func normalizeCypherWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// cypherNodeAt parses the node pattern starting at text[0] ("(v:Label ...").
func cypherNodeAt(text string) (variable string, labels string, ok bool) {
	match := cypherNodeHeadPattern.FindStringSubmatch(strings.TrimLeft(text, " \t\r\n"))
	if match == nil {
		return "", "", false
	}
	return match[1], match[2], true
}

// cypherNodeEndingAt returns the node pattern text that closes just before
// end, walking back over balanced parentheses.
func cypherNodeEndingAt(text string, end int) (string, bool) {
	i := end - 1
	for i >= 0 && strings.ContainsRune(" \t\r\n", rune(text[i])) {
		i--
	}
	if i < 0 || text[i] != ')' {
		return "", false
	}
	depth := 0
	for j := i; j >= 0; j-- {
		switch text[j] {
		case ')':
			depth++
		case '(':
			depth--
			if depth == 0 {
				return text[j : i+1], true
			}
		}
	}
	return "", false
}

// repositoryIncomingRelationships returns the relationship variables whose
// arrow-head endpoint is a Repository (labelled, or a Repository-bound
// variable). Undirected patterns count on either side.
func repositoryIncomingRelationships(text string, bindings map[string]bool) map[string]bool {
	isRepository := func(node string, ok bool) bool {
		if !ok {
			return false
		}
		variable, labels, parsed := cypherNodeAt(node)
		return parsed && (repositoryLabelPattern.MatchString(labels) || (variable != "" && bindings[variable]))
	}
	incoming := map[string]bool{}
	for _, loc := range cypherRelationshipRegex.FindAllStringSubmatchIndex(text, -1) {
		if loc[4] == loc[5] {
			continue // anonymous relationship: nothing can DELETE it by name
		}
		variable := text[loc[4]:loc[5]]
		leftArrow, rightArrow := loc[2] >= 0, loc[6] >= 0
		right := text[loc[1]:]
		left, leftOK := cypherNodeEndingAt(text, loc[0])
		switch {
		case rightArrow && !leftArrow:
			if isRepository(right, true) {
				incoming[variable] = true
			}
		case leftArrow && !rightArrow:
			if isRepository(left, leftOK) {
				incoming[variable] = true
			}
		case !leftArrow && !rightArrow:
			if isRepository(right, true) || isRepository(left, leftOK) {
				incoming[variable] = true
			}
		}
	}
	return incoming
}

// repositoryDeleteViolations applies rules 1 and 2 to one Cypher literal.
func repositoryDeleteViolations(text string) []string {
	violations, _, _ := repositoryDeleteScan(text)
	return violations
}

// repositoryDeleteScan is repositoryDeleteViolations plus how many Repository
// node deletes and incoming-Repository relationship deletes it evaluated, so
// the tree scan can prove it reached the statements it guards.
func repositoryDeleteScan(text string) (violations []string, nodeDeletes, incomingDeletes int) {
	bindings := map[string]bool{}
	for _, binding := range repositoryBindings(text) {
		bindings[binding] = true
	}
	incoming := repositoryIncomingRelationships(text, bindings)
	isPathCleanup := normalizeCypherWhitespace(text) == normalizeCypherWhitespace(canonicalNodeRepositoryPathCleanupCypher)
	for _, match := range cypherDeletePattern.FindAllStringSubmatch(text, -1) {
		for _, variable := range strings.Split(match[2], ",") {
			variable = strings.TrimSpace(variable)
			if bindings[variable] {
				nodeDeletes++
				if !isPathCleanup {
					violations = append(violations, "rule 1: Repository node "+variable+" deleted outside canonicalNodeRepositoryPathCleanupCypher")
				}
			}
			if incoming[variable] {
				incomingDeletes++
				if !regexp.MustCompile(`\b` + regexp.QuoteMeta(variable) + `\.evidence_source\s*=`).MatchString(text) {
					violations = append(violations, "rule 2: relationship "+variable+" into a Repository deleted without "+variable+".evidence_source =")
				}
			}
		}
	}
	return violations, nodeDeletes, incomingDeletes
}

// foldCypherString evaluates a string constant expression: a literal, a
// parenthesised or `+`-concatenated expression, or an identifier naming a
// same-package constant, so `"...-[rel:" + EdgeTypes + "]->(:Repository)"`
// is scanned whole instead of as fragments.
func foldCypherString(expr ast.Expr, consts map[string]ast.Expr, depth int) (string, bool) {
	if depth > 16 {
		return "", false
	}
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		text, err := strconv.Unquote(e.Value)
		return text, err == nil
	case *ast.ParenExpr:
		return foldCypherString(e.X, consts, depth+1)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		left, ok := foldCypherString(e.X, consts, depth+1)
		if !ok {
			return "", false
		}
		right, ok := foldCypherString(e.Y, consts, depth+1)
		return left + right, ok
	case *ast.Ident:
		if value, ok := consts[e.Name]; ok {
			return foldCypherString(value, consts, depth+1)
		}
	}
	return "", false
}

// scanRepositoryCypherStrings calls visit with every string constant
// expression under go/internal and go/cmd (non-test, non-testdata) that
// mentions ":Repository", folding same-package constant concatenations.
func scanRepositoryCypherStrings(t *testing.T, goRoot string, visit func(rel, text string)) {
	t.Helper()
	filesByDir := map[string][]string{}
	for _, dir := range []string{"internal", "cmd"} {
		walkErr := filepath.WalkDir(filepath.Join(goRoot, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" || entry.Name() == "vendor" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
				filesByDir[filepath.Dir(path)] = append(filesByDir[filepath.Dir(path)], path)
			}
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", dir, walkErr)
		}
	}
	dirs := make([]string, 0, len(filesByDir))
	for dir := range filesByDir {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		files := make([]*ast.File, 0, len(filesByDir[dir]))
		consts := map[string]ast.Expr{}
		for _, path := range filesByDir[dir] {
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			files = append(files, file)
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.CONST {
					continue
				}
				for _, spec := range gen.Specs {
					value, ok := spec.(*ast.ValueSpec)
					if !ok || len(value.Names) != len(value.Values) {
						continue
					}
					for i, name := range value.Names {
						consts[name.Name] = value.Values[i]
					}
				}
			}
		}
		for i, file := range files {
			rel, _ := filepath.Rel(goRoot, filesByDir[dir][i])
			ast.Inspect(file, func(node ast.Node) bool {
				switch n := node.(type) {
				case *ast.BinaryExpr:
					if text, ok := foldCypherString(n, consts, 0); ok {
						if strings.Contains(text, ":Repository") {
							visit(rel, text)
						}
						return false // scanned whole; do not rescan the fragments
					}
				case *ast.BasicLit:
					if text, ok := foldCypherString(n, consts, 0); ok && strings.Contains(text, ":Repository") {
						visit(rel, text)
					}
				}
				return true
			})
		}
	}
}

func TestRepositoryIncomingEdgesAreDeletedOnlyByTheirOwners(t *testing.T) {
	t.Parallel()

	goRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve go module root: %v", err)
	}
	var violations []string
	literals, deletes, nodeDeletes, incomingDeletes := 0, 0, 0, 0
	scanRepositoryCypherStrings(t, goRoot, func(rel, text string) {
		literals++
		if cypherDeletePattern.MatchString(text) {
			deletes++
		}
		found, nodes, incoming := repositoryDeleteScan(text)
		nodeDeletes += nodes
		incomingDeletes += incoming
		for _, violation := range found {
			violations = append(violations, rel+": "+violation)
		}
	})
	t.Logf("scanned %d Repository Cypher strings: %d with DELETE, %d Repository node deletes, %d incoming-Repository relationship deletes",
		literals, deletes, nodeDeletes, incomingDeletes)
	// Floors are what the tree held when the guard landed (#7324: 227
	// strings, 22 with DELETE; the path cleanup is the one node delete; the
	// six evidence-scoped incoming retracts are repo_dependency DEPENDS_ON,
	// CORRELATES_DEPLOYABLE_UNIT, PINS_SUBMODULE, BUILT_FROM and the two
	// folded typed-relationship retracts over RepoDependencyRelationshipEdgeTypes),
	// so a scanner regression that stops seeing them fails instead of passing.
	if literals < 200 || deletes < 20 || nodeDeletes < 1 || incomingDeletes < 6 {
		t.Fatalf("scan reached %d strings, %d DELETE strings, %d node deletes, %d incoming deletes; the scan is not reaching the tree",
			literals, deletes, nodeDeletes, incomingDeletes)
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("contract A (#7324): Repository nodes and incoming Repository edges may be deleted only by "+
			"canonicalNodeRepositoryPathCleanupCypher and by the edge's own evidence_source-scoped writer:\n%s",
			strings.Join(violations, "\n"))
	}
}

// TestRepositoryDeleteViolationsDetectsSeededDeletes is the guard's
// seeded-violation RED/GREEN pair, fed through the same scanner.
func TestRepositoryDeleteViolationsDetectsSeededDeletes(t *testing.T) {
	t.Parallel()

	rule1 := func(v string) string {
		return "rule 1: Repository node " + v + " deleted outside canonicalNodeRepositoryPathCleanupCypher"
	}
	rule2 := func(v string) string {
		return "rule 2: relationship " + v + " into a Repository deleted without " + v + ".evidence_source ="
	}
	cases := []struct {
		name  string
		text  string
		wants []string
	}{
		{"by-id detach delete (#7285)", "MATCH (r:Repository {id: $repo_id}) DETACH DELETE r", []string{rule1("r")}},
		{"reflowed by-id delete", "MATCH (r:Repository {id: $repo_id})\n\tDETACH\n  DELETE r", []string{rule1("r")}},
		{"plain node delete", "MATCH (repo:Repository {id: $id}) DELETE repo", []string{rule1("repo")}},
		{"aliased node delete", "MATCH (r:Repository {id: $id}) WITH r AS gone DETACH DELETE gone", []string{rule1("gone")}},
		{"label predicate node delete", "MATCH (n) WHERE n:Repository AND n.path = $p DETACH DELETE n", []string{rule1("n")}},
		{"path cleanup with its guard removed", "MATCH (r:Repository {path: $path})\nDETACH DELETE r", []string{rule1("r")}},
		{"the path cleanup constant", canonicalNodeRepositoryPathCleanupCypher, nil},
		{"the path cleanup, reflowed", "MATCH (r:Repository {path: $path})  WHERE r.id <> $repo_id\n\n DETACH DELETE r", nil},
		{"incoming delete, no evidence scope", "MATCH (w:Workload)-[rel:DEPLOYS_FROM]->(r:Repository {id: $id}) DELETE rel", []string{rule2("rel")}},
		{"incoming delete from the left", "MATCH (r:Repository {id: $id})<-[rel:DEPENDS_ON]-(o:Repository) DELETE rel", []string{rule2("rel")}},
		{"anonymous Repository head", "MATCH (i:ContainerImage)-[rel:BUILT_FROM]->(:Repository) WHERE i.uid = $uid DELETE rel", []string{rule2("rel")}},
		{"bound head variable", "MATCH (r:Repository {id: $id}) MATCH (e:EvidenceArtifact)-[rel:EVIDENCES_REPOSITORY_RELATIONSHIP]->(r) DELETE rel", []string{rule2("rel")}},
		{"undirected delete", "MATCH (r:Repository {id: $id})-[rel:CORRELATES_DEPLOYABLE_UNIT]-(o) DELETE rel", []string{rule2("rel")}},
		{"multi-variable delete", "MATCH (i:WorkloadInstance)-[rel:DEPLOYMENT_SOURCE]->(r:Repository) DELETE rel, i", []string{rule2("rel")}},
		{"incoming delete scoped by evidence_source", "MATCH (parent:Repository)-[rel:PINS_SUBMODULE]->(:Repository)\nWHERE parent.id IN $repo_ids AND rel.evidence_source = $evidence_source\nDELETE rel", nil},
		{"scope and evidence scoped", "MATCH (o:Repository)-[rel:DEPENDS_ON]->(r:Repository) WHERE o.id = $id AND rel.scope_id = $s AND rel.evidence_source = 'resolver/cross-repo' DELETE rel", nil},
		{"outgoing delete", "MATCH (r:Repository {id: $id})-[rel:REPO_CONTAINS]->(f:File) WHERE f.generation_id <> $g DELETE rel", nil},
		{"non-Repository node delete", "MATCH (r:Repository {id: $id})-[:REPO_CONTAINS]->(f:File) DETACH DELETE f", nil},
		{"evidence scope on another variable", "MATCH (o:Repository)-[other:X]->(p) MATCH (o)-[rel:DEPENDS_ON]->(r:Repository) WHERE other.evidence_source = 'x' DELETE rel", []string{rule2("rel")}},
	}
	for _, tc := range cases {
		got := repositoryDeleteViolations(tc.text)
		if strings.Join(got, "|") != strings.Join(tc.wants, "|") {
			t.Errorf("%s: violations = %v, want %v", tc.name, got, tc.wants)
		}
	}
}
