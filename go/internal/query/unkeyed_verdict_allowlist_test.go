// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// noCapabilityAllowedDirs lists the directories, relative to
// go/internal/query, whose WriteGraphReadError and GraphReadErrorEnvelope
// call sites may pass querycontract.NoCapability instead of a
// capability-matrix key. Each entry names why the routes there have no key
// and the issue that registers one. Only the directory itself is allowed,
// not its subdirectories. A bare "" literal is never accepted: it still
// fails the matrix check, so an empty capability is always a named choice.
var noCapabilityAllowedDirs = map[string]string{
	"admin": "the /api/v0/admin/* inspection reads (work items, decisions and " +
		"their evidence, replay events, repository reindex catalog lookup) are " +
		"operator tooling not registered in the capability matrix; #8176 " +
		"registers their keys and removes this entry",
}

// noCapabilityConstName is the querycontract constant the sweep recognizes.
const noCapabilityConstName = "NoCapability"

// isNoCapabilityArg reports whether expr names querycontract.NoCapability:
// a selector on an identifier the consuming file imports as the
// querycontract package. A same-spelled selector on any other package or
// value is not it.
func (s *capabilitySweep) isNoCapabilityArg(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != noCapabilityConstName {
		return false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	importPath, ok := s.fileImports[s.fset.Position(sel.Pos()).Filename][pkgIdent.Name]
	return ok && path.Base(importPath) == "querycontract"
}

// noCapabilityAllowedAt reports whether the call site at pos lives directly
// in an allowlisted directory under go/internal/query.
func (s *capabilitySweep) noCapabilityAllowedAt(pos token.Pos) bool {
	dir := filepath.ToSlash(s.dirOf(pos))
	for rel := range noCapabilityAllowedDirs {
		if strings.HasSuffix(dir, "/internal/query/"+rel) {
			return true
		}
	}
	return false
}

// noCapabilityFindings returns no finding for a querycontract.NoCapability
// call site in an allowlisted directory, and one finding anywhere else.
func (s *capabilitySweep) noCapabilityFindings(pos token.Pos) []string {
	if s.noCapabilityAllowedAt(pos) {
		return nil
	}
	return []string{"querycontract.NoCapability at " + s.fset.Position(pos).String() +
		" is outside noCapabilityAllowedDirs; pass a capability-matrix key"}
}

// TestCapabilitySweepNoCapabilityIsAllowlistedByDirectory is the seeded
// RED/GREEN pair for the NoCapability form: on-disk packages under a fake
// internal/query tree, swept by the real capabilitySweep, prove the
// allowlisted admin directory may pass querycontract.NoCapability, a
// non-allowlisted directory and an admin subdirectory may not, and a bare ""
// literal fails even inside admin.
func TestCapabilitySweepNoCapabilityIsAllowlistedByDirectory(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "internal", "query")
	write := func(rel, src string) string {
		t.Helper()
		dir := filepath.Join(root, rel)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		file := filepath.Join(dir, "route.go")
		if err := os.WriteFile(file, []byte(src), 0o600); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
		return file
	}
	const contract = "package querycontract\n\nconst NoCapability = \"\"\n\n" +
		"func WriteGraphReadError(w, r, err any, capability string) bool { return false }\n"
	route := func(pkg, arg string) string {
		return "package " + pkg + "\n\nimport \"example.test/internal/query/querycontract\"\n\n" +
			"func handle(w, r, err any) { querycontract.WriteGraphReadError(w, r, err, " + arg + ") }\n"
	}
	files := []string{
		write("querycontract", contract),
		write("admin", route("admin", "querycontract.NoCapability")),
		write("other", route("other", "querycontract.NoCapability")),
		write("admin/nested", route("nested", "querycontract.NoCapability")),
		write("bare", route("bare", `""`)),
	}

	fset := token.NewFileSet()
	sweep := newCapabilitySweep(fset)
	parsed := map[string]*ast.File{}
	for _, name := range files {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		parsed[name] = file
		sweep.collectDecls(file)
	}
	for _, file := range parsed {
		sweep.collectCallSites(file)
	}

	want := map[string]bool{ // directory -> want a finding
		"admin":        false,
		"other":        true,
		"admin/nested": true,
		"bare":         true,
	}
	for rel, wantFinding := range want {
		file := parsed[filepath.Join(root, rel, "route.go")]
		findings, matched := sweep.findCallSites(file)
		if matched != 1 {
			t.Fatalf("%s: matched %d call sites, want 1", rel, matched)
		}
		if got := len(findings) > 0; got != wantFinding {
			t.Errorf("%s: finding = %v (%v), want %v", rel, got, findings, wantFinding)
		}
	}

	// The bare literal is also refused inside the allowlisted directory.
	adminBare := write("admin", route("admin", `""`))
	file, err := parser.ParseFile(fset, adminBare, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", adminBare, err)
	}
	sweep.collectDecls(file)
	if findings, _ := sweep.findCallSites(file); len(findings) == 0 {
		t.Error(`admin: a bare "" capability passed the sweep, want a finding`)
	}
}
