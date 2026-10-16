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
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
)

// The census reads every production Go string a graph writer can execute and
// fails when a statement sets one of the twelve searched properties on an
// infra search label without the shared key clause appended by
// search.Statement. "Sets" means SET v.<field> = ..., SET v += / = <map>, or a
// MERGE/CREATE inline map that carries a searched field. REMOVE of a field
// and a label-only SET are not counted: they can only leave the key a superset
// of the node's current fields, which the reader's predicate re-check absorbs.
// A `%s` label template counts as an infra label, because the canonical
// entity templates are formatted with infra labels.
//
// A statement is keyed only when the clause came out of a search.Statement
// call. A clause pasted as literal text bypasses the dialect (NornicDB would
// receive it) and is reported. A package-level constant that only feeds a
// search.Statement call is a fragment and is judged through that call.

var (
	censusDirs        = []string{"internal/storage", "internal/reducer", "internal/projector", "internal/collector", "cmd"}
	censusClauseSplit = regexp.MustCompile(`(?i)\b(?:OPTIONAL\s+MATCH|DETACH\s+DELETE|ON\s+CREATE\s+SET|ON\s+MATCH\s+SET|ORDER\s+BY|MATCH|MERGE|CREATE|SET|WHERE|WITH|UNWIND|RETURN|DELETE|REMOVE|FOREACH|CALL|UNION|LIMIT|SKIP|YIELD)\b`)
	censusNodeBinding = regexp.MustCompile("\\(\\s*(\\w+)\\s*((?::\\s*(?:`?\\w+`?|%s)\\s*)+)")
	censusLabelToken  = regexp.MustCompile("(?:`?(\\w+)`?|(%s))")
	censusSetProp     = regexp.MustCompile(`^(\w+)\.(\w+)\s*=`)
	censusSetMap      = regexp.MustCompile(`^(\w+)\s*\+?=`)
	censusInlineMap   = regexp.MustCompile("\\(\\s*(\\w+)\\s*(?::\\s*(?:`?\\w+`?|%s)\\s*)+\\{([^{}]*)\\}")
	censusMapKey      = regexp.MustCompile(`(?:^|,)\s*(\w+)\s*:`)
)

// censusFold is a folded Go string expression and the node variables whose
// key clause a search.Statement call appended.
type censusFold struct {
	text  string
	keyed map[string]bool
}

// censusFinding is one statement that sets a searched field without the key.
type censusFinding struct{ file, problem, head string }

func (f censusFinding) String() string { return fmt.Sprintf("%s: %s: %q", f.file, f.problem, f.head) }

