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
// both. So outside go/internal/query/tracing, a call to a status writer may
// pass only these status operands:
//
//   - a literal in queryAllowedDirectStatus: a 4xx other than 408, 501, or
//     503, plus 1xx-3xx, which is a success or redirect and never a failure.
//   - a parameter of the nearest enclosing function that the function never
//     assigns: a pure forwarder, whose own callers are checked instead.
//   - a local whose every assignment is an allowed literal or position i of
//     a call whose result i is an allowed status (queryVerdictStatusSources,
//     or a status returner). Rationale: `status := http.StatusCreated; if
//     unchanged { status = http.StatusOK }` cannot write a failure, and
//     `status, env := tracing.ServerFailureEnvelope(...)` already carries the
//     499 and fence rules. A local with one disallowed literal, a call that is
//     not a returner, an increment, or no assignment fails.
//   - at the call site of a writer derived from the tree (not a base sink),
//     the literal 0. Rationale: issueBrowserSessionWithExternalAuth takes 0 as
//     "write no status" and writes only when status > 0. A base sink given 0
//     fails: net/http panics on WriteHeader(0).
//
// A status returner is a function with an int result k that, on every
// return, yields an allowed literal, an allowed local, or the result k of
// another returner or verdict source (entity's BuildServiceStoryEnvelope).
// Rationale: such a seam returns only statuses the rules already allow; one
// return of any other status makes it not a returner.
//
// A status writer is a base sink (WriteError, WriteContractError,
// writeContractError, WriteJSON, WriteErrorEnvelope, WriteSuccess, and
// WriteHeader under any qualifier or receiver, and http.Error), or a function
// that passes a parameter unchanged as a status writer's status. The writers
// are a fixed point over every package under go/internal/query, so a renamed
// wrapper chain (compare's writeCompareError -> writeCompareJSON ->
// WriteHeader) is followed at any depth, across packages for an exported
// wrapper. Writers are keyed by package, receiver type, and name, so two
// types' same-named methods stay apart; a method call resolves through its
// receiver's evident type (the method receiver, a typed parameter or local,
// or a field of a struct of known type), else through the union of the
// package's same-named methods, and the finding then says so. A call to a
// function that itself writes a disallowed literal status, at any depth,
// also fails, so each caller of a fixed-500 helper is named.
//
// Any other status fails: a literal 500, 502, 504, or 408, a named constant,
// a field, or a call. No struct field is ever allowed. The walk does not
// resolve types, so a writer reached through a function value, an interface,
// a function literal, or a method promoted from an embedded field is not
// seen.
// TestQueryDirectServerStatusSitesShapes, TestQueryWriterWrapperShapes, and
// TestQueryWriterReceiverShapes pin
// the shapes and the gaps; TestQueryErrorGuardsSeeded is the seeded RED/GREEN
// pair over a planted tree.
func TestNoDirectServerStatusWrites(t *testing.T) {
	t.Parallel()

	tree := loadQueryGuardTree(t, queryPackageDir(t), queryImportRoot)
	for _, finding := range queryDirectServerStatusFindings(tree) {
		t.Errorf("%s: %d direct server-status write(s); answer a server failure with "+
			"tracing.WriteServerFailure, or a configuration verdict with a literal 503 (#7674):\n%s",
			finding.file, len(finding.sites), formatQueryErrorTextLeakSites(finding.sites))
	}
}

// queryDirectServerStatusFindings is assertion B over tree: every file
// outside the exempt tracing package with a direct server-status write.
func queryDirectServerStatusFindings(tree *queryGuardTree) []queryGuardFinding {
	var findings []queryGuardFinding
	for _, f := range tree.files {
		if queryDirectServerStatusExempt(f.rel) {
			continue
		}
		if sites := queryDirectServerStatusSites(tree, f); len(sites) > 0 {
			findings = append(findings, queryGuardFinding{file: f.rel, sites: sites})
		}
	}
	return findings
}

