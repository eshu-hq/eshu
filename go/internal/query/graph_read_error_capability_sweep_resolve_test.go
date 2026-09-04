// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/ast"
	"go/token"
	"strconv"
)

// capabilitySweep and its resolvers are split out of
// graph_read_error_capability_sweep_test.go: that file crossed the repo's
// 500-line cap once the resolved-call-site floor was added. The assertion
// lives there; the AST machinery that feeds it lives here.
//
// #6060 made this sweep walk go/internal/query recursively, so its decl maps
// now see multiple packages. constStrings and funcDecls are keyed by
// (declaring directory, bare identifier) rather than by bare identifier
// alone, and every lookup resolves against a directory computed by dirOf
// (graph_read_error_capability_sweep_scope_test.go), which also carries the
// full rationale and the discriminating regression test for this scoping.
type capabilitySweep struct {
	constStrings map[string]map[string]string
	funcDecls    map[string]map[string]*ast.FuncDecl
	// packageNames maps a swept directory to the package clause declared by
	// the files in it, so a package-qualified constant (leaf.Capability from
	// a #6060 family leaf) resolves against the declaring package's own
	// directory rather than by bare identifier.
	packageNames map[string]string
	// callSites maps a called function/method name (Ident/Selector name only)
	// to every call site of it across the package, so a capability parameter
	// threaded through a helper can be resolved back to what each caller
	// passed. NOT directory-scoped: a leaf's exported function is legitimately
	// called from another directory, and resolveParam resolves each caller's
	// argument against that caller's own directory, not the callee's.
	callSites map[string][]capabilityCallSite
	fset      *token.FileSet
}

// capabilityCallSite is one call expression together with the *ast.FuncDecl it
// appears inside, so parameter-forwarding resolution can recurse into the
// caller's own locals/parameters.
type capabilityCallSite struct {
	call      *ast.CallExpr
	enclosing *ast.FuncDecl
}

func newCapabilitySweep(fset *token.FileSet) *capabilitySweep {
	return &capabilitySweep{
		constStrings: map[string]map[string]string{},
		funcDecls:    map[string]map[string]*ast.FuncDecl{},
		packageNames: map[string]string{},
		callSites:    map[string][]capabilityCallSite{},
		fset:         fset,
	}
}

// collectCallSites records every call expression in file against its callee
// name (Ident.Name for a free function, SelectorExpr.Sel.Name for a method
// call) and its enclosing function declaration.
func (s *capabilitySweep) collectCallSites(file *ast.File) {
	var funcStack []*ast.FuncDecl
	var visit func(ast.Node) bool
	visit = func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			funcStack = append(funcStack, node)
			if node.Body != nil {
				ast.Inspect(node.Body, visit)
			}
			funcStack = funcStack[:len(funcStack)-1]
			return false
		case *ast.CallExpr:
			name := calleeName(node.Fun)
			if name == "" {
				break
			}
			var enclosing *ast.FuncDecl
			if len(funcStack) > 0 {
				enclosing = funcStack[len(funcStack)-1]
			}
			s.callSites[name] = append(s.callSites[name], capabilityCallSite{call: node, enclosing: enclosing})
		}
		return true
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		visit(fn)
	}
}

// calleeName returns the plain name of a call's callee: the identifier for a
// free-function call, or the selected method name for a method call. Anything
// else (a func literal invoked inline, a map/slice index, etc.) reports "".
func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	default:
		return ""
	}
}

// paramIndex returns the zero-based position of a parameter named name in
// fn's signature, flattening grouped parameter names (func f(a, b string)).
func paramIndex(fn *ast.FuncDecl, name string) (int, bool) {
	if fn.Type.Params == nil {
		return 0, false
	}
	index := 0
	for _, field := range fn.Type.Params.List {
		if len(field.Names) == 0 {
			index++
			continue
		}
		for _, fieldName := range field.Names {
			if fieldName.Name == name {
				return index, true
			}
			index++
		}
	}
	return 0, false
}

// capabilitySweepDocumentedExceptions lists capability strings that are
// deliberately absent from capabilityMatrix, with the reason, so the sweep
// does not misreport a documented design choice as a gap. Adding an entry here
// requires the same justification a reviewer would want in the source: why the
// route bypasses the matrix's BuildTruthEnvelope panic-guard.
var capabilitySweepDocumentedExceptions = map[string]string{
	"repository_freshness.status": "repository_freshness.go's repositoryFreshnessTruth builds its " +
		"TruthEnvelope directly from Postgres runtime state rather than through " +
		"capabilityMatrix/BuildTruthEnvelope, and says so in its doc comment; not a gap.",
}

