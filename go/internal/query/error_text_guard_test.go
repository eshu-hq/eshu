// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/ast"
	"go/token"
	"slices"
	"strings"
	"testing"
)

// TestNoNewQueryErrorTextLeakSites is assertion A of the #7674 guard: no
// server-failure response body carries error text. Backend errors quote SQL,
// Cypher, hosts, and credentials, so a 5xx body built from err leaks them to
// every caller. The fix shape is tracing.WriteServerFailure (fixed message,
// 499 for a client cancel) after querycontract.WriteGraphReadError (fence and
// graph 503s). Assertion B, TestNoDirectServerStatusWrites, forbids the direct
// 500/502/504/408 writes themselves.
//
// It walks every non-test .go file under go/internal/query and fails on any
// leak site found by queryErrorTextLeakSites. There is no allowlist: the
// per-file ceiling ratchet that tracked the conversion was deleted when the
// last #7674 slice reached zero sites.
//
// Known gaps, not caught without type resolution: a message computed into a
// local first (`msg := err.Error()` then WriteError(w, 500, msg)); a helper
// that takes err itself and writes its text; a writer reached through a
// function value, an interface, or a function literal; and a local error whose
// name follows the sentinel convention (`errResp.Error()`), because
// `errFoo.Error()` and `pkg.ErrFoo.Error()` are treated as fixed sentinel
// text. TestQueryErrorTextLeakSitesShapes pins each gap as uncaught.
// Assertion B still fails the first gap when its status is a literal 500.
func TestNoNewQueryErrorTextLeakSites(t *testing.T) {
	t.Parallel()

	tree := loadQueryGuardTree(t, queryPackageDir(t), queryImportRoot)
	for _, finding := range queryErrorTextLeakFindings(tree) {
		t.Errorf("%s: %d error-text leak site(s); answer server failures with "+
			"tracing.WriteServerFailure and a fixed message (#7674):\n%s",
			finding.file, len(finding.sites), formatQueryErrorTextLeakSites(finding.sites))
	}
}

// queryGuardFinding is one file that holds sites for a guard assertion.
type queryGuardFinding struct {
	file  string
	sites []queryErrorTextLeakSite
}

// queryErrorTextLeakFindings is assertion A over tree: every file with an
// error-text leak site.
func queryErrorTextLeakFindings(tree *queryGuardTree) []queryGuardFinding {
	var findings []queryGuardFinding
	for _, f := range tree.files {
		if sites := queryErrorTextLeakSites(tree, f); len(sites) > 0 {
			findings = append(findings, queryGuardFinding{file: f.rel, sites: sites})
		}
	}
	return findings
}

// queryErrorTextLeakSite is one site found by queryErrorTextLeakSites or
// queryDirectServerStatusSites.
type queryErrorTextLeakSite struct {
	pos  token.Position
	kind string
}

func formatQueryErrorTextLeakSites(sites []queryErrorTextLeakSite) string {
	lines := make([]string, 0, len(sites))
	for _, site := range sites {
		lines = append(lines, "  "+site.pos.String()+": "+site.kind)
	}
	return strings.Join(lines, "\n")
}

// queryErrorTextLeakSites returns every error-text leak site in f:
//
//   - a call to a message writer whose message argument carries error text
//     and whose body the status makes a server failure. A message writer is a
//     base sink with a message (WriteError, WriteContractError,
//     writeContractError, and WriteJSON's body under any qualifier or
//     receiver, and http.Error), or a function whose never-assigned parameter
//     reaches a message writer's message, directly or through locals
//     (queryGuardTree). The status makes it a server failure unless it is a
//     literal in [100, 499]: the call's own status argument for a forwarder,
//     or the status the writer passes inside;
//   - the Message field of an ErrorEnvelope or pkg.ErrorEnvelope composite
//     literal that carries error text, unless the nearest enclosing call or
//     return statement has a literal 4xx status operand, or the literal sits
//     inside a call already reported.
//
// A message carries error text when it contains a no-argument .Error() call
// on anything other than a sentinel-named receiver (errFoo, ErrFoo,
// pkg.ErrFoo), or a fmt.Sprintf, fmt.Sprint, fmt.Sprintln, or fmt.Errorf call
// whose arguments mention an identifier that is err or ends in err or Err.
func queryErrorTextLeakSites(tree *queryGuardTree, f *queryGuardFile) []queryErrorTextLeakSite {
	var sites []queryErrorTextLeakSite
	var stack []ast.Node
	var reported []ast.Node
	ast.Inspect(f.file, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		switch n := node.(type) {
		case *ast.CallExpr:
			if kind, leaks := queryErrorTextWriterLeak(tree, f, n); leaks {
				sites = append(sites, queryErrorTextLeakSite{pos: tree.fset.Position(n.Pos()), kind: kind})
				reported = append(reported, n)
			}
		case *ast.CompositeLit:
			if message := queryErrorEnvelopeMessage(n); message != nil &&
				queryErrorTextCarriesErr(message) && !queryErrorTextClientContext(stack) &&
				!slices.ContainsFunc(reported, func(call ast.Node) bool { return call.Pos() <= n.Pos() && n.End() <= call.End() }) {
				sites = append(sites, queryErrorTextLeakSite{pos: tree.fset.Position(message.Pos()), kind: "ErrorEnvelope.Message"})
			}
		}
		stack = append(stack, node)
		return true
	})
	return sites
}