// queryDirectServerStatusExempt reports whether rel, a slash path relative to
// go/internal/query, is in the tracing package: the server-failure helpers
// there are the one place a computed 5xx or 499 may be written.
func queryDirectServerStatusExempt(rel string) bool {
	return strings.HasPrefix(rel, "tracing/")
}

// queryAllowedDirectStatus reports whether a literal status may be written
// directly: 1xx-3xx (not a failure), a 4xx other than 408 Request Timeout (a
// server-side deadline that WriteServerFailure owns), 501 Not Implemented, or
// 503 Service Unavailable.
func queryAllowedDirectStatus(status int) bool {
	switch {
	case status == http.StatusRequestTimeout:
		return false
	case status >= 100 && status <= 499:
		return true
	default:
		return status == http.StatusNotImplemented || status == http.StatusServiceUnavailable
	}
}

// queryDirectServerStatusSites returns every status-writer call in f whose
// status operand is not allowed (queryDirectStatusAllowed), and every call to
// a writer that itself writes a disallowed literal status.
func queryDirectServerStatusSites(tree *queryGuardTree, f *queryGuardFile) []queryErrorTextLeakSite {
	var sites []queryErrorTextLeakSite
	var stack []ast.Node
	ast.Inspect(f.file, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if call, ok := node.(*ast.CallExpr); ok {
			sites = append(sites, queryDirectServerStatusCallSites(tree, f, call, stack)...)
		}
		stack = append(stack, node)
		return true
	})
	return sites
}

// queryDirectServerStatusCallSites checks one call; see
// queryDirectServerStatusSites.
func queryDirectServerStatusCallSites(
	tree *queryGuardTree, f *queryGuardFile, call *ast.CallExpr, stack []ast.Node,
) []queryErrorTextLeakSite {
	spec, name, ok := tree.resolve(f, call)
	if !ok {
		return nil
	}
	pos := tree.fset.Position(call.Pos())
	var sites []queryErrorTextLeakSite
	if spec.status >= 0 && spec.status < len(call.Args) {
		status := call.Args[spec.status]
		if !queryIsZeroSentinel(spec, status) && !tree.statusAllowed(f, status, stack) {
			chain := name
			if spec.chain != "" {
				chain = name + " -> " + spec.chain
			}
			sites = append(sites, queryErrorTextLeakSite{pos: pos, kind: chain + " status " + queryExprString(status)})
		}
	}
	if spec.fixed != "" {
		sites = append(sites, queryErrorTextLeakSite{
			pos: pos, kind: name + " -> " + spec.fixedChain + " writes status " + spec.fixed,
		})
	}
	return sites
}

// statusAllowed reports whether status is an allowed literal, a
// pure-forwarded parameter of the innermost function on stack, or a local of
// that function whose every assignment is allowed (queryLocalStatusAllowed).
func (tree *queryGuardTree) statusAllowed(f *queryGuardFile, status ast.Expr, stack []ast.Node) bool {
	if value, ok := queryStatusLiteral(status); ok {
		return queryAllowedDirectStatus(value)
	}
	ident, ok := status.(*ast.Ident)
	if !ok {
		return false
	}
	for i := len(stack) - 1; i >= 0; i-- {
		var typ *ast.FuncType
		var body *ast.BlockStmt
		switch fn := stack[i].(type) {
		case *ast.FuncDecl:
			typ, body = fn.Type, fn.Body
		case *ast.FuncLit:
			typ, body = fn.Type, fn.Body
		default:
			continue
		}
		if queryIsParam(typ, ident.Name) {
			return !queryAssigns(body, ident.Name)
		}
		return tree.localStatusAllowed(f, body, ident.Name)
	}
	return false
}

// queryIsZeroSentinel reports whether status is the int literal 0 passed to a
// writer derived from the tree. Such a writer may take 0 as "write no status"
// (issueBrowserSessionWithExternalAuth writes only when status > 0); net/http
// itself panics on WriteHeader(0), so a base sink never takes it.
func queryIsZeroSentinel(spec queryWriterSpec, status ast.Expr) bool {
	lit, ok := status.(*ast.BasicLit)
	return spec.fromTree && ok && lit.Kind == token.INT && lit.Value == "0"
}