// collectDecls records every single-value string const and every function
// declaration in file, keyed by file's own directory so later resolution can
// look identifiers and calls up scoped to the declaring package (see the
// capabilitySweep doc comment for why bare-name keys are unsafe now that this
// sweep walks recursively).
func (s *capabilitySweep) collectDecls(file *ast.File) {
	dir := s.dirOf(file.Package)
	if file.Name != nil {
		s.packageNames[dir] = file.Name.Name
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.CONST {
				continue
			}
			for _, spec := range d.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok || len(valueSpec.Names) != 1 || len(valueSpec.Values) != 1 {
					continue
				}
				if lit, ok := stringLiteral(valueSpec.Values[0]); ok {
					if s.constStrings[dir] == nil {
						s.constStrings[dir] = map[string]string{}
					}
					s.constStrings[dir][valueSpec.Names[0].Name] = lit
				}
			}
		case *ast.FuncDecl:
			if d.Body != nil {
				if s.funcDecls[dir] == nil {
					s.funcDecls[dir] = map[string]*ast.FuncDecl{}
				}
				s.funcDecls[dir][d.Name.Name] = d
			}
		}
	}
}

// findCallSites returns one failure message per WriteGraphReadError call site
// in file whose capability argument could not be resolved to only-known-good
// capabilityMatrix keys, plus the total count of WriteGraphReadError call
// sites matched in file (by callee name and 4-argument arity), regardless of
// whether resolution succeeded. The caller sums that count across every file
// so the test can distinguish "examined N call sites and found them clean"
// from "matched nothing."
func (s *capabilitySweep) findCallSites(file *ast.File) ([]string, int) {
	var findings []string
	matched := 0
	var funcStack []*ast.FuncDecl

	var visit func(ast.Node) bool
	visit = func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			funcStack = append(funcStack, node)
			ast.Inspect(node.Body, visit)
			funcStack = funcStack[:len(funcStack)-1]
			return false
		case *ast.CallExpr:
			// Match both the bare call (package query's own forwarder) and the
			// qualified one. #6060 moves the implementation into querycontract
			// and the handler families into subpackages, so a family calls
			// querycontract.WriteGraphReadError(...) -- a SelectorExpr. Matching
			// only *ast.Ident would let every one of those call sites skip the
			// capabilityMatrix check while this sweep still reported clean,
			// which is the regression this gate exists to catch.
			capIdx, swept := callsWriteGraphReadError(node.Fun)
			if !swept || len(node.Args) <= capIdx {
				break
			}
			var enclosing *ast.FuncDecl
			if len(funcStack) > 0 {
				enclosing = funcStack[len(funcStack)-1]
			}
			// The root forwarder is not a call site. It passes its own
			// capability parameter straight through to querycontract, so there
			// is no literal here to check against the matrix -- the callers of
			// the forwarder are the sites that carry one, and they are swept
			// separately. Counting it would report an unresolvable argument on
			// every run.
			if enclosing != nil && enclosing.Name != nil {
				if _, isForwarder := sweptCapabilityCallees[enclosing.Name.Name]; isForwarder {
					break
				}
			}
			matched++
			values, resolved := s.resolveCapabilityArg(node.Args[capIdx], enclosing, map[string]bool{})
			if !resolved {
				findings = append(findings, "unresolvable WriteGraphReadError capability argument at "+
					s.fset.Position(node.Pos()).String()+" (extend TestWriteGraphReadErrorCapabilitiesExistInMatrix's sweep)")
				break
			}
			for _, value := range values {
				if _, ok := capabilityMatrix[value]; ok {
					continue
				}
				if _, documented := capabilitySweepDocumentedExceptions[value]; documented {
					continue
				}
				findings = append(findings, "WriteGraphReadError capability "+value+
					" is not a capabilityMatrix key at "+s.fset.Position(node.Pos()).String())
			}
		}
		return true
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		visit(fn)
	}
	return findings, matched
}

