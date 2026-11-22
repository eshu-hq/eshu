// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"errors"
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
// relative to the go/ directory plus "::" plus the enclosing function name, so
// one exempt site never hides another minting site in the same file. Every row
// needs a reason, and a row that no longer matches a site is itself a failure.
var factIDProducerAllowlist = map[string]string{
	"internal/collector/conformance/parity/fixture.go::classFact":                                                                         "conformance fixture facts carry no scope or generation and are never persisted",
	"internal/ifa/vuln_collector_odu.go::plantedEnvelope":                                                                                 "planted Ifa fixture: scope and generation are the package constants plantedScopeID and plantedGenerationID, a single fixed pair",
	"internal/query/ssm_host_inventory_overlay.go::ssmObservationsFromFacts":                                                              "read path: forwards the stored fact's own id into an overlay row, mints nothing",
	"internal/reducer/supplychain/core/file_payload_typed.go::projectTypedFileEnvelope":                                                   "decode path: forwards the id of a fact already read from fact_records",
	"internal/reducer/supplychain/core/file_reachability_fold.go::FileReachabilityFold.DigestEnvelope":                                    "in-memory digest envelope appended to an evidence set with a constant id; never written to fact_records",
	"internal/reducer/supplychain/core/file_reachability_fold.go::FileReachabilityFold.PythonReachabilityDigestEnvelope":                  "in-memory digest envelope appended to an evidence set with a constant id; never written to fact_records",
	"internal/storage/postgres/backfill/scan.go::ScanFactEnvelopeWithPayloadSize":                                                         "read path: forwards the id scanned from fact_records",
	"internal/storage/postgres/drift_overlay_tfstate.go::overlayEnvelope":                                                                 "read path: forwards the id scanned from fact_records",
	"internal/storage/postgres/installed_advisory_targets_os_package_envelope.go::osPackageAdvisoryFactEnvelopeFromTarget":                "read path: forwards the stored target's own fact id, mints nothing",
	"internal/storage/postgres/testfixtures/activation.go::ActivationRepositoryFact":                                                      "test fixture helper: the caller supplies the id",
	"internal/reducer/cloudasset/cloud_asset_resolution_writer.go::PostgresCloudAssetResolutionWriter.WriteCloudAssetResolution":          "the fact id is the reducer intent id (scope, generation, domain and entity key joined; not injective, #11215); factwrite.SingleInsertQuery's scope owner leg refuses a row held by another scope",
	"internal/reducer/platformfam/platform_materialization_writer.go::PostgresPlatformMaterializationWriter.WritePlatformMaterialization": "the fact id is the reducer intent id (scope, generation, domain and entity key joined; not injective, #11215); factwrite.SingleInsertQuery's scope owner leg refuses a row held by another scope",
	"internal/reducer/workload_identity_writer.go::PostgresWorkloadIdentityWriter.WriteWorkloadIdentity":                                  "the fact id is the reducer intent id (scope, generation, domain and entity key joined; not injective, #11215); factwrite.SingleInsertQuery's scope owner leg refuses a row held by another scope",
	"internal/reducer/packages/correlation/writer.go::PostgresPackageWriter.buildRow":                                                     "forwards a factID parameter minted by packageOwnershipFactID, packageConsumptionFactID and packagePublicationFactID, each of which is a checked site of its own",
	"internal/storage/postgres/decisions/decisions.go::scanEvidenceRows":                                                                  "read path: forwards the fact id scanned from stored evidence rows, mints nothing",
	"internal/storage/postgres/supply_chain_impact_winners_staged_compact.go::StagedCompactor.AddOverrideBatch":                           "forwards the fact id of winner rows already stored in fact_records into the compaction state, mints nothing",
	"internal/storage/postgres/vulnerability_suppression_store.go::vulnerabilitySuppressionFactID":                                        "single constant scope (vulnerabilitySuppressionScopeID); the generation and the stable key are hashed, so the id is distinct per generation. Safe only while the scope stays one constant",
	"internal/storage/postgres/vulnerability_suppression_store.go::writeVulnerabilitySuppressionFacts":                                    "MINTS its id (vulnerabilitySuppressionFactID): single constant scope (vulnerabilitySuppressionScopeID); generation hashed. Safe only while the scope stays one constant",
}

