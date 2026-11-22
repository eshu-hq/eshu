// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// Producer guard for issue #11203. fact_records is keyed on fact_id alone, so
// a producer whose FactID leaves out the scope or the generation lets two
// scopes (or two generations) that emit the same semantic identity share one
// row, and a write from one lands on the other's row. The guard scans every
// facts.Envelope composite literal that sets FactID and requires the value to
// be derived from both the scope and the generation.
//
// Limits, stated so nobody reads more into a green run than it proves:
//   - It reads the composite-literal form only. An envelope whose FactID is
//     assigned after construction (env.FactID = ...) is not seen.
//   - "Derived from scope and generation" is a syntactic check: the call that
//     builds the id (and, for a same-package helper, the helper body) must
//     name both a scope and a generation identifier or string key. It does
//     not prove the hash is injective; the live conflict guard in the
//     streaming save (TestUpsertFactBatchRejectsForeignScopeOrGenerationLive)
//     is the runtime backstop.
//   - A FactID that is only forwarded (a parameter, a field, a decoded row, a
//     constant) cannot be checked here and needs a reasoned allowlist row.

// factIDProducerAllowlist names the Envelope literals whose FactID is not
// derived from scope and generation at the literal, keyed by slash path
// relative to the go/ directory. Every row needs a reason, and a row that no
// longer matches a site is itself a failure.
var factIDProducerAllowlist = map[string]string{
	"internal/collector/conformance/parity/fixture.go":                            "conformance fixture facts carry no scope or generation and are never persisted",
	"internal/ifa/vuln_collector_odu.go":                                          "planted Ifa fixture: scope and generation are the package constants plantedScopeID and plantedGenerationID, a single fixed pair",
	"internal/query/ssm_host_inventory_overlay.go":                                "read path: forwards the stored fact's own id into an overlay row, mints nothing",
	"internal/reducer/supplychain/core/file_payload_typed.go":                     "decode path: forwards the id of a fact already read from fact_records",
	"internal/reducer/supplychain/core/file_reachability_fold.go":                 "in-memory digest envelopes appended to an evidence set with a constant id; never written to fact_records",
	"internal/storage/postgres/backfill/scan.go":                                  "read path: forwards the id scanned from fact_records",
	"internal/storage/postgres/drift_overlay_tfstate.go":                          "read path: forwards the id scanned from fact_records",
	"internal/storage/postgres/installed_advisory_targets_os_package_envelope.go": "read path: forwards the id scanned from fact_records",
	"internal/storage/postgres/testfixtures/activation.go":                        "test fixture helper: caller supplies the id",
	"internal/storage/postgres/vulnerability_suppression_store.go":                "write path under one fixed scope (vulnerabilitySuppressionScopeID): the id hashes the generation and the stable key, so a scope cannot collide with another",
}

// factIDSite is one Envelope literal that sets FactID.
type factIDSite struct {
	pos       string
	file      string
	expr      ast.Expr
	enclosing *ast.FuncDecl
}

// factIDProducerViolations scans Go source text (slash path to contents) and
// returns one message per Envelope literal whose FactID is not derived from
// both scope and generation and has no allowlist row. sites counts the
// literals seen, so a caller can tell a scan that saw nothing from a clean
// one; usedAllow records which allowlist rows matched a site.
func factIDProducerViolations(files map[string]string, allow map[string]string) (violations []string, sites int, usedAllow map[string]bool, err error) {
	usedAllow = map[string]bool{}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	fset := token.NewFileSet()
	parsed := map[string]*ast.File{}
	helpers := map[string]map[string]*ast.FuncDecl{} // directory -> func name -> decl
	for _, name := range names {
		file, parseErr := parser.ParseFile(fset, name, files[name], 0)
		if parseErr != nil {
			return nil, 0, nil, parseErr
		}
		parsed[name] = file
		dir := slashDir(name)
		if helpers[dir] == nil {
			helpers[dir] = map[string]*ast.FuncDecl{}
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Body != nil {
				helpers[dir][fn.Name.Name] = fn
			}
		}
	}

	for _, name := range names {
		for _, site := range envelopeFactIDSites(fset, name, parsed[name]) {
			sites++
			problem := factIDSiteProblem(site, helpers[slashDir(name)])
			if problem == "" {
				continue
			}
			if _, ok := allow[site.file]; ok {
				usedAllow[site.file] = true
				continue
			}
			violations = append(violations, site.pos+": FactID "+problem)
		}
	}
	return violations, sites, usedAllow, nil
}

