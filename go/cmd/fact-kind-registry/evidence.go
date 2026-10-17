// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// kindSymbols maps source identifiers to the fact kinds they name, so
// the evidence scan can recognize consumption written against fact-kind
// constants or typed decode seams instead of wire string literals.
type kindSymbols struct {
	identToKind  map[string]string
	decodeToKind map[string]string
}

// loadKindSymbols scans the facts tree and the factschema SDK for
// string constants whose values are fact kinds and for Decode<Kind>
// seams. It is a go/parser scan like discovery: nothing executes.
//
// Learning runs in two phases so the result is independent of file
// order: constants are collected across every root first, then decode
// seams resolve against the complete constant set. A Decode seam
// whose *FactKind* constant lives in a later-parsed file maps exactly
// as one whose constant came first.
func loadKindSymbols(repoRoot string, knownKinds map[string]bool) kindSymbols {
	symbols := kindSymbols{identToKind: map[string]string{}, decodeToKind: map[string]string{}}
	roots := []string{
		filepath.Join(repoRoot, "go", "internal", "facts"),
		filepath.Join(repoRoot, "sdk", "go", "factschema"),
		// The reducer's shared decode seams wrap the SDK decoders;
		// learning their Decode<Kind> mapping lets the evidence scan
		// attribute consumption to the domain handlers that call them.
		filepath.Join(repoRoot, "go", "internal", "reducer", "schemadecode"),
	}
	var files []*ast.File
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil //nolint:nilerr // a missing tree yields no symbols; validation reports gaps.
			}
			if !fileBuilds(filepath.Dir(path), entry.Name()) {
				return nil
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return nil
			}
			files = append(files, file)
			return nil
		})
	}
	for _, file := range files {
		collectConstSymbols(file, knownKinds, symbols.identToKind)
	}
	for _, file := range files {
		collectDecodeSymbols(file, symbols)
	}
	collectReducerAliases(repoRoot, knownKinds, symbols)
	return symbols
}

// collectReducerAliases learns two reducer-local vocabularies so
// handler packages that use them get consumption credit: string
// constants under the reducer tree whose values are fact kinds (the
// factload kind constants), and decode-seam alias vars of the form
// decodeX = schemadecode.DecodeX that root handlers call directly.
//
// Aliases resolve to a fixpoint so chained aliases (decodeA naming
// decodeB, which names a seam) resolve regardless of declaration
// order. Each round fills only unmapped names from the current maps,
// so the loop terminates once a full round adds nothing.
func collectReducerAliases(repoRoot string, knownKinds map[string]bool, symbols kindSymbols) {
	root := filepath.Join(repoRoot, "go", "internal", "reducer")
	var files []*ast.File
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil //nolint:nilerr // a missing tree yields no symbols; validation reports gaps.
		}
		if !fileBuilds(filepath.Dir(path), entry.Name()) {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil
		}
		files = append(files, file)
		return nil
	})
	for _, file := range files {
		collectConstSymbols(file, knownKinds, symbols.identToKind)
	}
	type alias struct {
		name   string
		target string
	}
	var aliases []alias
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range valueSpec.Names {
					if i >= len(valueSpec.Values) {
						continue
					}
					target, ok := aliasTarget(valueSpec.Values[i])
					if !ok {
						continue
					}
					aliases = append(aliases, alias{name: name.Name, target: target})
				}
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, a := range aliases {
			if _, ok := symbols.decodeToKind[a.name]; ok {
				continue
			}
			if _, ok := symbols.identToKind[a.name]; ok {
				continue
			}
			if kind, ok := symbols.decodeToKind[a.target]; ok {
				symbols.decodeToKind[a.name] = kind
				changed = true
				continue
			}
			if kind, ok := symbols.identToKind[a.target]; ok {
				symbols.identToKind[a.name] = kind
				changed = true
			}
		}
	}
}

// aliasTarget returns the vocabulary name one alias var points at:
// the selector of decodeX = schemadecode.DecodeX, or the identifier
// of a chained decodeA = decodeB alias. Anything else is not an alias.
func aliasTarget(expr ast.Expr) (string, bool) {
	switch expr := expr.(type) {
	case *ast.SelectorExpr:
		return expr.Sel.Name, true
	case *ast.Ident:
		return expr.Name, true
	default:
		return "", false
	}
}