// resolveCapabilityArg resolves expr to the set of string literals it can
// evaluate to. enclosing is the *ast.FuncDecl the expression appears in (for
// local-variable resolution); visitedFuncs guards against infinite recursion
// through function calls.
func (s *capabilitySweep) resolveCapabilityArg(expr ast.Expr, enclosing *ast.FuncDecl, visitedFuncs map[string]bool) ([]string, bool) {
	if lit, ok := stringLiteral(expr); ok {
		return []string{lit}, true
	}
	switch e := expr.(type) {
	case *ast.Ident:
		// e's own declaring/use directory, not enclosing's -- when
		// resolveParam recurses into a caller's argument, that argument
		// lexically lives in the caller's file, which can be a different
		// directory than the function whose parameter is being resolved.
		dir := s.dirOf(e.Pos())
		if lit, ok := s.constStrings[dir][e.Name]; ok {
			return []string{lit}, true
		}
		if enclosing == nil {
			return nil, false
		}
		return s.resolveLocalIdent(e.Name, enclosing, visitedFuncs)
	case *ast.CallExpr:
		callee, ok := e.Fun.(*ast.Ident)
		if !ok {
			return nil, false
		}
		// An unqualified call (bare *ast.Ident callee, not a package-selector)
		// can only reach a function declared in the same package as the call
		// site itself, so the callee's directory is the call expression's own
		// directory.
		return s.resolveFuncReturns(callee.Name, s.dirOf(e.Pos()), visitedFuncs)
	case *ast.SelectorExpr:
		// A package-qualified constant (advisory.AdvisoryEvidenceCapability
		// from a #6060 family leaf, passed by a root handler that can no
		// longer name the bare identifier). Resolved against the declaring
		// package's own directory, same scoping discipline as the Ident
		// case above.
		return s.resolveQualifiedConst(e)
	default:
		return nil, false
	}
}

// resolveQualifiedConst resolves pkg.Name to the string literal the named
// constant declares in the swept package named pkg. Every swept directory
// declaring that package name must agree on the value: unanimity keeps the
// directory-scoping guarantee (two same-named packages declaring different
// values fail closed as unresolvable, exactly like an unknown name, instead
// of resolving to whichever parsed last).
func (s *capabilitySweep) resolveQualifiedConst(e *ast.SelectorExpr) ([]string, bool) {
	pkgIdent, ok := e.X.(*ast.Ident)
	if !ok || e.Sel == nil {
		return nil, false
	}
	var values []string
	for dir, pkgName := range s.packageNames {
		if pkgName != pkgIdent.Name {
			continue
		}
		lit, ok := s.constStrings[dir][e.Sel.Name]
		if !ok {
			continue
		}
		values = append(values, lit)
	}
	if len(values) == 0 {
		return nil, false
	}
	for _, v := range values[1:] {
		if v != values[0] {
			return nil, false
		}
	}
	return []string{values[0]}, true
}

// resolveLocalIdent collects every literal (or further-resolvable) value
// assigned to name anywhere in enclosing's body, via both `:=` and `=`. When
// name is not locally assigned at all, it may be one of enclosing's own
// parameters (e.g. applyRepositorySelectorForCapability's capability
// parameter, forwarded straight into WriteGraphReadError); in that case
// resolution recurses into every call site of enclosing across the package,
// resolving whatever expression each caller passed for that parameter
// position.
func (s *capabilitySweep) resolveLocalIdent(name string, enclosing *ast.FuncDecl, visitedFuncs map[string]bool) ([]string, bool) {
	var values []string
	assigned := false
	ok := true
	ast.Inspect(enclosing.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range node.Lhs {
				lhsIdent, isIdent := lhs.(*ast.Ident)
				if !isIdent || lhsIdent.Name != name || i >= len(node.Rhs) {
					continue
				}
				assigned = true
				resolved, rOK := s.resolveCapabilityArg(node.Rhs[i], enclosing, visitedFuncs)
				if !rOK {
					ok = false
					continue
				}
				values = append(values, resolved...)
			}
		case *ast.GenDecl:
			// A local `const capability = "..."` inside the function body
			// (code_symbol.go's handleSymbolSearch pattern), not a
			// package-level const, so collectDecls never saw it.
			if node.Tok != token.CONST {
				break
			}
			for _, spec := range node.Specs {
				valueSpec, isValueSpec := spec.(*ast.ValueSpec)
				if !isValueSpec {
					continue
				}
				for i, ident := range valueSpec.Names {
					if ident.Name != name || i >= len(valueSpec.Values) {
						continue
					}
					assigned = true
					resolved, rOK := s.resolveCapabilityArg(valueSpec.Values[i], enclosing, visitedFuncs)
					if !rOK {
						ok = false
						continue
					}
					values = append(values, resolved...)
				}
			}
		}
		return true
	})
	if assigned {
		if len(values) == 0 {
			return nil, false
		}
		return values, ok
	}
	return s.resolveParam(name, enclosing, visitedFuncs)
}

