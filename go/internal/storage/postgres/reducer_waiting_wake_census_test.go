// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// wakeCensusClass is how one generation_active or producer-terminal wake
// call site avoids the #11228 window: a wake inside an open provider
// transaction cannot see a wait row that commits inside that window, so it
// needs a post-commit re-wake or a written reason it cannot occur.
type wakeCensusClass string

const (
	// wakeCensusForwarder passes its own database parameter to another wake
	// helper; its callers are censused instead.
	wakeCensusForwarder wakeCensusClass = "forwarder"
	// wakeCensusInTxRewoken wakes on an open transaction and re-runs the
	// same wake after the commit through the named re-wake function.
	wakeCensusInTxRewoken wakeCensusClass = "in_tx_rewoken"
	// wakeCensusPostCommit wakes on the pool after the provider's own
	// statement committed (autocommit), so the provider write is visible to
	// every park re-check that starts after it.
	wakeCensusPostCommit wakeCensusClass = "post_commit"
	// wakeCensusParker is a parker waking its own key after its re-check
	// saw the provider: not a provider site.
	wakeCensusParker wakeCensusClass = "parker"
	// wakeCensusAudit is the slow audit, a fallback that reads committed
	// state on the pool.
	wakeCensusAudit wakeCensusClass = "audit"
)

// wakeCensusEntry registers one call site, keyed "file:function".
type wakeCensusEntry struct {
	class wakeCensusClass
	// rewake names the post-commit re-wake an in_tx_rewoken site must call
	// after its Commit.
	rewake string
	// reason is the written reason the window cannot occur.
	reason string
}

// waitingWakeHelpers are the functions that issue or forward a wake of the
// generation_active or producer_domain_terminal kinds (#11228 scope; the
// other kinds are tracked by #11250).
var waitingWakeHelpers = map[string]bool{
	"wakeWaitingByKey":                            true,
	"wakeGenerationActiveWaits":                   true,
	"wakeGenerationActiveWaitsBestEffort":         true,
	"wakeTerminalGenerationActiveWaits":           true,
	"wakeTerminalGenerationActiveWaitsBestEffort": true,
	"wakeAckActivationWaits":                      true,
	"wakeProducerTerminalAfterAck":                true,
	"wakeProducerTerminalAfterAckWithPlan":        true,
	"wakeProducerTerminalCrossScope":              true,
}

// wakeKindsOutOfScope are wakeWaitingByKey kind constants this census does
// not cover: their providers are a different Dekker shape tracked by #11250.
var wakeKindsOutOfScope = map[string]bool{
	"WaitingKindPhaseCommitted":               true,
	"WaitingKindRelationshipGenerationActive": true,
}

// wakeCensusSite is one call of a wake helper found in the source.
type wakeCensusSite struct {
	key      string // file:function
	callee   string
	dbArg    string
	inTx     bool
	callPos  token.Pos
	function *ast.FuncDecl
}

// collectWakeCensusSites returns every in-scope wake helper call in files.
func collectWakeCensusSites(files map[string]*ast.File) []wakeCensusSite {
	var sites []wakeCensusSite
	for name, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			txNames := transactionIdents(fn)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				callee := calleeName(call)
				if !waitingWakeHelpers[callee] || len(call.Args) < 2 {
					return true
				}
				if callee == "wakeWaitingByKey" && len(call.Args) >= 5 {
					if kind, ok := call.Args[4].(*ast.Ident); ok && wakeKindsOutOfScope[kind.Name] {
						return true
					}
				}
				dbArg := exprText(call.Args[1])
				sites = append(sites, wakeCensusSite{
					key: name + ":" + fn.Name.Name, callee: callee, dbArg: dbArg,
					inTx: txNames[dbArg], callPos: call.Pos(), function: fn,
				})
				return true
			})
		}
	}
	return sites
}

// transactionIdents names the identifiers in fn bound to an open
// transaction: assigned from a Begin call, or a parameter typed as a
// transaction.
func transactionIdents(fn *ast.FuncDecl) map[string]bool {
	names := map[string]bool{}
	for _, field := range fn.Type.Params.List {
		if t := exprText(field.Type); strings.Contains(t, "Transaction") || strings.HasSuffix(t, "sql.Tx") {
			for _, n := range field.Names {
				names[n.Name] = true
			}
		}
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 {
			return true
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok || !strings.Contains(calleeName(call), "Begin") {
			return true
		}
		if id, ok := assign.Lhs[0].(*ast.Ident); ok {
			names[id.Name] = true
		}
		return true
	})
	return names
}