func slashDir(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[:i]
	}
	return ""
}

// envelopeFactIDSites finds every `Envelope{... FactID: expr ...}` literal.
func envelopeFactIDSites(fset *token.FileSet, name string, file *ast.File) []factIDSite {
	var out []factIDSite
	for _, decl := range file.Decls {
		fn, _ := decl.(*ast.FuncDecl)
		ast.Inspect(decl, func(node ast.Node) bool {
			lit, ok := node.(*ast.CompositeLit)
			if !ok || compositeTypeName(lit) != "Envelope" {
				return true
			}
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "FactID" {
					out = append(out, factIDSite{
						pos:       fset.Position(kv.Pos()).String(),
						file:      name,
						expr:      kv.Value,
						enclosing: fn,
					})
				}
			}
			return true
		})
	}
	return out
}

func compositeTypeName(lit *ast.CompositeLit) string {
	switch typed := lit.Type.(type) {
	case *ast.SelectorExpr:
		return typed.Sel.Name
	case *ast.Ident:
		return typed.Name
	}
	return ""
}

// factIDSiteProblem returns "" when the site's FactID is derived from scope
// and generation, otherwise a short reason.
func factIDSiteProblem(site factIDSite, helpers map[string]*ast.FuncDecl) string {
	exprs := []ast.Expr{site.expr}
	if ident, ok := site.expr.(*ast.Ident); ok && site.enclosing != nil {
		if assigned := localAssignments(site.enclosing, ident.Name); len(assigned) > 0 {
			exprs = assigned
		}
	}
	for _, expr := range exprs {
		call, ok := expr.(*ast.CallExpr)
		if !ok {
			return "is forwarded or constant, not derived from scope and generation (add a reasoned allowlist row only if it mints nothing)"
		}
		if !mentionsScopeAndGeneration(call) {
			return "is hashed without a scope and a generation identifier"
		}
		if fun, ok := call.Fun.(*ast.Ident); ok {
			if helper := helpers[fun.Name]; helper != nil && !mentionsScopeAndGeneration(helper.Body) {
				return "comes from helper " + fun.Name + " whose body never uses a scope and a generation"
			}
		}
	}
	return ""
}

// localAssignments returns the right-hand sides assigned to name inside fn
// (`name := expr`, `name = expr`, `var name = expr`).
func localAssignments(fn *ast.FuncDecl, name string) []ast.Expr {
	var out []ast.Expr
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.AssignStmt:
			if len(typed.Lhs) == len(typed.Rhs) {
				for i, lhs := range typed.Lhs {
					if id, ok := lhs.(*ast.Ident); ok && id.Name == name {
						out = append(out, typed.Rhs[i])
					}
				}
			}
		case *ast.ValueSpec:
			if len(typed.Names) == len(typed.Values) {
				for i, id := range typed.Names {
					if id.Name == name {
						out = append(out, typed.Values[i])
					}
				}
			}
		}
		return true
	})
	return out
}

// mentionsScopeAndGeneration reports whether node names a scope and a
// generation, by identifier or by string literal (a hash map key such as
// "scope_id").
func mentionsScopeAndGeneration(node ast.Node) bool {
	var scope, generation bool
	ast.Inspect(node, func(n ast.Node) bool {
		var text string
		switch typed := n.(type) {
		case *ast.Ident:
			text = typed.Name
		case *ast.BasicLit:
			if typed.Kind == token.STRING {
				if unquoted, err := strconv.Unquote(typed.Value); err == nil {
					text = unquoted
				}
			}
		}
		text = strings.ToLower(text)
		scope = scope || strings.Contains(text, "scope")
		generation = generation || strings.Contains(text, "generation")
		return true
	})
	return scope && generation
}
