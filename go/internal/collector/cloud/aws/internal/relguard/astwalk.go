// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relguard

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// relationshipObservationType is the unqualified struct type name whose
// TargetType field the static layer inspects. Scanner code constructs it as
// awscloud.RelationshipObservation, so the selector's Sel name is matched.
const relationshipObservationType = "RelationshipObservation"

// targetTypeField is the struct field carrying the join type the guard checks.
const targetTypeField = "TargetType"

// scannerSourceFiles are the per-service file names the static layer parses for
// target_type literals. Relationship construction lives in these files across
// the scanner fleet; helper-only files (awssdk adapters, fixtures) are skipped.
var scannerSourceFiles = map[string]struct{}{
	"relationships.go": {},
	"scanner.go":       {},
	"observations.go":  {},
}

// EmittedTargetType is one statically resolved target_type literal found in
// scanner source. ConstBacked is true when the value came from a qualified
// awscloud.ResourceType* selector, which the compiler already guarantees
// resolves to a declared constant; the guard records it for completeness but it
// cannot be an unknown value.
type EmittedTargetType struct {
	// Value is the resolved target_type string.
	Value string
	// File is the source file the literal was found in, for failure messages.
	File string
	// ConstBacked marks a value sourced from an awscloud.ResourceType* selector.
	ConstBacked bool
}

// EmittedTargetTypeLiterals AST-walks every scanner package under servicesDir
// and returns the target_type values it can resolve statically:
//
//   - a basic string literal assigned to TargetType;
//   - an identifier bound (by const or by a single literal assignment) to a
//     string literal in the same package;
//   - a qualified awscloud.ResourceType* selector (recorded as ConstBacked).
//
// It returns the count of TargetType expressions it could NOT resolve
// (unresolved), which are helper calls and field reads that only the runtime
// layer can check. Resolving these here is intentionally out of scope; the
// runtime layer (Check/AssertObservations) covers them.
func EmittedTargetTypeLiterals(servicesDir string) (literals []EmittedTargetType, unresolved int, err error) {
	serviceDirs, err := os.ReadDir(servicesDir)
	if err != nil {
		return nil, 0, fmt.Errorf("read services dir %q: %w", servicesDir, err)
	}
	for _, serviceDir := range serviceDirs {
		if !serviceDir.IsDir() {
			continue
		}
		dir := filepath.Join(servicesDir, serviceDir.Name())
		dirLiterals, dirUnresolved, walkErr := emittedFromPackage(dir)
		if walkErr != nil {
			return nil, 0, walkErr
		}
		literals = append(literals, dirLiterals...)
		unresolved += dirUnresolved
	}
	sort.Slice(literals, func(i, j int) bool {
		if literals[i].Value != literals[j].Value {
			return literals[i].Value < literals[j].Value
		}
		return literals[i].File < literals[j].File
	})
	return literals, unresolved, nil
}

// emittedFromPackage parses the scanner source files in a single service
// directory and resolves their TargetType expressions. The string-constant
// environment is built per directory so an identifier resolves against the same
// package's constants regardless of which file declares it.
func emittedFromPackage(dir string) (literals []EmittedTargetType, unresolved int, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, fmt.Errorf("read package dir %q: %w", dir, err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		if _, ok := scannerSourceFiles[entry.Name()]; !ok {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		file, parseErr := goparser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return nil, 0, fmt.Errorf("parse %q: %w", path, parseErr)
		}
		files = append(files, file)
		paths = append(paths, path)
	}
	if len(files) == 0 {
		return nil, 0, nil
	}
	strConsts := packageStringConstants(files)
	for i, file := range files {
		fileLiterals, fileUnresolved := resolveTargetTypes(file, paths[i], strConsts)
		literals = append(literals, fileLiterals...)
		unresolved += fileUnresolved
	}
	return literals, unresolved, nil
}

// packageStringConstants maps every package-level string identifier to its
// literal value across the parsed files of one scanner package. It covers both
// const declarations and package-level var declarations bound to a single
// string literal, since scanners use both shapes for target-type tokens.
func packageStringConstants(files []*ast.File) map[string]string {
	values := map[string]string{}
	for _, file := range files {
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || (genDecl.Tok != token.CONST && genDecl.Tok != token.VAR) {
				continue
			}
			for _, spec := range genDecl.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range valueSpec.Names {
					if i >= len(valueSpec.Values) {
						continue
					}
					if value, ok := stringLiteral(valueSpec.Values[i]); ok {
						values[name.Name] = value
					}
				}
			}
		}
	}
	return values
}