// calleeName returns the called function or method name.
func calleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

// exprText renders a short expression (identifier, selector, star).
func exprText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprText(v.X) + "." + v.Sel.Name
	case *ast.StarExpr:
		return "*" + exprText(v.X)
	}
	return ""
}

// paramNames returns fn's parameter names.
func paramNames(fn *ast.FuncDecl) map[string]bool {
	names := map[string]bool{}
	for _, field := range fn.Type.Params.List {
		for _, n := range field.Names {
			names[n.Name] = true
		}
	}
	return names
}

// callsAfterCommit reports whether fn calls rewake after a Commit call.
func callsAfterCommit(fn *ast.FuncDecl, rewake string) bool {
	var commitPos token.Pos
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch calleeName(call) {
		case "Commit":
			if commitPos == token.NoPos {
				commitPos = call.Pos()
			}
		case rewake:
			if commitPos != token.NoPos && call.Pos() > commitPos {
				found = true
			}
		}
		return true
	})
	return found
}

// wakeFollowsAnyCommit reports whether a pool wake is ordered after its
// function's own transaction: a function that begins a transaction must
// commit it before the wake, and no Commit may follow the wake.
func wakeFollowsAnyCommit(site wakeCensusSite) bool {
	begins := false
	var firstCommit, lastCommit token.Pos
	ast.Inspect(site.function.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := calleeName(call)
		switch {
		case strings.Contains(name, "Begin"):
			begins = true
		case name == "Commit":
			if firstCommit == token.NoPos {
				firstCommit = call.Pos()
			}
			lastCommit = call.Pos()
		}
		return true
	})
	if lastCommit > site.callPos {
		return false
	}
	return !begins || (firstCommit != token.NoPos && firstCommit < site.callPos)
}

// auditWakeCensus checks every in-scope wake site against registry and
// returns the violations, sorted.
func auditWakeCensus(files map[string]*ast.File, registry map[string]wakeCensusEntry) []string {
	var violations []string
	seen := map[string]bool{}
	for _, site := range collectWakeCensusSites(files) {
		seen[site.key] = true
		entry, ok := registry[site.key]
		switch {
		case !ok:
			violations = append(violations, site.key+": unregistered "+site.callee+" wake site (db "+site.dbArg+
				"): register it with a post-commit re-wake or a written reason (#11228)")
		case site.inTx && entry.class != wakeCensusInTxRewoken:
			violations = append(violations, site.key+": "+site.callee+" wakes on open transaction "+site.dbArg+
				" but is registered "+string(entry.class)+": it needs a post-commit re-wake (#11228)")
		case entry.class == wakeCensusInTxRewoken && !site.inTx:
			violations = append(violations, site.key+": registered in_tx_rewoken but "+site.dbArg+
				" is not an open transaction: reclassify the site")
		case entry.class == wakeCensusInTxRewoken && !callsAfterCommit(site.function, entry.rewake):
			violations = append(violations, site.key+": in-tx wake without a call to "+entry.rewake+" after Commit")
		case entry.class == wakeCensusForwarder && (!paramNames(site.function)[site.dbArg] || !waitingWakeHelpers[site.function.Name.Name]):
			violations = append(violations, site.key+": registered forwarder but "+site.dbArg+" is not its own database parameter of a wake helper")
		case entry.class == wakeCensusPostCommit && !wakeFollowsAnyCommit(site):
			violations = append(violations, site.key+": registered post_commit but the function opens a transaction "+
				"and does not commit it before this wake, or commits after it")
		case entry.class != wakeCensusForwarder && entry.class != wakeCensusInTxRewoken && strings.TrimSpace(entry.reason) == "":
			violations = append(violations, site.key+": "+string(entry.class)+" site without a written reason")
		}
	}
	for key := range registry {
		if !seen[key] {
			violations = append(violations, key+": stale census entry, no wake site found")
		}
	}
	sort.Strings(violations)
	return violations
}

// parsePackageSources parses the package's non-test Go files in dir.
func parsePackageSources(t *testing.T, dir string) map[string]*ast.File {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		files[filepath.Base(path)] = file
	}
	return files
}