// queryErrorTextWriterLeak reports whether call is a message writer whose
// message carries error text behind a server-failure status, and names the
// writer chain.
func queryErrorTextWriterLeak(tree *queryGuardTree, f *queryGuardFile, call *ast.CallExpr) (string, bool) {
	spec, name, ok := tree.resolve(f, call)
	if !ok || !queryCarriesErrAt(call.Args, spec.messages) {
		return "", false
	}
	switch spec.messageMode {
	case queryMessageClient, queryMessageNone:
		return "", false
	case queryMessageStatusArg:
		if spec.status < 0 || spec.status >= len(call.Args) || queryIsNonFailureStatus(call.Args[spec.status]) {
			return "", false
		}
	}
	if spec.chain != "" {
		name += " -> " + spec.chain
	}
	return name, true
}

// queryErrorEnvelopeMessage returns the Message value of an ErrorEnvelope
// composite literal, or nil when lit is not one or sets no Message.
func queryErrorEnvelopeMessage(lit *ast.CompositeLit) ast.Expr {
	switch typ := lit.Type.(type) {
	case *ast.Ident:
		if typ.Name != "ErrorEnvelope" {
			return nil
		}
	case *ast.SelectorExpr:
		if typ.Sel.Name != "ErrorEnvelope" {
			return nil
		}
	default:
		return nil
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Message" {
			return kv.Value
		}
	}
	return nil
}

// queryErrorTextClientContext reports whether the nearest call or return
// statement enclosing an ErrorEnvelope literal (the top of stack is its
// parent) has a literal 4xx operand, as in WriteErrorEnvelope(w, r,
// http.StatusBadRequest, &ErrorEnvelope{...}) or return http.StatusConflict,
// &ErrorEnvelope{...}. Any other statement ends the search.
func queryErrorTextClientContext(stack []ast.Node) bool {
	for i := len(stack) - 1; i >= 0; i-- {
		switch parent := stack[i].(type) {
		case *ast.CallExpr:
			return slices.ContainsFunc(parent.Args, queryErrorTextIsClientStatus)
		case *ast.ReturnStmt:
			return slices.ContainsFunc(parent.Results, queryErrorTextIsClientStatus)
		case ast.Stmt, ast.Decl:
			return false
		}
	}
	return false
}

// queryErrorTextCarriesErr reports whether expr carries error text; see
// queryErrorTextLeakSites for the shapes.
func queryErrorTextCarriesErr(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || found {
			return !found
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "Error" && len(call.Args) == 0 && !queryErrorTextIsSentinel(sel.X) {
			found = true
			return false
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "fmt" &&
			slices.Contains([]string{"Sprintf", "Sprint", "Sprintln", "Errorf"}, sel.Sel.Name) &&
			slices.ContainsFunc(call.Args, queryErrorTextMentionsErr) {
			found = true
			return false
		}
		return true
	})
	return found
}

// queryErrorTextMentionsErr reports whether expr mentions an identifier named
// err or ending in err or Err (rerr, configErr).
func queryErrorTextMentionsErr(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok &&
			(ident.Name == "err" || strings.HasSuffix(ident.Name, "err") || strings.HasSuffix(ident.Name, "Err")) {
			found = true
		}
		return !found
	})
	return found
}

// queryErrorTextIsSentinel reports whether receiver follows the Go sentinel
// error naming convention: errFoo, ErrFoo, or pkg.ErrFoo. Its .Error() is
// fixed text.
func queryErrorTextIsSentinel(receiver ast.Expr) bool {
	var name string
	switch r := receiver.(type) {
	case *ast.Ident:
		name = r.Name
	case *ast.SelectorExpr:
		name = r.Sel.Name
	default:
		return false
	}
	if len(name) < 4 || (!strings.HasPrefix(name, "err") && !strings.HasPrefix(name, "Err")) {
		return false
	}
	next := name[3]
	return (next >= 'A' && next <= 'Z') || (next >= '0' && next <= '9') || next == '_'
}

// queryErrorTextIsClientStatus reports whether expr is a literal 4xx status:
// an int literal in [400, 499] or http.StatusXxx with a 4xx value.
func queryErrorTextIsClientStatus(expr ast.Expr) bool {
	value, ok := queryStatusLiteral(expr)
	return ok && value >= 400 && value <= 499
}

// queryCarriesErrAt reports whether any argument whose bit is set in messages
// carries error text.
func queryCarriesErrAt(args []ast.Expr, messages uint64) bool {
	for i, arg := range args {
		if i < 64 && messages&(1<<i) != 0 && queryErrorTextCarriesErr(arg) {
			return true
		}
	}
	return false
}