func collectConstSymbols(file *ast.File, knownKinds map[string]bool, out map[string]string) {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
			continue
		}
		for _, spec := range gen.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range valueSpec.Names {
				if i >= len(valueSpec.Values) {
					continue
				}
				lit, ok := valueSpec.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(lit.Value)
				// Only FactKind-named constants identify fact kinds.
				// Other string constants reuse kind spellings for
				// unrelated purposes (for example an AWS resource-type
				// constant whose value equals the aws_iam_instance_profile
				// kind) and must never become kind symbols.
				if err == nil && knownKinds[value] && strings.Contains(name.Name, "FactKind") {
					out[name.Name] = value
				}
			}
		}
	}
}

// collectDecodeSymbols maps Decode<Name> functions to kinds by finding
// a known kind constant referenced in the function body or signature
// vicinity (the factschema decode seams reference their FactKind*
// constant). Resolution happens after const collection for the file.
func collectDecodeSymbols(file *ast.File, symbols kindSymbols) {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || !strings.HasPrefix(fn.Name.Name, "Decode") || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.Ident:
				if kind, ok := symbols.identToKind[node.Name]; ok {
					symbols.decodeToKind[fn.Name.Name] = kind
					return false
				}
			case *ast.SelectorExpr:
				if kind, ok := symbols.identToKind[node.Sel.Name]; ok {
					symbols.decodeToKind[fn.Name.Name] = kind
					return false
				}
			}
			return true
		})
	}
}

// packageSymbols returns the top-level symbol names declared in one
// package directory (functions, methods, types, vars, consts).
func packageSymbols(dirAbs string) map[string]bool {
	out := map[string]bool{}
	entries, err := os.ReadDir(dirAbs)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// A symbol that exists only behind an unsatisfied build
		// constraint never compiles into the package, so it cannot
		// anchor a binding.
		if !fileBuilds(dirAbs, name) {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(dirAbs, name), nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				out[decl.Name.Name] = true
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.ValueSpec:
						for _, ident := range spec.Names {
							out[ident.Name] = true
						}
					case *ast.TypeSpec:
						out[spec.Name.Name] = true
					}
				}
			}
		}
	}
	return out
}

// evidenceInDir returns, for one package directory, the set of fact
// kinds with reducer-consumption evidence. Evidence means a kind
// identifier, wire literal, or Decode seam used in an active
// consumption position: a comparison, a switch case, or anywhere
// inside a call expression (for example a fact loader's kind list
// argument). Mere mentions do not count: declarations, kind-list
// composite literals outside calls (such as a returned []string of
// kinds a domain loads but never decodes), struct/map literals, and
// string constants that reuse a kind's spelling for another purpose
// are all inert. Files behind an unsatisfied //go:build constraint
// contribute nothing: their consumption never compiles. This mirrors
// the distinction the disclosure ledger documents between a loaded
// kind and a consumed kind.
//
// A directory named schemadecode is decode substrate, not a consumer:
// it contributes no evidence itself, and consumption is attributed to
// the handler packages that call its Decode seams (whose kind mapping
// loadKindSymbols learns).
func evidenceInDir(repoRoot, dirAbs string, symbols kindSymbols, knownKinds map[string]bool) map[string]sourcePos {
	out := map[string]sourcePos{}
	if filepath.Base(dirAbs) == "schemadecode" {
		return out
	}
	entries, err := os.ReadDir(dirAbs)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// Consumption that exists only behind an unsatisfied build
		// constraint never runs, so it is not reducer evidence.
		if !fileBuilds(dirAbs, name) {
			continue
		}
		path := filepath.Join(dirAbs, name)
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		scan := &evidenceScan{fset: fset, repoRoot: repoRoot, path: path, symbols: symbols, knownKinds: knownKinds, out: out}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
				scan.walk(fn.Body, walkState{})
			}
		}
	}
	return out
}

type evidenceScan struct {
	fset       *token.FileSet
	repoRoot   string
	path       string
	symbols    kindSymbols
	knownKinds map[string]bool
	out        map[string]sourcePos
}

func (s *evidenceScan) record(kind string, node ast.Node) {
	if _, seen := s.out[kind]; !seen {
		s.out[kind] = posOf(s.fset, s.repoRoot, s.path, node.Pos())
	}
}

// walkState tracks two independent activation levels: active covers
// kind-constant and decode-seam evidence, literal additionally covers
// bare wire-string literals. Literals are far noisier than constants
// (payload keys and resource types reuse kind spellings, and the
// legacy kinds "file" and "repository" match ordinary words), so a
// literal counts only when compared against, or switched on, an
// expression that reads an envelope's FactKind.
type walkState struct {
	active  bool
	literal bool
}

