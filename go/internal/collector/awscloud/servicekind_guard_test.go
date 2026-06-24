// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestScannerServiceKindSwitchesCanonicalize is a by-construction guard against
// the service_kind canonicalization bug fixed across the awscloud scanner fleet.
//
// Every scanner gates its work behind:
//
//	switch strings.TrimSpace(boundary.ServiceKind) {
//	case "", awscloud.Service<X>:
//	    boundary.ServiceKind = awscloud.Service<X>
//	default:
//	    return ..., fmt.Errorf(...)
//	}
//
// The switch trims only for the comparison, so a padded input like " sns "
// matches the canonical case yet keeps its padding unless the matched case
// writes the canonical constant back to boundary.ServiceKind. envelope.go copies
// boundary.ServiceKind verbatim into every emitted fact, so a missing write-back
// leaks the padded string into the graph and breaks joins/filters keyed on the
// canonical value.
//
// The original bug was an empty-bodied non-default case (`case awscloud.Service<X>:`
// with nothing after it): the kind was validated but never canonicalized. This
// guard walks every services/*/scanner.go that uses the trim-switch and asserts
// the shape that makes the bug impossible: a default arm (mismatch rejection),
// no empty-bodied non-default case, and at least one arm that writes
// boundary.ServiceKind back. A new scanner that copies the old boilerplate fails
// this test instead of silently shipping the leak.
func TestScannerServiceKindSwitchesCanonicalize(t *testing.T) {
	dir := servicesSourceDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read services dir %q: %v", dir, err)
	}

	checked, skipped := 0, 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		scannerPath := filepath.Join(dir, entry.Name(), "scanner.go")
		if _, statErr := os.Stat(scannerPath); statErr != nil {
			continue
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, scannerPath, nil, 0)
		if parseErr != nil {
			t.Errorf("%s: parse scanner.go: %v", entry.Name(), parseErr)
			continue
		}
		sw := findServiceKindSwitch(file)
		if sw == nil {
			// The scanner does not use the trim-switch pattern; this guard only
			// enforces canonicalization on scanners that do.
			skipped++
			continue
		}
		checked++
		assertSwitchCanonicalizes(t, entry.Name(), sw)
	}

	if checked == 0 {
		t.Fatalf("guard inspected zero service_kind switches under %q; check directory resolution", dir)
	}
	t.Logf("service_kind canonicalization guard: %d scanners checked, %d skipped (no trim-switch)", checked, skipped)
}

// servicesSourceDir resolves go/internal/collector/awscloud/services from this
// test file's own location so the guard has no dependency on the caller's
// working directory.
func servicesSourceDir(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve current file to locate services dir")
	}
	return filepath.Join(filepath.Dir(currentFile), "services")
}

// findServiceKindSwitch returns the first switch statement in file whose tag is
// strings.TrimSpace(boundary.ServiceKind), or nil when the file has none.
func findServiceKindSwitch(file *ast.File) *ast.SwitchStmt {
	var found *ast.SwitchStmt
	ast.Inspect(file, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		sw, ok := n.(*ast.SwitchStmt)
		if ok && isTrimSpaceServiceKind(sw.Tag) {
			found = sw
			return false
		}
		return true
	})
	return found
}

// assertSwitchCanonicalizes fails the test with a service-named message for each
// way sw deviates from the canonicalizing shape: a missing default arm, an
// empty-bodied non-default case (the validated-but-not-canonicalized bug), or no
// arm that writes boundary.ServiceKind back.
func assertSwitchCanonicalizes(t *testing.T, service string, sw *ast.SwitchStmt) {
	t.Helper()
	var hasDefault, writesBack bool
	for _, stmt := range sw.Body.List {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		if clause.List == nil {
			hasDefault = true
			continue
		}
		if len(clause.Body) == 0 {
			t.Errorf("%s: service_kind switch has an empty-bodied case %s; the matched kind is "+
				"validated but never canonicalized, so a padded service_kind leaks into emitted facts "+
				"(merge it with the \"\" case and write boundary.ServiceKind back)",
				service, caseListText(clause.List))
		}
		if bodyAssignsServiceKind(clause.Body) {
			writesBack = true
		}
	}
	if !hasDefault {
		t.Errorf("%s: service_kind switch has no default arm; an unexpected service_kind must be rejected", service)
	}
	if !writesBack {
		t.Errorf("%s: service_kind switch never assigns boundary.ServiceKind; the canonical value is "+
			"not written back and padded input would leak into emitted facts", service)
	}
}

// isTrimSpaceServiceKind reports whether expr is the call
// strings.TrimSpace(boundary.ServiceKind).
func isTrimSpaceServiceKind(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	if !isSelector(call.Fun, "strings", "TrimSpace") {
		return false
	}
	return isSelector(call.Args[0], "boundary", "ServiceKind")
}

// bodyAssignsServiceKind reports whether any statement in body assigns to
// boundary.ServiceKind.
func bodyAssignsServiceKind(body []ast.Stmt) bool {
	for _, stmt := range body {
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok {
			continue
		}
		for _, lhs := range assign.Lhs {
			if isSelector(lhs, "boundary", "ServiceKind") {
				return true
			}
		}
	}
	return false
}

// isSelector reports whether expr is the selector x.sel where x is a bare
// identifier (for example boundary.ServiceKind or strings.TrimSpace).
func isSelector(expr ast.Expr, x, sel string) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != sel {
		return false
	}
	ident, ok := selector.X.(*ast.Ident)
	return ok && ident.Name == x
}

// caseListText renders a case clause's expression list for a failure message,
// naming the constants that arm matches (for example `awscloud.ServiceSNS`).
func caseListText(list []ast.Expr) string {
	parts := make([]string, 0, len(list))
	for _, expr := range list {
		switch e := expr.(type) {
		case *ast.SelectorExpr:
			if ident, ok := e.X.(*ast.Ident); ok {
				parts = append(parts, ident.Name+"."+e.Sel.Name)
				continue
			}
			parts = append(parts, e.Sel.Name)
		case *ast.BasicLit:
			parts = append(parts, e.Value)
		case *ast.Ident:
			parts = append(parts, e.Name)
		default:
			parts = append(parts, "<expr>")
		}
	}
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += ", "
		}
		out += part
	}
	return "`case " + out + ":`"
}
