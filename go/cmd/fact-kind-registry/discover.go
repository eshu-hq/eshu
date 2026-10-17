// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

// contractImportPath is the only import path whose Declare/Bind calls
// the discovery scan treats as fact-family contract declarations.
const contractImportPath = "github.com/eshu-hq/eshu/go/internal/facts/contract"

// sourcePos is one declaration source location in repo-relative form.
type sourcePos struct {
	file string
	line int
}

func (p sourcePos) String() string { return fmt.Sprintf("%s:%d", p.file, p.line) }

// discoveredFamily is one contract.Family declaration with the source
// positions diagnostics need.
type discoveredFamily struct {
	family  contract.Family
	pos     sourcePos
	kindPos map[string]sourcePos
	prodPos []sourcePos
}

// discoveredBinding is one contract.Binding declaration with its source
// position.
type discoveredBinding struct {
	binding contract.Binding
	pos     sourcePos
}

// diagnostics accumulates independent validation failures so one run
// reports every broken declaration instead of stopping at the first.
type diagnostics []string

func (d *diagnostics) addf(pos sourcePos, format string, args ...any) {
	*d = append(*d, pos.String()+": "+fmt.Sprintf(format, args...))
}

func (d diagnostics) err() error {
	if len(d) == 0 {
		return nil
	}
	sorted := append(diagnostics(nil), d...)
	sort.Strings(sorted)
	return fmt.Errorf("fact-family contract validation failed:\n%s", strings.Join(sorted, "\n"))
}

// discoverDeclarations statically scans the Go tree under goRoot for
// contract.Declare and contract.Bind declarations. It uses go/parser
// only: packages are never compiled, loaded, or executed, so scanned
// package initialization cannot run. repoRoot converts file paths to
// the repo-relative form diagnostics and FactDir checks use.
func discoverDeclarations(repoRoot, goRoot string) ([]discoveredFamily, []discoveredBinding, diagnostics) {
	var families []discoveredFamily
	var bindings []discoveredBinding
	var diags diagnostics
	// String constants from the contract package source resolve
	// <local>.<Const> references in declarations (see evalString), so
	// declarations get compile-checked spellings.
	contractConsts := loadContractStringConsts(repoRoot)
	fset := token.NewFileSet()
	walkErr := filepath.WalkDir(goRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "vendor" || entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			diags.addf(posOf(fset, repoRoot, path, 0), "cannot parse Go source: %v; ACTION: fix the syntax error", err)
			return nil
		}
		local := contractLocalName(file)
		if local == "" {
			return nil
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				fam, bind, found := evalValueSpec(fset, repoRoot, path, local, contractConsts, spec, &diags)
				if !found {
					continue
				}
				if fam != nil {
					families = append(families, *fam)
				}
				if bind != nil {
					bindings = append(bindings, *bind)
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		diags.addf(sourcePos{file: goRoot, line: 1}, "cannot scan Go tree: %v; ACTION: check the -repo-root path", walkErr)
	}
	sort.Slice(families, func(i, j int) bool { return families[i].pos.String() < families[j].pos.String() })
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].pos.String() < bindings[j].pos.String() })
	return families, bindings, diags
}

// contractLocalName returns the file-local name bound to the contract
// package import, or "" when the file does not import it.
func contractLocalName(file *ast.File) string {
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != contractImportPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "contract"
	}
	return ""
}

func posOf(fset *token.FileSet, repoRoot, path string, offset token.Pos) sourcePos {
	line := 1
	if offset != 0 {
		line = fset.Position(offset).Line
	}
	rel, err := filepath.Rel(repoRoot, path)
	if err != nil {
		rel = path
	}
	return sourcePos{file: filepath.ToSlash(rel), line: line}
}

