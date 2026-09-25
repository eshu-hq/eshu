// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// sweepDirs are the production trees, relative to the go/ module root, whose
// non-test Go string literals can carry graph write Cypher.
var sweepDirs = []string{"internal/storage", "internal/reducer", "internal/projector", "internal/collector", "cmd"}

// sweepAllow lists literals the sweep does not require the guard to read. An
// entry matches by file suffix and a substring of the normalized literal, so a
// moved line does not break it, and it must still match something: a stale
// entry fails the test. Relationship-only writes and label templates need no
// entry; the sweep classifies them (see sweepWritesIndexedLabel).
// sweepMinCandidates is the floor on indexed-write literals the sweep must
// examine; the tree held 94 when it was written.
const sweepMinCandidates = 60

var sweepAllow = []struct{ file, contains, reason string }{
	{
		file:     "cmd/read-api-latency-gate/seed_graph.go",
		contains: "UNWIND $rows AS row CREATE (n:%s { %s })",
		reason:   "the read-API latency gate seeds a throwaway perf graph with generated short synthetic values; it is not a collector write path",
	},
	{
		file:     "internal/storage/cypher/tfstate_canonical_writer_retract.go",
		contains: "SET r:TerraformStateResource REMOVE r:TerraformResource",
		reason:   "a label migration: it moves an existing node between labels and writes no property value, so there is no new indexed value to measure",
	},
	{
		file:     "internal/storage/cypher/canonical_rationale_edges.go",
		contains: "MERGE (rationale:Rationale {uid: row.rationale_uid})",
		reason:   "a multi-part template: the UNWIND row binding lives in a sibling string part, so the sweep reads this fragment without row context; the executed whole statement analyzes clean and runtime measurement sees real rows",
	},
}

var (
	sweepWriteKeyword = regexp.MustCompile(`(?i)\b(?:MERGE|CREATE|SET)\b`)
	sweepDDL          = regexp.MustCompile(`(?i)\bCREATE\s+(?:CONSTRAINT|INDEX|FULLTEXT|TEXT\s+INDEX|RANGE\s+INDEX|POINT\s+INDEX|VECTOR\s+INDEX|LOOKUP\s+INDEX)\b`)
	sweepFormatVerb   = regexp.MustCompile(`%(?:\[\d+\])?[sdqv]`)
	sweepClauseSplit  = regexp.MustCompile(`(?i)\b(?:OPTIONAL\s+MATCH|DETACH\s+DELETE|ON\s+CREATE|ON\s+MATCH|ORDER\s+BY|MATCH|MERGE|CREATE|SET|WHERE|WITH|UNWIND|RETURN|DELETE|REMOVE|FOREACH|CALL|UNION|LIMIT|SKIP|YIELD)\b`)
	sweepNodeBinding  = regexp.MustCompile("\\(\\s*([^\\s:(){}\\[\\]]+)\\s*:\\s*`?(\\w+)`?")
	sweepLabelToken   = regexp.MustCompile("[^:]:\\s*`?(\\w+)`?")
	sweepNested       = regexp.MustCompile(`\{[^{}]*\}|\[[^\[\]]*\]`)
	sweepRelVar       = regexp.MustCompile(`\[\s*([^\s:\]{]+)\s*[:\]{]`)
	sweepSetTarget    = regexp.MustCompile(`(?:^|,)\s*([^\s.,=+:]+)\s*(?:\.\w+\s*\+?=|\+?=|:)`)
)