// factIDSite is one Envelope literal that sets FactID.
type factIDSite struct {
	pos       string
	file      string
	fn        string // enclosing function, "" outside one
	expr      ast.Expr
	enclosing *ast.FuncDecl
}

// key is the allowlist key: file plus enclosing function.
func (s factIDSite) key() string { return s.file + "::" + s.fn }

// factIDProducerViolations scans Go source text (slash path to contents) and
// returns one message per Envelope literal whose FactID is not derived from
// both scope and generation and has no allowlist row. sites counts the
// literals seen; a scan that sees none is an error, never a clean pass.
// usedAllow records which allowlist rows matched a site.
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
		for _, site := range factIDSites(fset, name, parsed[name]) {
			sites++
			problem := factIDSiteProblem(site, helpers[slashDir(name)])
			if problem == "" {
				continue
			}
			if _, ok := allow[site.key()]; ok {
				usedAllow[site.key()] = true
				continue
			}
			violations = append(violations, site.pos+" ["+site.key()+"]: FactID "+problem)
		}
	}
	if sites == 0 {
		return nil, 0, nil, errors.New("scanned zero fact id mint sites: the guard saw nothing and would pass vacuously")
	}
	return violations, sites, usedAllow, nil
}

// funcName names a function declaration, "Recv.Method" for a method, "" for none.
func funcName(fn *ast.FuncDecl) string {
	if fn == nil {
		return ""
	}
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		recv := fn.Recv.List[0].Type
		if star, ok := recv.(*ast.StarExpr); ok {
			recv = star.X
		}
		if id, ok := recv.(*ast.Ident); ok {
			return id.Name + "." + fn.Name.Name
		}
	}
	return fn.Name.Name
}

func slashDir(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[:i]
	}
	return ""
}

// factwriteRowTypes are the reducer fact row types whose FactID is a mint site
// (#11216): factwrite.Row and factwrite.VersionedRow, or a bare Row and
// VersionedRow inside the factwrite package itself.
var factwriteRowTypes = map[string]bool{"Row": true, "VersionedRow": true}

// factIDSites finds every place a fact id is minted in file: a
// `Envelope{... FactID: expr ...}` literal, a factwrite.Row or
// factwrite.VersionedRow literal, the fact id positional argument of a call that
// passes factwrite.SingleInsertQuery (the id follows the query), and an id
// assigned after construction (`x.FactID = expr`, a blanking `= ""` mints
// nothing and is skipped).
func factIDSites(fset *token.FileSet, name string, file *ast.File) []factIDSite {
	var out []factIDSite
	inFactwrite := strings.Contains(name, "reducer/factwrite/")
	add := func(fn *ast.FuncDecl, pos token.Pos, expr ast.Expr) {
		out = append(out, factIDSite{
			pos:       fset.Position(pos).String(),
			file:      name,
			fn:        funcName(fn),
			expr:      expr,
			enclosing: fn,
		})
	}
	for _, decl := range file.Decls {
		fn, _ := decl.(*ast.FuncDecl)
		if fn != nil && isFactIDHelper(fn) {
			// A function named ...FactID that returns the id is a mint site of
			// its own: it covers ids threaded through a variable or a parameter
			// to a row literal elsewhere. Its site expression is its own call.
			add(fn, fn.Pos(), &ast.CallExpr{Fun: fn.Name})
		}
		ast.Inspect(decl, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.CompositeLit:
				if !isFactIDLiteral(typed, inFactwrite) {
					return true
				}
				for _, elt := range typed.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "FactID" {
						add(fn, kv.Pos(), kv.Value)
					}
				}
			case *ast.CallExpr:
				for i, arg := range typed.Args {
					sel, ok := arg.(*ast.SelectorExpr)
					if ok && sel.Sel.Name == "SingleInsertQuery" && i+1 < len(typed.Args) {
						add(fn, typed.Args[i+1].Pos(), typed.Args[i+1])
					}
				}
			case *ast.AssignStmt:
				if typed.Tok != token.ASSIGN || len(typed.Lhs) != 1 || len(typed.Rhs) != 1 {
					return true
				}
				sel, ok := typed.Lhs[0].(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "FactID" {
					return true
				}
				if lit, ok := typed.Rhs[0].(*ast.BasicLit); ok && lit.Value == `""` {
					return true
				}
				add(fn, typed.Pos(), typed.Rhs[0])
			}
			return true
		})
	}
	return out
}

