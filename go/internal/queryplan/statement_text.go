// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// cypherScope carries the name tables one Cypher expression resolves
// against: the file's imports, single-assignment function variables
// visible at the literal, and the shared cross-file constant resolver.
type cypherScope struct {
	resolver *constResolver
	fileDir  string
	imports  map[string]string
	funcVars map[string]ast.Expr
}

// maxConstDepth bounds constant-chain resolution. Genuine initialization
// cycles do not compile, but discovery parses without type-checking, so a
// broken tree must degrade to dynamic, never hang the gate.
const maxConstDepth = 16

// templateText resolves the full static Cypher text: literals,
// concatenations of resolvable parts, same-package and imported-package
// constants (followed through chains), and single-assignment function
// variables. Anything else fails, marking the builder dynamic.
func (scope *cypherScope) templateText(expr ast.Expr, depth int) (string, bool) {
	if scope == nil || depth > maxConstDepth {
		return "", false
	}
	switch value := expr.(type) {
	case *ast.BasicLit:
		return staticString(expr)
	case *ast.BinaryExpr:
		if value.Op != token.ADD {
			return "", false
		}
		left, ok := scope.templateText(value.X, depth+1)
		if !ok {
			return "", false
		}
		right, ok := scope.templateText(value.Y, depth+1)
		if !ok {
			return "", false
		}
		return left + right, true
	case *ast.Ident, *ast.SelectorExpr:
		def, defScope, ok := scope.resolveName(expr, depth)
		if !ok {
			return "", false
		}
		return defScope.templateText(def, depth+1)
	default:
		return "", false
	}
}

// resolveName maps a variable or qualified constant reference to its
// defining expression and the scope that expression resolves in: function
// variables stay in the current scope, same-package constants resolve
// under their declaring file's imports, and imported-package constants
// under the target directory. Anything else fails.
func (scope *cypherScope) resolveName(expr ast.Expr, depth int) (ast.Expr, *cypherScope, bool) {
	if scope == nil || depth > maxConstDepth {
		return nil, nil, false
	}
	switch name := expr.(type) {
	case *ast.Ident:
		if sub, ok := scope.funcVars[name.Name]; ok {
			return sub, scope, true
		}
		if sub, ok := scope.resolver.packageConsts(scope.fileDir)[name.Name]; ok {
			subScope := &cypherScope{resolver: scope.resolver, fileDir: scope.fileDir, imports: sub.imports}
			return sub.expr, subScope, true
		}
		return nil, nil, false
	case *ast.SelectorExpr:
		pkg, ok := name.X.(*ast.Ident)
		if !ok {
			return nil, nil, false
		}
		importPath, ok := scope.imports[pkg.Name]
		if !ok {
			return nil, nil, false
		}
		dir := scope.resolver.importDir(importPath)
		if dir == "" {
			return nil, nil, false
		}
		entry, ok := scope.resolver.packageConsts(dir)[name.Sel.Name]
		if !ok {
			return nil, nil, false
		}
		subScope := &cypherScope{resolver: scope.resolver, fileDir: dir, imports: entry.imports}
		return entry.expr, subScope, true
	default:
		return nil, nil, false
	}
}

// textFragments collects the ordered static literal runs inside a composed
// Cypher expression: plain literals, string concatenations, Sprintf format
// strings split at their verbs, and resolvable names contributing their
// whole text as one run. Anything else (parameters, helper calls, user
// input) contributes no fragment, so a builder whose text is fully
// computed matches nothing by construction.
func (scope *cypherScope) textFragments(expr ast.Expr, depth int) []string {
	if scope == nil || depth > maxConstDepth {
		return nil
	}
	switch value := expr.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return nil
		}
		text, err := strconv.Unquote(value.Value)
		if err != nil {
			return nil
		}
		if normalized := normalizeStatementTemplate(text); normalized != "" {
			return []string{normalized}
		}
		return nil
	case *ast.BinaryExpr:
		if value.Op != token.ADD {
			return nil
		}
		return append(scope.textFragments(value.X, depth), scope.textFragments(value.Y, depth)...)
	case *ast.CallExpr:
		return sprintfFormatFragments(value)
	case *ast.Ident, *ast.SelectorExpr:
		def, defScope, ok := scope.resolveName(expr, depth)
		if !ok {
			return nil
		}
		return defScope.textFragments(def, depth+1)
	default:
		return nil
	}
}