// sweepWritesIndexedLabel reports whether the literal writes a node of a
// schema-indexed label: a MERGE or CREATE pattern names one, or a SET targets
// a variable MATCHed as one. It reads the text with its own regexes rather
// than the analyzer's, so a shape the analyzer misses still counts. A
// relationship-only MERGE over MATCHed endpoints names no schema label in a
// write clause and SETs only relationship variables, so it does not count.
// Format verbs are filled with a schema label, the shape every %s label
// template takes.
func sweepWritesIndexedLabel(text string) bool {
	text = sweepFormatVerb.ReplaceAllString(text, "Function")
	if !sweepWriteKeyword.MatchString(text) || sweepDDL.MatchString(text) {
		return false
	}
	byLabel := SchemaIndexKeysByLabel()
	bound := map[string]string{} // variable -> schema label
	for _, m := range sweepNodeBinding.FindAllStringSubmatch(text, -1) {
		if _, ok := byLabel[m[2]]; ok {
			bound[m[1]] = m[2]
		}
	}
	rels := map[string]bool{}
	for _, m := range sweepRelVar.FindAllStringSubmatch(text, -1) {
		rels[m[1]] = true
	}

	locs := sweepClauseSplit.FindAllStringIndex(text, -1)
	for i, loc := range locs {
		word := strings.ToUpper(strings.Join(strings.Fields(text[loc[0]:loc[1]]), " "))
		if word != "MERGE" && word != "CREATE" && word != "SET" {
			continue
		}
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		region := text[loc[1]:end]
		if word == "SET" {
			for _, m := range sweepSetTarget.FindAllStringSubmatch(region, -1) {
				if _, node := bound[m[1]]; node && !rels[m[1]] {
					return true
				}
			}
			continue
		}
		for prev := ""; prev != region; {
			prev, region = region, sweepNested.ReplaceAllString(region, " ")
		}
		for _, w := range sweepLabelToken.FindAllStringSubmatch(region, -1) {
			if _, ok := byLabel[w[1]]; ok {
				return true
			}
		}
	}
	return false
}

// sweepFinding is one literal that writes an indexed label the guard does not
// fully read.
type sweepFinding struct {
	file, head, problem string
}

func (f sweepFinding) String() string { return fmt.Sprintf("%s: %s: %q", f.file, f.problem, f.head) }

// sweepIndexedWrites parses every non-test Go file under root/dirs and returns
// the string literals that write a schema-indexed label but are not fully
// read by the guard, plus the allowlist entries that matched nothing. A
// literal built from package-level string constants (Base + "\nMERGE ...") is
// folded first, so the sweep reads the whole statement the executor sees.
func sweepIndexedWrites(t *testing.T, root string, dirs []string) (findings []sweepFinding, stale []string, candidates int) {
	t.Helper()
	type parsedFile struct {
		rel  string
		dir  string
		fset *token.FileSet
		file *ast.File
	}
	var files []parsedFile
	for _, dir := range dirs {
		err := filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			fset := token.NewFileSet()
			file, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				t.Fatalf("parse %s: %v", rel, perr)
			}
			files = append(files, parsedFile{rel: rel, dir: filepath.Dir(rel), fset: fset, file: file})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}

	consts := map[string]map[string]string{} // package dir -> const name -> value
	resolver := func(dir string) func(string) (string, bool) {
		return func(name string) (string, bool) { v, ok := consts[dir][name]; return v, ok }
	}
	for pass := 0; pass < 6; pass++ {
		for _, pf := range files {
			for _, decl := range pf.file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.CONST {
					continue
				}
				for _, spec := range gen.Specs {
					vs := spec.(*ast.ValueSpec)
					if len(vs.Names) != 1 || len(vs.Values) != 1 {
						continue
					}
					if text, ok := foldStringLiteral(vs.Values[0], resolver(pf.dir)); ok {
						if consts[pf.dir] == nil {
							consts[pf.dir] = map[string]string{}
						}
						consts[pf.dir][vs.Names[0].Name] = text
					}
				}
			}
		}
	}

	used := make([]bool, len(sweepAllow))
	for _, pf := range files {
		ast.Inspect(pf.file, func(n ast.Node) bool {
			if _, isIdent := n.(*ast.Ident); isIdent {
				return true
			}
			text, ok := foldStringLiteral(n, resolver(pf.dir))
			if !ok {
				return true
			}
			if !sweepWritesIndexedLabel(text) {
				return false
			}
			candidates++
			plan := analyzeIndexWrites(sweepFormatVerb.ReplaceAllString(text, "Function"))
			problem := ""
			switch {
			case len(plan.vars) == 0:
				problem = "writes an indexed label but the guard binds no write"
			case len(plan.unanalyzed) > 0:
				problem = fmt.Sprintf("guard reports %+v", plan.unanalyzed)
			}
			if problem == "" {
				return false
			}
			normalized := strings.Join(strings.Fields(text), " ")
			allowed := false
			for i, a := range sweepAllow {
				if strings.HasSuffix(pf.rel, a.file) && strings.Contains(normalized, a.contains) {
					used[i], allowed = true, true
				}
			}
			if !allowed {
				head := normalized
				if len(head) > 160 {
					head = head[:160]
				}
				findings = append(findings, sweepFinding{file: fmt.Sprintf("%s:%d", pf.rel, pf.fset.Position(n.Pos()).Line), head: head, problem: problem})
			}
			return false
		})
	}
	for i, a := range sweepAllow {
		if !used[i] {
			stale = append(stale, a.file+" "+a.contains)
		}
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].file < findings[j].file })
	return findings, stale, candidates
}