// isFactIDHelper reports whether fn is a package-level function named like a
// fact id derivation (the name ends in FactID, or is factID: artifactID is not
// one) that returns exactly one string: the shape every reducer id function has.
func isFactIDHelper(fn *ast.FuncDecl) bool {
	if fn.Recv != nil || fn.Body == nil || fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return false
	}
	result, ok := fn.Type.Results.List[0].Type.(*ast.Ident)
	return ok && result.Name == "string" && (strings.HasSuffix(fn.Name.Name, "FactID") || fn.Name.Name == "factID")
}

// isFactIDLiteral reports whether lit is a composite literal whose FactID key
// mints a durable fact id: Envelope anywhere, factwrite.Row and
// factwrite.VersionedRow, and a bare Row or VersionedRow inside the factwrite
// package. A Row type in another package is not a fact row.
func isFactIDLiteral(lit *ast.CompositeLit, inFactwrite bool) bool {
	switch typed := lit.Type.(type) {
	case *ast.SelectorExpr:
		if typed.Sel.Name == "Envelope" {
			return true
		}
		pkg, ok := typed.X.(*ast.Ident)
		return ok && pkg.Name == "factwrite" && factwriteRowTypes[typed.Sel.Name]
	case *ast.Ident:
		return typed.Name == "Envelope" || (inFactwrite && factwriteRowTypes[typed.Name])
	}
	return false
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
		call, ok := derivedCall(expr)
		if !ok {
			return "is forwarded or constant, not derived from scope and generation (add a reasoned allowlist row only if it mints nothing)"
		}
		// A same-package helper takes its inputs as structs (write, decision),
		// so the scope and generation are named inside it or in the identity
		// function it calls, not in the call's arguments: follow it. Any other
		// call (facts.StableID, a standard-library hash) must name them itself.
		if fun, ok := call.Fun.(*ast.Ident); ok && helpers[fun.Name] != nil {
			if !helperMentionsScopeAndGeneration(helpers, helpers[fun.Name], factIDHelperDepth) {
				return "comes from helper " + fun.Name + " whose body, and the same-package functions it calls, never use a scope and a generation"
			}
			continue
		}
		if !mentionsScopeAndGeneration(call) {
			return "is hashed without a scope and a generation identifier"
		}
	}
	return ""
}

// derivedCall unwraps a string concatenation (`kind + ":" + hash(...)`) to the
// one call that derives the id, so a kind prefix does not hide the hash.
func derivedCall(expr ast.Expr) (*ast.CallExpr, bool) {
	switch typed := expr.(type) {
	case *ast.CallExpr:
		return typed, true
	case *ast.ParenExpr:
		return derivedCall(typed.X)
	case *ast.BinaryExpr:
		if typed.Op != token.ADD {
			return nil, false
		}
		if call, ok := derivedCall(typed.Y); ok {
			return call, true
		}
		return derivedCall(typed.X)
	}
	return nil, false
}

// factIDHelperDepth bounds how many same-package calls the guard follows from a
// fact id helper to the function that names the scope and the generation.
const factIDHelperDepth = 3

// helperMentionsScopeAndGeneration reports whether fn's body, or the body of a
// same-package function it calls within depth hops, names a scope and a
// generation.
func helperMentionsScopeAndGeneration(helpers map[string]*ast.FuncDecl, fn *ast.FuncDecl, depth int) bool {
	if mentionsScopeAndGeneration(fn.Body) {
		return true
	}
	if depth == 0 {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || found {
			return !found
		}
		if fun, ok := call.Fun.(*ast.Ident); ok && helpers[fun.Name] != nil && helpers[fun.Name] != fn {
			found = helperMentionsScopeAndGeneration(helpers, helpers[fun.Name], depth-1)
		}
		return !found
	})
	return found
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