// resolveParam handles name being a parameter of enclosing rather than a
// locally assigned variable: it resolves every call site of enclosing (by
// name, across every directory -- a leaf's callers legitimately live in
// another package, e.g. packagereg calling
// queryselector.ResolveForRequestWithAccess) at that parameter's argument
// position. Each caller's argument resolves against that caller's OWN
// directory, never enclosing's.
func (s *capabilitySweep) resolveParam(name string, enclosing *ast.FuncDecl, visitedFuncs map[string]bool) ([]string, bool) {
	index, isParam := paramIndex(enclosing, name)
	if !isParam {
		return nil, false
	}
	// funcKey is qualified by enclosing's own directory: two different
	// packages can declare a same-named function, and an unqualified key
	// would let resolving one wrongly block recursion into the other.
	funcKey := s.dirOf(enclosing.Pos()) + "\x00" + enclosing.Name.Name
	if visitedFuncs[funcKey] {
		return nil, false
	}
	visitedFuncs[funcKey] = true

	callers := s.callSites[enclosing.Name.Name]
	if len(callers) == 0 {
		return nil, false
	}
	var values []string
	allOK := true
	for _, caller := range callers {
		if index >= len(caller.call.Args) {
			allOK = false
			continue
		}
		resolved, rOK := s.resolveCapabilityArg(caller.call.Args[index], caller.enclosing, visitedFuncs)
		if !rOK {
			allOK = false
			continue
		}
		values = append(values, resolved...)
	}
	if len(values) == 0 {
		return nil, false
	}
	return values, allOK
}

// resolveFuncReturns collects every literal value a single-return-value,
// package-level function can return, by walking its body for return
// statements. It recurses at most one level deep per distinct (directory,
// function name) pair to stay terminating; dir scopes the funcDecls lookup to
// the package the call site actually resolves to (an unqualified call can
// only reach a function in its own package).
func (s *capabilitySweep) resolveFuncReturns(name string, dir string, visitedFuncs map[string]bool) ([]string, bool) {
	key := dir + "\x00" + name
	if visitedFuncs[key] {
		return nil, false
	}
	fn, ok := s.funcDecls[dir][name]
	if !ok || fn.Body == nil {
		return nil, false
	}
	visitedFuncs[key] = true

	var values []string
	allOK := true
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		ret, isReturn := n.(*ast.ReturnStmt)
		if !isReturn || len(ret.Results) != 1 {
			return true
		}
		resolved, rOK := s.resolveCapabilityArg(ret.Results[0], fn, visitedFuncs)
		if !rOK {
			allOK = false
			return true
		}
		values = append(values, resolved...)
		return true
	})
	if len(values) == 0 {
		return nil, false
	}
	return values, allOK
}

// stringLiteral returns the unquoted value of expr when it is a string
// BasicLit, else ("", false).
func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	unquoted, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return unquoted, true
}

// sweptCapabilityCallees maps each capability-taking function to the argument
// INDEX its capability sits at. GraphReadErrorEnvelope is swept alongside
// WriteGraphReadError because it takes the same capability and renders the same
// envelope -- it is the variant for seams that return the envelope instead of
// writing it -- so omitting it would let a caller escape the matrix check by
// picking the other function.
//
// The index travels with the name because the two do NOT share an arity:
// WriteGraphReadError(w, r, err, capability) versus
// GraphReadErrorEnvelope(err, capability). A single hard-coded position matched
// the envelope variant's NAME and then skipped every one of its call sites on
// the argument count, which a seeded-violation probe caught and a passing suite
// did not.
var sweptCapabilityCallees = map[string]int{
	"WriteGraphReadError":    3, // (w, r, err, capability)
	"GraphReadErrorEnvelope": 1, // (err, capability)
	"graphReadErrorEnvelope": 1, // root's unexported forwarder
}

// callsWriteGraphReadError reports whether fun calls one of the swept
// capability-taking functions, named directly or through a package qualifier.
func callsWriteGraphReadError(fun ast.Expr) (int, bool) {
	switch callee := fun.(type) {
	case *ast.Ident:
		idx, ok := sweptCapabilityCallees[callee.Name]
		return idx, ok
	case *ast.SelectorExpr:
		if callee.Sel == nil {
			return 0, false
		}
		idx, ok := sweptCapabilityCallees[callee.Sel.Name]
		return idx, ok
	default:
		return 0, false
	}
}
