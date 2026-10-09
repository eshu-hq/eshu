// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestNoNewQueryErrorTextLeakSites is the #7674 ratchet against error text
// in server-failure response bodies. Backend errors quote SQL, Cypher, hosts,
// and credentials, so a 5xx body built from err leaks them to every caller.
// The fix shape is tracing.WriteServerFailure (fixed message, 499 for a client
// cancel) after querycontract.WriteGraphReadError (fence and graph 503s).
//
// It walks every non-test .go file under go/internal/query and counts leak
// sites with queryErrorTextLeakSites: a call to WriteError, WriteContractError,
// writeContractError (any qualifier or receiver), or http.Error whose status
// is not a literal 4xx and whose message carries error text, plus the Message
// field of an ErrorEnvelope literal that carries error text outside a literal
// 4xx context. A variable or computed status counts as a server status,
// because most such statuses can be 5xx. queryErrorTextLeakSites documents the
// exact shapes.
//
// The ratchet: queryErrorTextLeakCeilings pins each file's count. A file over
// its ceiling, or an unlisted file with any site, fails. A file under its
// ceiling, or a listed file with no sites or no longer present, only logs a
// "tighten ceiling" note. Stale entries never fail because other PRs remove
// sites in these packages concurrently; a hard stale failure would force each
// of them to edit this allowlist. Each #7674 slice sets exact ceilings for the
// packages it converts, and review enforces that.
//
// Known gaps, not caught without type resolution: a message computed into a
// local first (`msg := err.Error()` then WriteError(w, 500, msg)); a wrapper
// helper that takes err and writes it; and a local error whose name follows
// the sentinel convention (`errResp.Error()`), because `errFoo.Error()` and
// `pkg.ErrFoo.Error()` are treated as fixed sentinel text.
// TestQueryErrorTextLeakSitesShapes pins each gap as uncaught.
//
// The final #7674 slice deletes queryErrorTextLeakCeilings and asserts zero
// sites everywhere.
func TestNoNewQueryErrorTextLeakSites(t *testing.T) {
	t.Parallel()

	root := queryPackageDir(t)
	counts := map[string][]queryErrorTextLeakSite{}
	fset := token.NewFileSet()
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || hasTestSuffix(path) {
			return nil
		}
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		file, parseErr := parser.ParseFile(fset, path, contents, 0)
		if parseErr != nil {
			t.Errorf("parse %s: %v", path, parseErr)
			return nil
		}
		if sites := queryErrorTextLeakSites(fset, file); len(sites) > 0 {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			counts[filepath.ToSlash(rel)] = sites
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", root, walkErr)
	}

	files := make([]string, 0, len(counts))
	for file := range counts {
		files = append(files, file)
	}
	sort.Strings(files)
	for _, file := range files {
		sites := counts[file]
		ceiling, listed := queryErrorTextLeakCeilings[file]
		switch {
		case !listed:
			t.Errorf("%s: %d error-text leak site(s) in a file with no ceiling; "+
				"answer server failures with tracing.WriteServerFailure and a fixed "+
				"message (#7674):\n%s", file, len(sites), formatQueryErrorTextLeakSites(sites))
		case len(sites) > ceiling:
			t.Errorf("%s: %d error-text leak site(s), ceiling %d; a new site was "+
				"added. Answer server failures with tracing.WriteServerFailure and a "+
				"fixed message (#7674):\n%s", file, len(sites), ceiling, formatQueryErrorTextLeakSites(sites))
		case len(sites) < ceiling:
			t.Logf("%s: %d error-text leak site(s), ceiling %d; tighten ceiling", file, len(sites), ceiling)
		}
	}
	for file, ceiling := range queryErrorTextLeakCeilings {
		if _, found := counts[file]; !found && ceiling > 0 {
			t.Logf("%s: no error-text leak sites, ceiling %d; tighten ceiling (remove the entry)", file, ceiling)
		}
	}
}

// queryErrorTextLeakSite is one leak site found by queryErrorTextLeakSites.
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

// queryErrorTextWriterArgs maps each guarded writer name to the index of its
// status and message arguments.
var queryErrorTextWriterArgs = map[string]struct{ status, message int }{
	"WriteError":         {status: 1, message: 2},
	"WriteContractError": {status: 2, message: 3},
	"writeContractError": {status: 2, message: 3},
}

