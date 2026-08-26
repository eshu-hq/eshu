// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
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

// relationshipMergeKeywordPattern finds where a Cypher clause that may CREATE a
// relationship begins: a MERGE or CREATE opening a node pattern. Whether that
// pattern continues into a `-[` or `<-[` relationship bracket is decided by
// mergesRelationship, which walks the pattern's parentheses instead of matching
// them.
//
// MATCH is deliberately excluded. Every family's retract template matches an
// existing relationship before deleting it (`MATCH (:Function)-[rel:TAINT_FLOWS_TO]->
// (:Function) ... DELETE rel`), and a writer's upsert template routinely
// MATCHes both endpoint nodes before merging the edge between them. Counting
// MATCH would classify every retract port, and several genuinely node-only
// writers, as edge writers — the loud-but-wrong direction, which buries the
// silent one.
//
// The relationship type itself is deliberately not captured. Four production
// templates interpolate it (`MERGE (sg)-[rel:%s]->(rule)`), so requiring a
// literal type would drop exactly the families whose type is chosen at
// runtime.
var relationshipMergeKeywordPattern = regexp.MustCompile(`(?i)\b(?:MERGE|CREATE)\s*\(`)

// mergesRelationship reports whether value contains a MERGE or CREATE whose
// node pattern continues into a relationship bracket.
//
// The node pattern is walked to its BALANCED closing parenthesis rather than
// matched with a `[^()]*` run. That run required the pattern to hold no
// parentheses of its own, so `MERGE (n:Label {id: coalesce($a, $b)})-[r:T]->(m)`
// — an ordinary merge keyed on a coalesced identity — ended its match at
// `coalesce(` and read as node-only. A port whose only write site is such a
// template would be classified node-only, its family would never enter the
// enumeration, and no ledger row would be missing for it: silent, and the
// direction this guard exists to prevent. An unbalanced pattern resolves to no
// match, as before.
//
// Adjacency after the closing parenthesis is kept exactly as the old pattern
// had it — whitespace, then an optional `<`, then `-[` — so a `-[` appearing
// anywhere later in the template still does not make an unrelated clause read
// as a relationship merge.
//
// stripCypherComments blanks comments before any of that runs, because all
// three readers misread one: the keyword pattern matches a MERGE written inside
// a comment, an unbalanced `)` in a comment moves the walk's depth, and a
// comment between the node pattern and its `-[` breaks the adjacency.
func mergesRelationship(value string) bool {
	value = stripCypherComments(value)
	for offset := 0; offset < len(value); {
		loc := relationshipMergeKeywordPattern.FindStringIndex(value[offset:])
		if loc == nil {
			return false
		}
		// The match ends on the "(" that opens the node pattern.
		open := offset + loc[1] - 1
		if closed, balanced := closingParen(value, open); balanced {
			rest := strings.TrimLeft(value[closed+1:], " \t\r\n")
			rest = strings.TrimPrefix(rest, "<")
			if strings.HasPrefix(rest, "-[") {
				return true
			}
		}
		offset = open + 1
	}
	return false
}