// constEntry is one package-level string constant or variable: its value
// expression plus the imports of its declaring file, so chained
// cross-package references resolve in the right namespace.
type constEntry struct {
	expr    ast.Expr
	imports map[string]string
}

// constResolver collects package-level string constants per directory,
// cached so a directory parses once no matter how many of its files walk.
type constResolver struct {
	moduleRoot string
	modulePath string
	dirs       map[string]map[string]constEntry
}

func newConstResolver(sourceDir string) *constResolver {
	root, path := findModule(sourceDir)
	return &constResolver{moduleRoot: root, modulePath: path, dirs: make(map[string]map[string]constEntry)}
}

// findModule walks up from dir to the enclosing go.mod, returning its
// directory and module path. Outside any module both are empty and
// cross-package resolution stays off.
func findModule(dir string) (root, path string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", ""
	}
	for {
		// #nosec G304 -- path is the caller-provided directory joined with the literal go.mod, walking upward
		data, err := os.ReadFile(filepath.Join(abs, "go.mod"))
		if err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if name, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
					return abs, strings.TrimSpace(name)
				}
			}
			return abs, ""
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", ""
		}
		abs = parent
	}
}

// packageConsts returns name-to-entry for top-level const and var string
// declarations in dir. Unparseable files contribute nothing: dependents
// degrade to dynamic, and the manifest validator fails loudly on the
// resulting mismatch.
func (r *constResolver) packageConsts(dir string) map[string]constEntry {
	if cached, ok := r.dirs[dir]; ok {
		return cached
	}
	table := make(map[string]constEntry)
	r.dirs[dir] = table
	entries, err := os.ReadDir(dir)
	if err != nil {
		return table
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, filepath.Join(dir, name), nil, 0)
		if err != nil {
			continue
		}
		imports := fileImports(file)
		for _, decl := range file.Decls {
			decl, ok := decl.(*ast.GenDecl)
			if !ok || (decl.Tok != token.CONST && decl.Tok != token.VAR) {
				continue
			}
			for _, spec := range decl.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok || len(value.Names) != 1 || len(value.Values) != 1 {
					continue
				}
				table[value.Names[0].Name] = constEntry{expr: value.Values[0], imports: imports}
			}
		}
	}
	return table
}

// importDir maps an import path to its directory for module-local imports.
// Anything outside the module (stdlib, third party) has no parseable
// source here and resolves nowhere.
func (r *constResolver) importDir(importPath string) string {
	if r == nil || r.moduleRoot == "" || r.modulePath == "" {
		return ""
	}
	rel, ok := strings.CutPrefix(importPath, r.modulePath)
	if !ok {
		return ""
	}
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || strings.Contains(rel, "..") {
		return ""
	}
	return filepath.Join(r.moduleRoot, filepath.FromSlash(rel))
}

// fileImports maps each import's local package name to its path, skipping
// blank imports. Dot imports resolve nowhere: their names would match
// silently without a namespace.
func fileImports(file *ast.File) map[string]string {
	out := make(map[string]string)
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if imp.Name != nil {
			if imp.Name.Name == "_" || imp.Name.Name == "." {
				continue
			}
			out[imp.Name.Name] = path
			continue
		}
		if base := path[strings.LastIndex(path, "/")+1:]; base != "" {
			out[base] = path
		}
	}
	return out
}

// varAssign is one function-body string-variable assignment. pos is the
// token position: same-file positions share one base, so ordering by pos
// is sound without resolving file offsets.
type varAssign struct {
	name string
	expr ast.Expr
	pos  token.Pos
}

