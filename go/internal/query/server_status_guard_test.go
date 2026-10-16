// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/ast"
	"go/token"
	"net/http"
	"strings"
	"testing"
)

// TestNoDirectServerStatusWrites is assertion B of the #7674 guard. A server
// failure answers through tracing.WriteServerFailure, which records the failure
// on the span and turns a client cancel into 499; a direct 500 write skips
// both. So outside go/internal/query/tracing a guarded writer call
// (WriteError, WriteContractError, or writeContractError under any qualifier or
// receiver, or http.Error) may pass only one of these status operands:
//
//   - a literal in queryAllowedDirectStatus: a 4xx other than 408, 501, or 503,
//     as an int literal or a net/http Status constant;
//   - an identifier that is a parameter of the nearest enclosing function and
//     is never assigned in that function: a pure forwarder such as
//     querycontract.WriteErrorEnvelope, whose own callers are guarded here.
//
// Any other status fails: a literal 500, 502, 504, or 408, a local variable,
// a named constant, or a call. queryDirectServerStatusSites documents the
// shapes; TestQueryDirectServerStatusSitesShapes pins them.
//
// The walk covers every non-test .go file under go/internal/query, the same
// root as TestNoNewQueryErrorTextLeakSites, skipping testdata and dot
// directories. It does not resolve types, so a differently named wrapper, and
// the writers outside the guarded set (WriteErrorEnvelope, WriteJSON,
// WriteHeader), are not seen; the shapes test pins those gaps.
// TestQueryErrorGuardsSeeded is the seeded RED/GREEN pair over a planted tree.
func TestNoDirectServerStatusWrites(t *testing.T) {
	t.Parallel()

	for _, finding := range queryDirectServerStatusFindings(t, queryPackageDir(t)) {
		t.Errorf("%s: %d direct server-status write(s); answer a server failure with "+
			"tracing.WriteServerFailure, or a configuration verdict with a literal 503 (#7674):\n%s",
			finding.file, len(finding.sites), formatQueryErrorTextLeakSites(finding.sites))
	}
}

// queryDirectServerStatusFindings is assertion B over root: every non-test
// file outside the exempt tracing package with a direct server-status write,
// keyed by path relative to root.
func queryDirectServerStatusFindings(t *testing.T, root string) []queryGuardFinding {
	t.Helper()

	var findings []queryGuardFinding
	walkQueryNonTestFiles(t, root, func(rel string, fset *token.FileSet, file *ast.File) {
		if queryDirectServerStatusExempt(rel) {
			return
		}
		if sites := queryDirectServerStatusSites(fset, file); len(sites) > 0 {
			findings = append(findings, queryGuardFinding{file: rel, sites: sites})
		}
	})
	return findings
}

// queryDirectServerStatusExempt reports whether rel, a slash path relative to
// go/internal/query, is in the tracing package: the server-failure helpers
// there are the one place a computed 5xx or 499 may be written.
func queryDirectServerStatusExempt(rel string) bool {
	return strings.HasPrefix(rel, "tracing/")
}

// queryAllowedDirectStatus reports whether a literal status may be written
// directly: a 4xx other than 408 Request Timeout (a server-side deadline that
// WriteServerFailure owns), 501 Not Implemented, or 503 Service Unavailable.
func queryAllowedDirectStatus(status int) bool {
	switch {
	case status == http.StatusRequestTimeout:
		return false
	case status >= 400 && status <= 499:
		return true
	default:
		return status == http.StatusNotImplemented || status == http.StatusServiceUnavailable
	}
}

// queryDirectServerStatusSites returns every guarded writer call in file whose
// status operand is neither an allowed literal (queryAllowedDirectStatus) nor a
// pure-forwarded parameter of the nearest enclosing function. A literal is an
// int literal or a net/http Status constant. A parameter is pure-forwarded when
// no assignment in that function, := included, names it on the left.
func queryDirectServerStatusSites(fset *token.FileSet, file *ast.File) []queryErrorTextLeakSite {
	var sites []queryErrorTextLeakSite
	var stack []ast.Node
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if call, ok := node.(*ast.CallExpr); ok {
			if name, status, ok := queryGuardedWriterStatus(call); ok && !queryDirectStatusAllowed(status, stack) {
				sites = append(sites, queryErrorTextLeakSite{
					pos:  fset.Position(call.Pos()),
					kind: name + " status " + queryExprString(status),
				})
			}
		}
		stack = append(stack, node)
		return true
	})
	return sites
}

// queryGuardedWriterStatus returns the writer name and status operand of call
// when call is a guarded writer with enough arguments.
func queryGuardedWriterStatus(call *ast.CallExpr) (string, ast.Expr, bool) {
	name, args, ok := queryGuardedWriter(call)
	if !ok || len(call.Args) <= args.status {
		return "", nil, false
	}
	return name, call.Args[args.status], true
}

// queryDirectStatusAllowed reports whether status is an allowed literal or a
// pure-forwarded parameter of the innermost function on stack.
func queryDirectStatusAllowed(status ast.Expr, stack []ast.Node) bool {
	if value, ok := queryStatusLiteral(status); ok {
		return queryAllowedDirectStatus(value)
	}
	ident, ok := status.(*ast.Ident)
	if !ok {
		return false
	}
	for i := len(stack) - 1; i >= 0; i-- {
		switch fn := stack[i].(type) {
		case *ast.FuncDecl:
			return queryIsParam(fn.Type, ident.Name) && !queryAssigns(fn.Body, ident.Name)
		case *ast.FuncLit:
			return queryIsParam(fn.Type, ident.Name) && !queryAssigns(fn.Body, ident.Name)
		}
	}
	return false
}

// queryIsParam reports whether name is a parameter of a function type.
func queryIsParam(typ *ast.FuncType, name string) bool {
	if typ == nil || typ.Params == nil {
		return false
	}
	for _, field := range typ.Params.List {
		for _, param := range field.Names {
			if param.Name == name {
				return true
			}
		}
	}
	return false
}

// queryAssigns reports whether any assignment or increment in body, nested
// function literals included, names name on its left-hand side.
func queryAssigns(body *ast.BlockStmt, name string) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				if ident, ok := lhs.(*ast.Ident); ok && ident.Name == name {
					found = true
				}
			}
		case *ast.IncDecStmt:
			if ident, ok := n.X.(*ast.Ident); ok && ident.Name == name {
				found = true
			}
		case *ast.ValueSpec:
			for _, ident := range n.Names {
				if ident.Name == name {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// queryStatusLiteral returns the value of an int literal or a net/http Status
// constant.
func queryStatusLiteral(expr ast.Expr) (int, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return queryIntLiteral(e)
	case *ast.SelectorExpr:
		pkg, ok := e.X.(*ast.Ident)
		if !ok || pkg.Name != "http" {
			return 0, false
		}
		value, ok := queryErrorTextHTTPStatusValues[e.Sel.Name]
		return value, ok
	default:
		return 0, false
	}
}

// queryExprString renders a status operand for a failure message.
func queryExprString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return e.Value
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return queryExprString(e.X) + "." + e.Sel.Name
	case *ast.CallExpr:
		return queryExprString(e.Fun) + "(...)"
	default:
		return "<expr>"
	}
}