// foldStringLiteral returns the value of a string literal, a package-level
// string constant resolve knows, or a + chain of them.
func foldStringLiteral(n ast.Node, resolve func(string) (string, bool)) (string, bool) {
	switch x := n.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(x.Value)
		return s, err == nil
	case *ast.Ident:
		return resolve(x.Name)
	case *ast.ParenExpr:
		return foldStringLiteral(x.X, resolve)
	case *ast.CallExpr:
		// strings.Join(labels, "|") splices a named label list into a
		// statement (the Rationale EXPLAINS writer). Read it as a %s slot, the
		// same placeholder the sweep already fills with a schema label, so the
		// statement is analyzed whole instead of as detached fragments. The
		// fold assumes the list holds labels: a Join that splices a clause or
		// property map would be read as a label here. Only a named list
		// (identifier or selector) folds; a Join over inline literals is
		// statement text itself, so it is left for the walk to descend into
		// and judge, never pruned.
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Join" && len(x.Args) > 0 {
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "strings" {
				switch x.Args[0].(type) {
				case *ast.Ident, *ast.SelectorExpr:
					return "%s", true
				}
			}
		}
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		l, lok := foldStringLiteral(x.X, resolve)
		r, rok := foldStringLiteral(x.Y, resolve)
		return l + r, lok && rok
	}
	return "", false
}

// TestProductionCypherLiteralsAreGuarded is the sweep behind #7058 review N1:
// every string literal in the production write paths that writes a
// schema-indexed label must produce a guard plan with no unanalyzed report,
// so a new writer shape the analyzer cannot read fails here instead of
// reviving the oversized-key failure on Neo4j with no signal.
func TestProductionCypherLiteralsAreGuarded(t *testing.T) {
	findings, stale, candidates := sweepIndexedWrites(t, filepath.Join("..", ".."), sweepDirs)
	// The reviewed tree has well over sweepMinCandidates indexed writers; a
	// far smaller count means the sweep stopped reading them (wrong root,
	// changed layout), which would make a clean result vacuous.
	if candidates < sweepMinCandidates {
		t.Fatalf("sweep examined %d indexed-write literals, want at least %d", candidates, sweepMinCandidates)
	}
	t.Logf("sweep examined %d indexed-write literals", candidates)
	for _, f := range findings {
		t.Errorf("unguarded indexed write: %s", f)
	}
	for _, s := range stale {
		t.Errorf("stale sweepAllow entry matches no literal: %s", s)
	}
}