// evalValueSpec evaluates one var spec when it is a Declare/Bind call.
// found reports whether the spec was such a call (even when invalid).
func evalValueSpec(fset *token.FileSet, repoRoot, path, local string, contractConsts map[string]string, spec ast.Spec, diags *diagnostics) (*discoveredFamily, *discoveredBinding, bool) {
	valueSpec, ok := spec.(*ast.ValueSpec)
	if !ok || len(valueSpec.Values) != 1 {
		return nil, nil, false
	}
	call, ok := valueSpec.Values[0].(*ast.CallExpr)
	if !ok {
		return nil, nil, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "Declare" && sel.Sel.Name != "Bind") {
		return nil, nil, false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok || pkgIdent.Name != local {
		return nil, nil, false
	}
	pos := posOf(fset, repoRoot, path, call.Pos())
	if len(call.Args) != 1 {
		diags.addf(pos, "%s takes exactly one struct-literal argument, got %d; ACTION: pass a single %s literal", sel.Sel.Name, len(call.Args), map[string]string{"Declare": "contract.Family", "Bind": "contract.Binding"}[sel.Sel.Name])
		return nil, nil, true
	}
	ev := &literalEval{fset: fset, repoRoot: repoRoot, path: path, local: local, contractConsts: contractConsts, diags: diags}
	if sel.Sel.Name == "Declare" {
		var family contract.Family
		if !ev.eval(call.Args[0], reflect.ValueOf(&family).Elem()) {
			return nil, nil, true
		}
		return &discoveredFamily{family: family, pos: pos, kindPos: ev.kindPos, prodPos: ev.prodPos}, nil, true
	}
	var binding contract.Binding
	if !ev.eval(call.Args[0], reflect.ValueOf(&binding).Elem()) {
		return nil, nil, true
	}
	return nil, &discoveredBinding{binding: binding, pos: pos}, true
}

// literalEval evaluates the restricted literal grammar into contract
// model values by reflection. Only string/bool literals, <local>.<Const>
// references to string constants declared in the contract package,
// slice literals, and keyed struct literals from the contract package
// are supported; every other expression is a diagnostic naming the
// exact source position and the corrective action.
type literalEval struct {
	fset           *token.FileSet
	repoRoot       string
	path           string
	local          string
	contractConsts map[string]string
	diags          *diagnostics
	kindPos        map[string]sourcePos
	prodPos        []sourcePos
}

func (e *literalEval) pos(node ast.Node) sourcePos {
	return posOf(e.fset, e.repoRoot, e.path, node.Pos())
}

func (e *literalEval) eval(expr ast.Expr, target reflect.Value) bool {
	switch target.Kind() {
	case reflect.String:
		return e.evalString(expr, target)
	case reflect.Bool:
		return e.evalBool(expr, target)
	case reflect.Slice:
		return e.evalSlice(expr, target)
	case reflect.Struct:
		return e.evalStruct(expr, target)
	default:
		e.diags.addf(e.pos(expr), "unsupported contract field type %s; ACTION: extend the contract model and generator together", target.Type())
		return false
	}
}

func (e *literalEval) evalString(expr ast.Expr, target reflect.Value) bool {
	if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			e.diags.addf(e.pos(expr), "malformed string literal; ACTION: fix the quoting")
			return false
		}
		target.SetString(value)
		return true
	}
	// A <local>.<Const> reference resolves against the contract
	// package's string constants, so declarations get compile-checked
	// spellings (a typo'd constant fails the Go build instead of
	// slipping a typo'd value past it to the generate gate).
	if sel, ok := expr.(*ast.SelectorExpr); ok {
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == e.local {
			if value, ok := e.contractConsts[sel.Sel.Name]; ok {
				target.SetString(value)
				return true
			}
			e.diags.addf(e.pos(expr), "unknown contract constant %q; ACTION: use a quoted string literal or a string constant declared in go/internal/facts/contract", sel.Sel.Name)
			return false
		}
	}
	e.diags.addf(e.pos(expr), "value is not a string literal; ACTION: replace it with a quoted string literal or a %s.<Const> reference to a contract string constant (variables, calls, and concatenations are not allowed in contract declarations)", e.local)
	return false
}

// loadContractStringConsts parses the contract package source once per
// discovery run and maps its string constant names to values. The
// discovery scan stays parse-only: constants are read as source text,
// never evaluated, so scanned package initialization cannot run.
func loadContractStringConsts(repoRoot string) map[string]string {
	out := map[string]string{}
	dir := filepath.Join(repoRoot, "go", "internal", "facts", "contract")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				values := valueSpec.Values
				// One value may serve several names (const A, B = "x").
				if len(values) == 1 && len(valueSpec.Names) > 1 {
					for _, ident := range valueSpec.Names {
						setContractConst(values[0], ident.Name, out)
					}
					continue
				}
				for i, ident := range valueSpec.Names {
					if i >= len(values) {
						continue
					}
					setContractConst(values[i], ident.Name, out)
				}
			}
		}
	}
	return out
}