// censusInfraWrites returns the node variables the statement writes a
// searched field on, for variables bound to an infra search label or a %s
// template label.
func censusInfraWrites(text string) []string {
	infra := map[string]bool{}
	for _, m := range censusNodeBinding.FindAllStringSubmatch(text, -1) {
		for _, lm := range censusLabelToken.FindAllStringSubmatch(m[2], -1) {
			if lm[2] == "%s" || graph.IsInfraSearchKeyLabel(lm[1]) {
				infra[m[1]] = true
			}
		}
	}
	if len(infra) == 0 {
		return nil
	}
	searched := search.Fields()
	written := map[string]bool{}
	locs := censusClauseSplit.FindAllStringIndex(text, -1)
	for i, loc := range locs {
		word := strings.ToUpper(strings.Join(strings.Fields(text[loc[0]:loc[1]]), " "))
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		if word == "MERGE" || word == "CREATE" {
			// An inline map in MERGE or CREATE sets its properties when the
			// node is created; one in MATCH is only a lookup.
			for _, m := range censusInlineMap.FindAllStringSubmatch(text[loc[0]:end], -1) {
				if !infra[m[1]] {
					continue
				}
				for _, k := range censusMapKey.FindAllStringSubmatch(m[2], -1) {
					if slices.Contains(searched, k[1]) {
						written[m[1]] = true
					}
				}
			}
			continue
		}
		if !strings.HasSuffix(word, "SET") {
			continue
		}
		for _, item := range splitCensusItems(text[loc[1]:end]) {
			item = strings.TrimSpace(item)
			if m := censusSetProp.FindStringSubmatch(item); m != nil {
				if infra[m[1]] && slices.Contains(searched, m[2]) {
					written[m[1]] = true
				}
				continue
			}
			if m := censusSetMap.FindStringSubmatch(item); m != nil && infra[m[1]] {
				written[m[1]] = true
			}
		}
	}
	out := make([]string, 0, len(written))
	for v := range written {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// splitCensusItems splits a SET region on top-level commas.
func splitCensusItems(s string) []string {
	var parts []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
		case c == ',' && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// censusScanner holds the parsed tree and the package-level string
// declarations it folds identifiers against.
type censusScanner struct {
	files []censusFile
	decls map[string]map[string]ast.Expr // dir -> name -> value
}

type censusFile struct {
	rel, dir string
	fset     *token.FileSet
	file     *ast.File
}

func newCensusScanner(t *testing.T, root string, dirs []string) *censusScanner {
	t.Helper()
	s := &censusScanner{decls: map[string]map[string]ast.Expr{}}
	for _, dir := range dirs {
		err := filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
			rel, _ := filepath.Rel(root, path)
			if err == nil && info.IsDir() && censusSkipsDir(rel) {
				return filepath.SkipDir
			}
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			fset := token.NewFileSet()
			file, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				t.Fatalf("parse %s: %v", rel, perr)
			}
			cf := censusFile{rel: rel, dir: filepath.Dir(rel), fset: fset, file: file}
			s.files = append(s.files, cf)
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
					continue
				}
				for _, spec := range gen.Specs {
					vs := spec.(*ast.ValueSpec)
					if len(vs.Names) == 1 && len(vs.Values) == 1 {
						if s.decls[cf.dir] == nil {
							s.decls[cf.dir] = map[string]ast.Expr{}
						}
						s.decls[cf.dir][vs.Names[0].Name] = vs.Values[0]
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	return s
}

// censusSkipsDir skips cmd/*-gate and cmd/*-gates tools, the same rule as the
// graph package's index-key sweep: they seed throwaway stores and are not
// deployed write paths.
func censusSkipsDir(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	return len(parts) >= 2 && parts[0] == "cmd" && (strings.HasSuffix(parts[1], "-gate") || strings.HasSuffix(parts[1], "-gates"))
}

// isSearchStatementCall reports whether call is search.Statement(...).
func isSearchStatementCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Statement" && len(call.Args) == 4 && isIdentNamed(sel.X, "search")
}

func isIdentNamed(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// fold evaluates a string expression: literals, package-level identifiers,
// + chains, fmt.Sprintf on a folded format, and search.Statement, which it
// evaluates by calling the real search.Clause.
func (s *censusScanner) fold(dir string, n ast.Node, depth int) (censusFold, bool) {
	if depth > 16 {
		return censusFold{}, false
	}
	switch x := n.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return censusFold{}, false
		}
		v, err := strconv.Unquote(x.Value)
		return censusFold{text: v}, err == nil
	case *ast.Ident:
		if expr, ok := s.decls[dir][x.Name]; ok {
			return s.fold(dir, expr, depth+1)
		}
	case *ast.ParenExpr:
		return s.fold(dir, x.X, depth+1)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return censusFold{}, false
		}
		l, lok := s.fold(dir, x.X, depth+1)
		r, rok := s.fold(dir, x.Y, depth+1)
		if !lok || !rok {
			return censusFold{}, false
		}
		keyed := map[string]bool{}
		for k := range l.keyed {
			keyed[k] = true
		}
		for k := range r.keyed {
			keyed[k] = true
		}
		return censusFold{text: l.text + r.text, keyed: keyed}, true
	case *ast.SelectorExpr:
		// The key and flag property names, so a statement assembled from the
		// graph constants is read like one spelled out.
		if isIdentNamed(x.X, "graph") {
			switch x.Sel.Name {
			case "InfraSearchKeyProperty":
				return censusFold{text: graph.InfraSearchKeyProperty}, true
			case "InfraSearchOverflowProperty":
				return censusFold{text: graph.InfraSearchOverflowProperty}, true
			}
		}
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Sprintf" && isIdentNamed(sel.X, "fmt") && len(x.Args) > 0 {
			return s.fold(dir, x.Args[0], depth+1)
		}
		if isSearchStatementCall(x) {
			inner, ok := s.fold(dir, x.Args[3], depth+1)
			alias, aerr := strconv.Unquote(exprString(x.Args[2]))
			if !ok || aerr != nil {
				return censusFold{}, false
			}
			keyed := map[string]bool{alias: true}
			for k := range inner.keyed {
				keyed[k] = true
			}
			return censusFold{text: inner.text + "\n" + search.Clause(alias), keyed: keyed}, true
		}
	}
	return censusFold{}, false
}