// closingParen returns the index of the parenthesis closing the one at open,
// and whether value holds one at all.
//
// Parentheses inside a quoted property value do not count. A template like
// `MERGE (n:Repo {path: "a/b)c"})-[r:CONTAINS]->(m)` closes its node pattern at
// the `)` inside the string literal if the walk is quote-unaware, the trailing
// `-[r:CONTAINS]->` is never seen, and the port is classified node-only -- the
// silent false-green this scan exists to prevent, one level below the
// function-call case (`coalesce($a, $b)`) the balanced walk already handles.
// No production template uses that shape today; the guard is for the day one
// does. A backslash escapes the next byte so an escaped quote does not end the
// string.
//
// Comments are not this function's problem: mergesRelationship blanks them
// first. Not free — a `'` in `// the repo's id` used to open a quoted region
// here and swallow the rest of the template, a false-green the quote tracking
// below introduced. TestCypherCommentsDoNotHideARelationshipMerge holds it.
//
// Limit, measured rather than assumed: an UNTERMINATED quote makes the walk run
// to the end without ever closing the node pattern, so the port reads node-only
// — the false-GREEN direction. Ten adversarial inputs were tried (unterminated
// single and double quotes, a trailing backslash, a backtick label, mixed quote
// types, an escaped backslash before a closing quote, and the empty and
// lone-paren cases); none panicked or looped, and only
// the two unterminated-quote inputs misclassify. An unterminated quote is not
// valid Cypher, so no template that compiles can reach it — but that is the
// reason it is safe, and it is worth stating rather than implying the walk
// handles every shape.
func closingParen(value string, open int) (int, bool) {
	depth := 0
	var quote byte
	for i := open; i < len(value); i++ {
		c := value[i]
		if quote != 0 {
			switch c {
			case '\\':
				i++
			case quote:
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// cypherPortClassification is one reducer graph-write port's verdict, derived
// from the Cypher the port reaches rather than from its name.
type cypherPortClassification struct {
	// Port is the reducer interface method name.
	Port string
	// Impl is the repo-relative file declaring the cypher-package method that
	// implements it.
	Impl string
	// WritesEdges is true when the port reaches at least one Cypher template
	// containing a relationship MERGE or CREATE.
	WritesEdges bool
	// Evidence is the first relationship-merging line the port reaches, so a
	// failure report can name the write site instead of asserting one exists.
	Evidence string
}

// cypherPackageSource is the parsed go/internal/storage/cypher package: the
// string constants it declares and the identifier references of every function
// and method body, which together let a port be followed to the Cypher it
// reaches.
type cypherPackageSource struct {
	// stringValues maps a package-level const or var name to its string value.
	stringValues map[string]string
	// bodyRefs maps a function key to the identifiers its body references.
	bodyRefs map[string][]string
	// bodyLiterals maps a function key to string literals written directly in
	// its body, including function-local consts.
	bodyLiterals map[string][]string
	// keysByName maps a bare function or method name to every function key
	// declaring it, so a selector call (`w.writeBatched(...)`) can be followed
	// without resolving the receiver's type.
	keysByName map[string][]string
	// fileByKey maps a function key to the repo-relative file declaring it.
	fileByKey map[string]string
}

// scanReducerInterfacePorts returns every method name the reducer declares on
// an interface, as a set.
//
// Interface methods only. A concrete method or a local helper that happens to
// share a name is not a port the reducer depends on, and counting one would
// put a port in the classification tables that the reducer cannot reach.
func scanReducerInterfacePorts(t *testing.T, reducerDir string) map[string]struct{} {
	t.Helper()

	out := map[string]struct{}{}
	forEachGoFile(t, reducerDir, func(_ string, file *ast.File) {
		ast.Inspect(file, func(n ast.Node) bool {
			iface, ok := n.(*ast.InterfaceType)
			if !ok || iface.Methods == nil {
				return true
			}
			for _, field := range iface.Methods.List {
				if _, isFunc := field.Type.(*ast.FuncType); !isFunc {
					continue
				}
				for _, ident := range field.Names {
					out[ident.Name] = struct{}{}
				}
			}
			return true
		})
	})
	if len(out) == 0 {
		t.Fatalf("scanned %s and found no interface methods; the scan went vacuous", reducerDir)
	}
	return out
}

// parseCypherPackage reads go/internal/storage/cypher into the shape
// classifyCypherPorts follows.
func parseCypherPackage(t *testing.T, cypherDir string) *cypherPackageSource {
	t.Helper()

	src := &cypherPackageSource{
		stringValues: map[string]string{},
		bodyRefs:     map[string][]string{},
		bodyLiterals: map[string][]string{},
		keysByName:   map[string][]string{},
		fileByKey:    map[string]string{},
	}

	forEachGoFile(t, cypherDir, func(rel string, file *ast.File) {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				collectStringValues(d, src.stringValues)
			case *ast.FuncDecl:
				if d.Body == nil {
					continue
				}
				key := funcDeclKey(d)
				src.fileByKey[key] = rel
				src.keysByName[d.Name.Name] = append(src.keysByName[d.Name.Name], key)
				refs, lits := collectBodyRefs(d.Body)
				src.bodyRefs[key] = refs
				src.bodyLiterals[key] = lits
			}
		}
	})

	if len(src.stringValues) == 0 || len(src.bodyRefs) == 0 {
		t.Fatalf("scanned %s and found no string constants or no function bodies; the scan went vacuous", cypherDir)
	}
	return src
}

// classifyCypherPorts returns, for every reducer interface port implemented in
// the cypher package, whether that port reaches a relationship MERGE.
//
// "Reaches" is followed transitively through package-local calls, because the
// write site is routinely two hops from the port: WriteSemanticEntities calls
// semanticEntityPlans, which names the upsert templates that carry the
// CONTAINS merge. A one-hop scan classifies that port as node-only, which is
// precisely the miss this guard exists to prevent.
func classifyCypherPorts(src *cypherPackageSource, ports map[string]struct{}) []cypherPortClassification {
	var out []cypherPortClassification
	for name := range ports {
		keys, ok := src.keysByName[name]
		if !ok {
			continue
		}
		sort.Strings(keys)
		row := cypherPortClassification{Port: name, Impl: src.fileByKey[keys[0]]}
		for _, key := range keys {
			if evidence, found := src.reachesRelationshipMerge(key); found {
				row.WritesEdges = true
				row.Evidence = evidence
				break
			}
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

// reachesRelationshipMerge walks the call graph from key and returns the first
// relationship-merging Cypher line it finds.
func (s *cypherPackageSource) reachesRelationshipMerge(key string) (string, bool) {
	seen := map[string]struct{}{key: {}}
	queue := []string{key}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, lit := range s.bodyLiterals[current] {
			if line, ok := relationshipMergeLine(lit); ok {
				return line, true
			}
		}
		for _, ref := range s.bodyRefs[current] {
			if value, isString := s.stringValues[ref]; isString {
				if line, ok := relationshipMergeLine(value); ok {
					return line, true
				}
			}
			for _, next := range s.keysByName[ref] {
				if _, visited := seen[next]; visited {
					continue
				}
				seen[next] = struct{}{}
				queue = append(queue, next)
			}
		}
	}
	return "", false
}

// relationshipMergeLine returns the first line of value that merges a
// relationship.
//
// The search runs over comment-blanked text and the evidence quotes the RAW
// line at the same position: searching raw lines would read a MERGE inside a
// block comment as the write site, and quoting the stripped line would hand an
// operator a row of spaces. The blanking is in place, so the splits align.
func relationshipMergeLine(value string) (string, bool) {
	scannable := stripCypherComments(value)
	if !mergesRelationship(scannable) {
		return "", false
	}
	raw := strings.Split(value, "\n")
	for i, line := range strings.Split(scannable, "\n") {
		if mergesRelationship(line) {
			return strings.TrimSpace(raw[i]), true
		}
	}
	return strings.TrimSpace(value), true
}

// forEachGoFile parses every non-test .go file directly in dir and calls fn
// with the file's base name and parsed AST.
//
// AST rather than a text scan for the reason
// TestPropertyKeyedRelationshipMergesMatchKnownAllowList already records for
// its own scan of go/internal/storage/cypher: a doc comment quoting an example
// port signature or an example MERGE is never part of the expression tree, so
// it cannot be mistaken for a declaration. A regex over raw source would need
// exclusion logic to tell the two apart, and that logic is where such a scan
// goes quietly wrong.
func forEachGoFile(t *testing.T, dir string, fn func(name string, file *ast.File)) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", filepath.Join(dir, name), err)
		}
		fn(name, file)
	}
}

// collectStringValues records every package-level const or var whose value
// resolves to static string text into out.
func collectStringValues(decl *ast.GenDecl, out map[string]string) {
	if decl.Tok != token.CONST && decl.Tok != token.VAR {
		return
	}
	for _, spec := range decl.Specs {
		value, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for i, name := range value.Names {
			if i >= len(value.Values) {
				continue
			}
			if literal, ok := staticStringValue(value.Values[i]); ok {
				out[name.Name] = literal
			}
		}
	}
}

// staticStringValue renders an expression's static string text, if it has any.
//
// A composite literal is walked rather than skipped. A Cypher template is as
// executable held in a `map[string]string{...}`, a slice, or a struct field as
// it is in a bare const, and the templates in this package are routinely
// grouped that way. Resolving only literals and `+` concatenations left a
// relationship MERGE reachable from a reducer port and invisible to the scan —
// a hole in the guard whose whole claim is that EVERY port reaching a MERGE is
// a declared family. Elements are joined with newlines so relationshipMergeLine
// still reports one real source line as evidence rather than a run-on.
func staticStringValue(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		unquoted, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}
		return unquoted, true
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		left, leftOK := staticStringValue(e.X)
		right, rightOK := staticStringValue(e.Y)
		if !leftOK && !rightOK {
			return "", false
		}
		return left + right, true
	case *ast.CompositeLit:
		var parts []string
		for _, elt := range e.Elts {
			// A keyed element carries text on either side: a map key can be the
			// template and a struct field name cannot, and staticStringValue
			// rejects the identifier either way, so both are simply offered.
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if key, ok := staticStringValue(kv.Key); ok {
					parts = append(parts, key)
				}
				elt = kv.Value
			}
			if value, ok := staticStringValue(elt); ok {
				parts = append(parts, value)
			}
		}
		if len(parts) == 0 {
			return "", false
		}
		return strings.Join(parts, "\n"), true
	}
	return "", false
}