// collectAssignments gathers every :=, =, and single var declaration of
// one name to one value in the function body, including inside closures
// (which conservatively counts toward reassignment).
func collectAssignments(body *ast.BlockStmt) []varAssign {
	assigns := make([]varAssign, 0)
	ast.Inspect(body, func(node ast.Node) bool {
		switch stmt := node.(type) {
		case *ast.AssignStmt:
			if len(stmt.Lhs) != 1 || len(stmt.Rhs) != 1 {
				return true
			}
			if stmt.Tok != token.DEFINE && stmt.Tok != token.ASSIGN {
				return true
			}
			id, ok := stmt.Lhs[0].(*ast.Ident)
			if !ok || id.Name == "_" {
				return true
			}
			assigns = append(assigns, varAssign{name: id.Name, expr: stmt.Rhs[0], pos: stmt.Pos()})
		case *ast.ValueSpec:
			if len(stmt.Names) != 1 || len(stmt.Values) != 1 {
				return true
			}
			if stmt.Names[0].Name == "_" {
				return true
			}
			assigns = append(assigns, varAssign{name: stmt.Names[0].Name, expr: stmt.Values[0], pos: stmt.Pos()})
		}
		return true
	})
	return assigns
}

// visibleVars keeps names assigned exactly once function-wide at a position
// before use. Reassigned names resolve nowhere even when the use precedes
// the second write: the later value may flow through a closure, so any
// second assignment disqualifies.
func visibleVars(assigns []varAssign, use token.Pos) map[string]ast.Expr {
	counts := make(map[string]int)
	for _, assign := range assigns {
		counts[assign.name]++
	}
	out := make(map[string]ast.Expr)
	for _, assign := range assigns {
		if counts[assign.name] == 1 && assign.pos < use {
			out[assign.name] = assign.expr
		}
	}
	return out
}

// sprintfFormatFragments splits a fmt.Sprintf-style format string at its
// verbs, so "MATCH (n:%s) RETURN n" yields its two static runs. Only calls
// whose function is a Sprintf (qualified or not) with a literal format
// qualify; every other call shape yields no fragments.
func sprintfFormatFragments(call *ast.CallExpr) []string {
	name := ""
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		name = fn.Sel.Name
	case *ast.Ident:
		name = fn.Name
	}
	if name != "Sprintf" || len(call.Args) == 0 {
		return nil
	}
	format, ok := call.Args[0].(*ast.BasicLit)
	if !ok || format.Kind != token.STRING {
		return nil
	}
	text, err := strconv.Unquote(format.Value)
	if err != nil {
		return nil
	}
	fragments := make([]string, 0)
	current := strings.Builder{}
	flush := func() {
		if normalized := normalizeStatementTemplate(current.String()); normalized != "" {
			fragments = append(fragments, normalized)
		}
		current.Reset()
	}
	for i := 0; i < len(text); i++ {
		if text[i] != '%' || i+1 >= len(text) {
			current.WriteByte(text[i])
			continue
		}
		flush()
		i++
		// Skip flags, width, precision, and the verb itself: %[flags][width][.precision]verb.
		for i < len(text) && !isFormatVerb(text[i]) {
			i++
		}
	}
	flush()
	return fragments
}

// isFormatVerb reports whether the byte terminates a printf verb.
func isFormatVerb(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b == '%'
}

// staticExpressionText renders short static operation expressions: string
// literals and package-qualified names. Anything computed (calls,
// variables, concatenation) renders empty so the manifest marks the
// operation unknown instead of recording a guess.
func staticExpressionText(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.BasicLit:
		if value.Kind == token.STRING {
			if text, err := strconv.Unquote(value.Value); err == nil {
				return text
			}
		}
		return ""
	case *ast.SelectorExpr:
		ident, ok := value.X.(*ast.Ident)
		if !ok {
			return ""
		}
		return ident.Name + "." + value.Sel.Name
	default:
		return ""
	}
}

// staticString reports whether the expression is a static string: a plain
// literal or a concatenation of plain literals. Anything else (calls,
// variables, formatting) is dynamic.
func staticString(expr ast.Expr) (string, bool) {
	switch value := expr.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return "", false
		}
		text, err := strconv.Unquote(value.Value)
		if err != nil {
			return "", false
		}
		return text, true
	case *ast.BinaryExpr:
		if value.Op != token.ADD {
			return "", false
		}
		left, ok := staticString(value.X)
		if !ok {
			return "", false
		}
		right, ok := staticString(value.Y)
		if !ok {
			return "", false
		}
		return left + right, true
	default:
		return "", false
	}
}

// normalizeStatementTemplate collapses whitespace runs so template
// comparison ignores formatting. Case, comments, and literals are
// preserved: the template identifies the statement shape, not its data.
func normalizeStatementTemplate(cypher string) string {
	return strings.Join(strings.Fields(cypher), " ")
}