// TestSweepFlagsSeededViolations proves the sweep can fail: a planted writer
// whose shape the guard cannot read is found, while recognized shapes,
// relationship-only MERGEs, and DDL are not.
func TestSweepFlagsSeededViolations(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		path := filepath.Join(root, "internal", "storage", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package storage\n\nconst q = "+strconv.Quote(body)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("nested.go", "UNWIND $rows AS row\nUNWIND row.params AS p\nMERGE (x:Parameter {name: p.name, path: row.file_path, function_line_number: 1})")
	write("accent.go", "UNWIND $rows AS row\nMERGE (módulo:Module {name: row.name})")
	write("template.go", "UNWIND $rows AS row\nMERGE (n:%s {uid: row.entity_id})\nSET n += row.props")
	write("recognized.go", "unwind $rows as row\nmerge (m:`Module` {name: row.name})")
	write("relationship.go", "UNWIND $rows AS row\nMATCH (p:Directory {path: row.parent})\nMATCH (d:Directory {path: row.path})\nMERGE (p)-[rel:CONTAINS]->(d)\nSET rel.generation_id = row.generation_id")
	write("ddl.go", "CREATE CONSTRAINT module_name IF NOT EXISTS FOR (m:Module) REQUIRE m.name IS UNIQUE")

	findings, _, _ := sweepIndexedWrites(t, root, []string{"internal/storage"})
	got := map[string]bool{}
	for _, f := range findings {
		got[filepath.Base(strings.SplitN(f.file, ":", 2)[0])] = true
	}
	for _, name := range []string{"nested.go", "accent.go"} {
		if !got[name] {
			t.Errorf("seeded violation %s not found; findings = %v", name, findings)
		}
	}
	for _, name := range []string{"template.go", "recognized.go", "relationship.go", "ddl.go"} {
		if got[name] {
			t.Errorf("%s flagged, want clean; findings = %v", name, findings)
		}
	}
}

// TestSweepReadsJoinedLabelTemplateWhole proves the sweep reads a statement
// assembled with strings.Join over a label list (the shape of the Rationale
// EXPLAINS writer) as one statement, the way the executor sees it. Read
// fragment-wise, the tail literal loses the UNWIND that binds row and a
// correctly guarded writer is reported as unresolved; a genuinely unreadable
// shape in the same construction must still be flagged.
func TestSweepReadsJoinedLabelTemplateWhole(t *testing.T) {
	root := t.TempDir()
	write := func(name, decl string) {
		t.Helper()
		path := filepath.Join(root, "internal", "storage", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		src := "package storage\n\nimport \"strings\"\n\nvar labels = []string{\"Function\", \"Class\"}\n\n" + decl + "\n"
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("joined_ok.go", "var q = `UNWIND $rows AS row\nMATCH (t:` + strings.Join(labels, \"|\") + ` {uid: row.target})\nMERGE (m:Module {name: row.name})\nMERGE (m)-[:EXPLAINS]->(t)`")
	write("joined_bad.go", "var q = `UNWIND $rows AS row\nUNWIND row.params AS p\nMATCH (t:` + strings.Join(labels, \"|\") + ` {uid: row.target})\nMERGE (x:Parameter {name: p.name, path: row.file_path, function_line_number: 1})`")

	write("joined_literals_bad.go", "var q = strings.Join([]string{\"UNWIND $rows AS row\", \"UNWIND row.params AS p\", \"MERGE (x:Parameter {name: p.name, path: row.file_path, function_line_number: 1})\"}, \"\\n\")")

	findings, _, _ := sweepIndexedWrites(t, root, []string{"internal/storage"})
	got := map[string]bool{}
	for _, f := range findings {
		got[filepath.Base(strings.SplitN(f.file, ":", 2)[0])] = true
	}
	if !got["joined_literals_bad.go"] {
		t.Errorf("joined_literals_bad.go not flagged: a Join of statement literals must still be read; findings = %v", findings)
	}
	if got["joined_ok.go"] {
		t.Errorf("joined_ok.go flagged, want clean; findings = %v", findings)
	}
	if !got["joined_bad.go"] {
		t.Errorf("joined_bad.go not flagged; findings = %v", findings)
	}
}