// setContractConst records one constant name when its value is a
// string literal. Typed (const A Disposition = "x") and untyped
// constants both qualify; anything computed does not.
func setContractConst(expr ast.Expr, name string, out map[string]string) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return
	}
	if value, err := strconv.Unquote(lit.Value); err == nil {
		out[name] = value
	}
}

func (e *literalEval) evalBool(expr ast.Expr, target reflect.Value) bool {
	ident, ok := expr.(*ast.Ident)
	if !ok || (ident.Name != "true" && ident.Name != "false") {
		e.diags.addf(e.pos(expr), "value is not a boolean literal; ACTION: use true or false")
		return false
	}
	target.SetBool(ident.Name == "true")
	return true
}

func (e *literalEval) evalSlice(expr ast.Expr, target reflect.Value) bool {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		e.diags.addf(e.pos(expr), "value is not a slice literal; ACTION: use a []T{...} literal with literal elements")
		return false
	}
	out := reflect.MakeSlice(target.Type(), 0, len(lit.Elts))
	ok = true
	for _, elt := range lit.Elts {
		elem := reflect.New(target.Type().Elem()).Elem()
		if e.eval(elt, elem) {
			out = reflect.Append(out, elem)
			e.recordElemPos(elem, elt)
		} else {
			ok = false
		}
	}
	target.Set(out)
	return ok
}

// recordElemPos remembers Kind and Producer literal positions so
// per-kind diagnostics point at the offending element, not the family.
func (e *literalEval) recordElemPos(elem reflect.Value, elt ast.Expr) {
	switch value := elem.Interface().(type) {
	case contract.Kind:
		if e.kindPos == nil {
			e.kindPos = map[string]sourcePos{}
		}
		if _, dup := e.kindPos[value.Kind]; !dup {
			e.kindPos[value.Kind] = e.pos(elt)
		}
	case contract.Producer:
		e.prodPos = append(e.prodPos, e.pos(elt))
	}
}

func (e *literalEval) evalStruct(expr ast.Expr, target reflect.Value) bool {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		e.diags.addf(e.pos(expr), "value is not a %s struct literal; ACTION: use a keyed %s{...} literal", target.Type().Name(), target.Type().Name())
		return false
	}
	if !e.literalTypeMatches(lit, target.Type().Name()) {
		e.diags.addf(e.pos(lit), "struct literal is not %s.%s; ACTION: use the contract package type %s", e.local, target.Type().Name(), target.Type().Name())
		return false
	}
	ok = true
	seen := map[string]sourcePos{}
	for _, elt := range lit.Elts {
		kv, isKV := elt.(*ast.KeyValueExpr)
		if !isKV {
			e.diags.addf(e.pos(elt), "positional struct fields are not allowed; ACTION: name every field (Field: value)")
			ok = false
			continue
		}
		key, isIdent := kv.Key.(*ast.Ident)
		if !isIdent {
			e.diags.addf(e.pos(kv.Key), "field key is not an identifier; ACTION: use contract field names")
			ok = false
			continue
		}
		if prev, dup := seen[key.Name]; dup {
			e.diags.addf(e.pos(kv), "field %q is set twice (first at %s); ACTION: keep one %s entry", key.Name, prev, key.Name)
			ok = false
			continue
		}
		seen[key.Name] = e.pos(kv)
		field := target.FieldByName(key.Name)
		if !field.IsValid() || !field.CanSet() {
			e.diags.addf(e.pos(kv), "unknown field %q on %s; ACTION: use a declared contract field or extend go/internal/facts/contract", key.Name, target.Type().Name())
			ok = false
			continue
		}
		if !e.eval(kv.Value, field) {
			ok = false
		}
	}
	return ok
}

// literalTypeMatches reports whether a composite literal's type is the
// named contract type, written as local.Name, Name (same-package test
// fixtures never qualify), or via a slice element type.
func (e *literalEval) literalTypeMatches(lit *ast.CompositeLit, want string) bool {
	switch typ := lit.Type.(type) {
	case *ast.SelectorExpr:
		pkg, ok := typ.X.(*ast.Ident)
		return ok && pkg.Name == e.local && typ.Sel.Name == want
	case *ast.Ident:
		// Element literals inside []contract.Kind{{...}} elide the type
		// entirely (nil Type). A bare identifier is accepted only when
		// it is exactly the type name; the import check on the outer
		// Declare/Bind call already binds the file to this package.
		return typ.Name == want
	case nil:
		return true
	default:
		return false
	}
}
