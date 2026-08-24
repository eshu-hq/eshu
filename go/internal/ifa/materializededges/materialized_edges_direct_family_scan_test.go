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

// relationshipMergePattern matches a Cypher clause that CREATES a relationship:
// a MERGE or CREATE whose pattern continues into a `-[` or `<-[` relationship
// bracket.
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
var relationshipMergePattern = regexp.MustCompile(`(?i)\b(?:MERGE|CREATE)\s*\([^()]*\)\s*<?-\[`)

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
func relationshipMergeLine(value string) (string, bool) {
	if !relationshipMergePattern.MatchString(value) {
		return "", false
	}
	for _, line := range strings.Split(value, "\n") {
		if relationshipMergePattern.MatchString(line) {
			return strings.TrimSpace(line), true
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