// collectBodyRefs returns the identifiers a function body references and the
// string literals written directly inside it.
func collectBodyRefs(body *ast.BlockStmt) ([]string, []string) {
	refSeen := map[string]struct{}{}
	var refs []string
	var literals []string

	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.Ident:
			if _, dup := refSeen[node.Name]; !dup {
				refSeen[node.Name] = struct{}{}
				refs = append(refs, node.Name)
			}
		case *ast.SelectorExpr:
			if _, dup := refSeen[node.Sel.Name]; !dup {
				refSeen[node.Sel.Name] = struct{}{}
				refs = append(refs, node.Sel.Name)
			}
		case *ast.BasicLit:
			if node.Kind != token.STRING {
				return true
			}
			if unquoted, err := strconv.Unquote(node.Value); err == nil {
				literals = append(literals, unquoted)
			}
		}
		return true
	})
	sort.Strings(refs)
	return refs, literals
}

// funcDeclKey renders a function declaration as "Receiver.Method" or "Func".
func funcDeclKey(decl *ast.FuncDecl) string {
	if decl.Recv == nil || len(decl.Recv.List) == 0 {
		return decl.Name.Name
	}
	return receiverTypeName(decl.Recv.List[0].Type) + "." + decl.Name.Name
}

// receiverTypeName renders a receiver type expression as its bare type name.
func receiverTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return receiverTypeName(e.X)
	case *ast.Ident:
		return e.Name
	case *ast.IndexExpr:
		return receiverTypeName(e.X)
	}
	return "?"
}