// resolveTargetTypes finds every RelationshipObservation composite literal in
// file and resolves the TargetType field expression to a literal value when it
// can. Local single-assignment string variables (targetType := "aws_x") are
// resolved per file so the load-balancer-style branch literals are caught.
func resolveTargetTypes(
	file *ast.File,
	path string,
	pkgConsts map[string]string,
) (literals []EmittedTargetType, unresolved int) {
	localStrings := localStringAssignments(file)
	ast.Inspect(file, func(n ast.Node) bool {
		composite, ok := n.(*ast.CompositeLit)
		if !ok || !isRelationshipObservation(composite.Type) {
			return true
		}
		expr := targetTypeExpr(composite)
		if expr == nil {
			// No TargetType set: an empty target_type the runtime layer rejects.
			unresolved++
			return true
		}
		lit, constBacked, resolved := resolveExpr(expr, pkgConsts, localStrings)
		if !resolved {
			unresolved++
			return true
		}
		literals = append(literals, EmittedTargetType{Value: lit, File: path, ConstBacked: constBacked})
		return true
	})
	return literals, unresolved
}

// resolveExpr resolves a TargetType expression to a string value. It returns
// constBacked=true for awscloud.ResourceType* selectors, whose value is fixed
// by the compiler. Helper calls, field selectors, and unknown identifiers
// return resolved=false so the runtime layer handles them.
func resolveExpr(
	expr ast.Expr,
	pkgConsts map[string]string,
	localStrings map[string]string,
) (value string, constBacked bool, resolved bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if v, ok := stringLiteral(e); ok {
			return v, false, true
		}
	case *ast.Ident:
		if v, ok := pkgConsts[e.Name]; ok {
			return v, false, true
		}
		if v, ok := localStrings[e.Name]; ok {
			return v, false, true
		}
	case *ast.SelectorExpr:
		// awscloud.ResourceType* is compiler-guaranteed to be a declared
		// constant; record the const-backed marker without a literal value.
		if strings.HasPrefix(e.Sel.Name, resourceTypeConstPrefix) {
			return "", true, true
		}
	}
	return "", false, false
}

// localStringAssignments collects file-local identifiers assigned a string
// literal anywhere in file. A name assigned more than one distinct literal is
// dropped, because a branch could pick either value and the static layer must
// not guess which; the runtime layer checks those. This still catches the
// common single-literal initializer shape (targetType := "aws_x") that scanner
// load-balancer branches use.
func localStringAssignments(file *ast.File) map[string]string {
	values := map[string]string{}
	conflicts := map[string]struct{}{}
	record := func(lhs, rhs ast.Expr) {
		ident, ok := lhs.(*ast.Ident)
		if !ok {
			return
		}
		literal, ok := stringLiteral(rhs)
		if !ok {
			return
		}
		if existing, seen := values[ident.Name]; seen && existing != literal {
			conflicts[ident.Name] = struct{}{}
			return
		}
		values[ident.Name] = literal
	}
	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		if len(assign.Lhs) != len(assign.Rhs) {
			return true
		}
		for i := range assign.Lhs {
			record(assign.Lhs[i], assign.Rhs[i])
		}
		return true
	})
	for name := range conflicts {
		delete(values, name)
	}
	return values
}

// isRelationshipObservation reports whether a composite-literal type names the
// awscloud RelationshipObservation struct, qualified (awscloud.X) or bare (X)
// for code inside the awscloud package itself.
func isRelationshipObservation(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.SelectorExpr:
		return t.Sel.Name == relationshipObservationType
	case *ast.Ident:
		return t.Name == relationshipObservationType
	}
	return false
}

// targetTypeExpr returns the value expression assigned to the TargetType field
// of a composite literal, or nil when the field is not set.
func targetTypeExpr(composite *ast.CompositeLit) ast.Expr {
	for _, elt := range composite.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != targetTypeField {
			continue
		}
		return kv.Value
	}
	return nil
}