func exprString(e ast.Expr) string {
	if lit, ok := e.(*ast.BasicLit); ok {
		return lit.Value
	}
	return ""
}

// fragments returns the package-level declarations that feed a
// search.Statement call, transitively, keyed by dir and name.
func (s *censusScanner) fragments() map[string]bool {
	out := map[string]bool{}
	var mark func(dir string, n ast.Node)
	mark = func(dir string, n ast.Node) {
		ast.Inspect(n, func(c ast.Node) bool {
			id, ok := c.(*ast.Ident)
			if !ok {
				return true
			}
			if expr, ok := s.decls[dir][id.Name]; ok && !out[dir+"\x00"+id.Name] {
				out[dir+"\x00"+id.Name] = true
				mark(dir, expr)
			}
			return true
		})
	}
	for _, f := range s.files {
		ast.Inspect(f.file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && isSearchStatementCall(call) {
				mark(f.dir, call.Args[3])
			}
			return true
		})
	}
	return out
}

// scan judges every foldable string in the tree. It returns the findings and
// the number of statements keyed through search.Statement.
func (s *censusScanner) scan() (findings []censusFinding, keyedWrites int) {
	frag := s.fragments()
	judge := func(f censusFile, n ast.Node) bool {
		if _, isIdent := n.(*ast.Ident); isIdent {
			return true
		}
		folded, ok := s.fold(f.dir, n, 0)
		if !ok {
			return true
		}
		for _, v := range censusInfraWrites(folded.text) {
			head := strings.Join(strings.Fields(folded.text), " ")
			if len(head) > 160 {
				head = head[:160]
			}
			where := fmt.Sprintf("%s:%d", f.rel, f.fset.Position(n.Pos()).Line)
			switch {
			case folded.keyed[v]:
				keyedWrites++
			case strings.Contains(folded.text, search.Clause(v)):
				findings = append(findings, censusFinding{where, "key clause pasted as text for " + v + "; append it with search.Statement so the dialect applies", head})
			default:
				findings = append(findings, censusFinding{where, "sets a searched field on " + v + " without search.Statement", head})
			}
		}
		return false
	}
	for _, f := range s.files {
		for _, decl := range f.file.Decls {
			if gen, ok := decl.(*ast.GenDecl); ok && (gen.Tok == token.CONST || gen.Tok == token.VAR) {
				for _, spec := range gen.Specs {
					vs := spec.(*ast.ValueSpec)
					if len(vs.Names) == 1 && frag[f.dir+"\x00"+vs.Names[0].Name] {
						continue
					}
					for _, v := range vs.Values {
						ast.Inspect(v, func(n ast.Node) bool { return judge(f, n) })
					}
				}
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool { return judge(f, n) })
		}
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].file < findings[j].file })
	return findings, keyedWrites
}

// censusMinKeyedWrites is the floor on keyed statements the production scan
// must find: five canonical entity templates, three Terraform state upserts,
// and the CloudResource and EC2 instance upserts. Fewer means the census
// stopped reading them and a clean result would be vacuous.
const censusMinKeyedWrites = 10

// TestInfraSearchKeyCensus fails when a production statement sets a searched
// field on an infra search label without the shared key clause.
func TestInfraSearchKeyCensus(t *testing.T) {
	t.Parallel()

	findings, keyed := newCensusScanner(t, filepath.Join("..", "..", ".."), censusDirs).scan()
	for _, f := range findings {
		t.Errorf("unkeyed infra search write: %s", f)
	}
	t.Logf("census found %d keyed infra writes", keyed)
	if keyed < censusMinKeyedWrites {
		t.Errorf("census found %d keyed infra writes, want at least %d", keyed, censusMinKeyedWrites)
	}
}