// walk traverses a function body tracking whether the current position
// is an active consumption position. A composite literal deactivates
// its subtree unless the literal itself sits inside a call argument
// (already active), which keeps loader argument lists as evidence
// while standalone kind lists stay inert.
func (s *evidenceScan) walk(node ast.Node, state walkState) {
	if node == nil {
		return
	}
	switch node := node.(type) {
	case *ast.CompositeLit:
		// A slice/array literal inside an active position (a loader
		// call's kind list) stays active. Struct and map literals
		// deactivate: constructing an envelope or table is production
		// or declaration, not consumption.
		_, isArray := node.Type.(*ast.ArrayType)
		elemState := walkState{active: state.active && isArray}
		for _, elt := range node.Elts {
			s.walk(elt, elemState)
		}
		return
	case *ast.CallExpr:
		s.noteCall(node)
		s.walk(node.Fun, walkState{active: true})
		for _, arg := range node.Args {
			s.walk(arg, walkState{active: true})
		}
		return
	case *ast.BinaryExpr:
		s.walk(node.X, walkState{active: true, literal: isFactKindExpr(node.Y)})
		s.walk(node.Y, walkState{active: true, literal: isFactKindExpr(node.X)})
		return
	case *ast.SwitchStmt:
		s.walk(node.Init, walkState{})
		s.walk(node.Tag, walkState{})
		if node.Body != nil {
			for _, stmt := range node.Body.List {
				clause, ok := stmt.(*ast.CaseClause)
				if !ok {
					s.walk(stmt, state)
					continue
				}
				s.walkCase(clause, isFactKindExpr(node.Tag), state)
			}
		}
		return
	case *ast.CaseClause:
		s.walkCase(node, false, state)
		return
	case *ast.BasicLit:
		if state.literal && node.Kind == token.STRING {
			if value, err := strconv.Unquote(node.Value); err == nil && s.knownKinds[value] {
				s.record(value, node)
			}
		}
		return
	case *ast.SelectorExpr:
		if state.active {
			if kind, ok := s.symbols.identToKind[node.Sel.Name]; ok {
				s.record(kind, node)
			}
		}
		s.walk(node.X, state)
		return
	case *ast.Ident:
		if state.active {
			if kind, ok := s.symbols.identToKind[node.Name]; ok {
				s.record(kind, node)
			}
		}
		return
	}
	for _, child := range childNodes(node) {
		s.walk(child, state)
	}
}

func (s *evidenceScan) walkCase(clause *ast.CaseClause, tagIsFactKind bool, outer walkState) {
	for _, expr := range clause.List {
		s.walk(expr, walkState{active: true, literal: tagIsFactKind})
	}
	for _, stmt := range clause.Body {
		s.walk(stmt, outer)
	}
}

// isFactKindExpr reports whether an expression reads a fact kind: a
// .FactKind selector, a plain kind/factKind identifier, or a call
// (such as a kind normalizer) wrapping one of those.
func isFactKindExpr(expr ast.Expr) bool {
	switch expr := expr.(type) {
	case *ast.SelectorExpr:
		return expr.Sel.Name == "FactKind"
	case *ast.Ident:
		return expr.Name == "kind" || expr.Name == "factKind"
	case *ast.CallExpr:
		for _, arg := range expr.Args {
			if isFactKindExpr(arg) {
				return true
			}
		}
	case *ast.ParenExpr:
		return isFactKindExpr(expr.X)
	}
	return false
}

// noteCall records a Decode<Kind> seam call as evidence for its kind.
func (s *evidenceScan) noteCall(call *ast.CallExpr) {
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		if kind, ok := s.symbols.decodeToKind[fun.Sel.Name]; ok {
			s.record(kind, call)
		}
	case *ast.Ident:
		if kind, ok := s.symbols.decodeToKind[fun.Name]; ok {
			s.record(kind, call)
		}
	}
}

// childNodes lists a node's immediate AST children for the generic
// traversal cases evidenceScan does not special-case.
func childNodes(node ast.Node) []ast.Node {
	var out []ast.Node
	ast.Inspect(node, func(child ast.Node) bool {
		if child != nil && child != node {
			out = append(out, child)
			return false
		}
		return true
	})
	return out
}
