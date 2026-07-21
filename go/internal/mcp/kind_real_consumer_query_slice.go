// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
)

// pqArraySliceFactKinds scans every non-test .go file directly under dir
// (expected: go/internal/query) for package-level `<name> = []string{"a",
// "b", ...}` var/const declarations, then separately for `pq.Array(<name>)`
// call arguments referencing one of those identifiers, and returns the
// union of literal kind strings from every slice actually passed to
// pq.Array.
//
// This closes a second round-2 #5474 blind spot:
// vulnerabilitySourceSnapshotFactKinds (supply_chain_impact_readiness_families.go:39,
// `[]string{"vulnerability.source_snapshot"}`) is bound via
// `pq.Array(vulnerabilitySourceSnapshotFactKinds)`
// (supply_chain_impact_readiness_postgres.go:76) into positional parameter
// $8 of listSupplyChainImpactReadinessQuery, whose
// `WHERE fact.fact_kind = ANY($8::text[])` (supply_chain_impact_readiness_postgres_query.go:179)
// feeds the vulnerability_source_snapshot_active CTE, whose payload fields
// (source, ecosystem, cache_artifact_version, warning_message, ...) are read
// via `payload->>'field'` into the readiness API response
// (supply_chain_impact_readiness_postgres_query.go:404-423) — a genuine
// consumer with no typed decode seam, no locally-declared single-string
// const (namedConstStoreKinds only matches a bare string const, not a
// []string slice), and no literal `fact_kind = '...'` predicate
// (rawSQLFactKindReaders only matches literal quotes, not a `= ANY($N)`
// parameterized array bind).
//
// The `pq.Array(<name>)` requirement is what keeps this precise: a
// same-shaped local slice that is declared but never bound into a query —
// there is no such case in go/internal/query today, but the requirement is
// deliberate so a future dead `*FactKinds` slice does not silently count as
// a consumer. This mirrors the payload-read requirement in
// postgresPayloadReaderKinds and the case/equality-dispatch requirement in
// factsDispatchedKinds: mere declaration or a load-list append is not
// consumption, only a call site that actually feeds a live query is.
func pqArraySliceFactKinds(dir string) (map[string]bool, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, fmt.Errorf("kind_real_consumer: glob %s: %w", dir, err)
	}

	fset := token.NewFileSet()
	sliceLiterals := map[string][]string{} // identifier -> declared []string{"a","b"} values
	var files []*ast.File
	for _, path := range matches {
		if isGoTestFile(path) {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("kind_real_consumer: parse %s: %w", path, err)
		}
		files = append(files, file)
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || (gen.Tok != token.VAR && gen.Tok != token.CONST) {
				continue
			}
			for _, spec := range gen.Specs {
				vspec, ok := spec.(*ast.ValueSpec)
				if !ok || len(vspec.Names) != 1 || len(vspec.Values) != 1 {
					continue
				}
				values, ok := stringSliceLiteralValues(vspec.Values[0])
				if !ok {
					continue
				}
				sliceLiterals[vspec.Names[0].Name] = values
			}
		}
	}

	boundIdents := map[string]bool{}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Array" {
				return true
			}
			if pkgIdent, ok := sel.X.(*ast.Ident); !ok || pkgIdent.Name != "pq" {
				return true
			}
			if ident, ok := call.Args[0].(*ast.Ident); ok {
				boundIdents[ident.Name] = true
			}
			return true
		})
	}

	kinds := map[string]bool{}
	for name, values := range sliceLiterals {
		if !boundIdents[name] {
			continue
		}
		for _, v := range values {
			kinds[v] = true
		}
	}
	return kinds, nil
}

// stringSliceLiteralValues reports whether expr is a `[]string{"a", "b",
// ...}` composite literal of plain string BasicLits, returning the unquoted
// values.
func stringSliceLiteralValues(expr ast.Expr) ([]string, bool) {
	comp, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil, false
	}
	arr, ok := comp.Type.(*ast.ArrayType)
	if !ok || arr.Len != nil {
		return nil, false
	}
	elt, ok := arr.Elt.(*ast.Ident)
	if !ok || elt.Name != "string" {
		return nil, false
	}
	values := make([]string, 0, len(comp.Elts))
	for _, e := range comp.Elts {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return nil, false
		}
		unquoted, err := unquoteGoString(lit.Value)
		if err != nil {
			return nil, false
		}
		values = append(values, unquoted)
	}
	return values, true
}