// queryErrorTextLeakSites returns every error-text leak site in file:
//
//   - a call to WriteError(w, status, message), WriteContractError(w, r,
//     status, message, ...), or writeContractError(w, r, status, message, ...)
//     under any qualifier or receiver (querycontract.WriteError,
//     a.deps.WriteError, h.writeContractError), or http.Error(w, message,
//     status), whose status is not a literal 4xx and whose message carries
//     error text;
//   - the Message field of an ErrorEnvelope or pkg.ErrorEnvelope composite
//     literal that carries error text, unless the nearest enclosing call or
//     return statement has a literal 4xx status operand.
//
// A message carries error text when it contains a no-argument .Error() call
// on anything other than a sentinel-named receiver (errFoo, ErrFoo,
// pkg.ErrFoo), or a fmt.Sprintf, fmt.Sprint, fmt.Sprintln, or fmt.Errorf call
// whose arguments mention an identifier that is err or ends in err or Err.
// A literal 4xx is an int literal in [400, 499] or a net/http Status constant
// in that range.
func queryErrorTextLeakSites(fset *token.FileSet, file *ast.File) []queryErrorTextLeakSite {
	var sites []queryErrorTextLeakSite
	var stack []ast.Node
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		switch n := node.(type) {
		case *ast.CallExpr:
			if kind, leaks := queryErrorTextWriterLeak(n); leaks {
				sites = append(sites, queryErrorTextLeakSite{pos: fset.Position(n.Pos()), kind: kind})
			}
		case *ast.CompositeLit:
			if message := queryErrorEnvelopeMessage(n); message != nil &&
				queryErrorTextCarriesErr(message) && !queryErrorTextClientContext(stack) {
				sites = append(sites, queryErrorTextLeakSite{pos: fset.Position(message.Pos()), kind: "ErrorEnvelope.Message"})
			}
		}
		stack = append(stack, node)
		return true
	})
	return sites
}

// queryErrorTextWriterLeak reports whether call is a guarded writer with a
// server status and an error-text message, and names the writer.
func queryErrorTextWriterLeak(call *ast.CallExpr) (string, bool) {
	var name string
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		name = fun.Name
	case *ast.SelectorExpr:
		name = fun.Sel.Name
		if name == "Error" {
			pkg, ok := fun.X.(*ast.Ident)
			if !ok || pkg.Name != "http" {
				return "", false
			}
			name = "http.Error"
		}
	default:
		return "", false
	}
	args, ok := queryErrorTextWriterArgs[name]
	if name == "http.Error" {
		args, ok = struct{ status, message int }{status: 2, message: 1}, true
	}
	if !ok || len(call.Args) <= max(args.status, args.message) {
		return "", false
	}
	if queryErrorTextIsClientStatus(call.Args[args.status]) || !queryErrorTextCarriesErr(call.Args[args.message]) {
		return "", false
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
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.INT {
			return false
		}
		value, err := strconv.Atoi(e.Value)
		return err == nil && value >= 400 && value <= 499
	case *ast.SelectorExpr:
		pkg, ok := e.X.(*ast.Ident)
		if !ok || pkg.Name != "http" {
			return false
		}
		value, ok := queryErrorTextHTTPStatusValues[e.Sel.Name]
		return ok && value >= 400 && value <= 499
	default:
		return false
	}
}

// queryErrorTextHTTPStatusValues maps net/http status constant names to their
// values, so the 4xx test reads the real constants rather than a name list.
var queryErrorTextHTTPStatusValues = map[string]int{
	"StatusBadRequest":                   http.StatusBadRequest,
	"StatusUnauthorized":                 http.StatusUnauthorized,
	"StatusPaymentRequired":              http.StatusPaymentRequired,
	"StatusForbidden":                    http.StatusForbidden,
	"StatusNotFound":                     http.StatusNotFound,
	"StatusMethodNotAllowed":             http.StatusMethodNotAllowed,
	"StatusNotAcceptable":                http.StatusNotAcceptable,
	"StatusProxyAuthRequired":            http.StatusProxyAuthRequired,
	"StatusRequestTimeout":               http.StatusRequestTimeout,
	"StatusConflict":                     http.StatusConflict,
	"StatusGone":                         http.StatusGone,
	"StatusLengthRequired":               http.StatusLengthRequired,
	"StatusPreconditionFailed":           http.StatusPreconditionFailed,
	"StatusRequestEntityTooLarge":        http.StatusRequestEntityTooLarge,
	"StatusRequestURITooLong":            http.StatusRequestURITooLong,
	"StatusUnsupportedMediaType":         http.StatusUnsupportedMediaType,
	"StatusRequestedRangeNotSatisfiable": http.StatusRequestedRangeNotSatisfiable,
	"StatusExpectationFailed":            http.StatusExpectationFailed,
	"StatusTeapot":                       http.StatusTeapot,
	"StatusMisdirectedRequest":           http.StatusMisdirectedRequest,
	"StatusUnprocessableEntity":          http.StatusUnprocessableEntity,
	"StatusLocked":                       http.StatusLocked,
	"StatusFailedDependency":             http.StatusFailedDependency,
	"StatusTooEarly":                     http.StatusTooEarly,
	"StatusUpgradeRequired":              http.StatusUpgradeRequired,
	"StatusPreconditionRequired":         http.StatusPreconditionRequired,
	"StatusTooManyRequests":              http.StatusTooManyRequests,
	"StatusRequestHeaderFieldsTooLarge":  http.StatusRequestHeaderFieldsTooLarge,
	"StatusUnavailableForLegalReasons":   http.StatusUnavailableForLegalReasons,
	"StatusInternalServerError":          http.StatusInternalServerError,
	"StatusServiceUnavailable":           http.StatusServiceUnavailable,
	"StatusGatewayTimeout":               http.StatusGatewayTimeout,
	"StatusBadGateway":                   http.StatusBadGateway,
	"StatusNotImplemented":               http.StatusNotImplemented,
}
