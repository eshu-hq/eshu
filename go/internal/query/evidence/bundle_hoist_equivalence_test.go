// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

// TestHoistedStatusPlumbingEquivalence pins the hoisted status-plumbing
// copies in bundle_live.go to their root originals (#8345 F3a, #6642
// evidence-leaf move). bundle_live.go carries copies of
// loadStatusReportFiltered (root status_mappers.go), domainBacklogBuckets,
// pendingDomainWork, queueBlockageCountsByDomain, and the domainWorkBuckets
// type (root aws_materialization_status.go) so this package avoids an import
// cycle through root's compatibility aliases. The bundle snapshot must
// describe the same report the status routes render, so any divergence in
// these copies is a product-truth bug: this test fails until the copies are
// reunited (shared home) or re-synced.
//
// The comparison is semantic, not textual: each declaration is parsed and
// re-printed, so formatting drift passes while signature or body changes
// fail. Comments are ignored. Root-only members are intentionally out of
// scope: domainWorkBuckets.add aggregates per-domain buckets for the status
// routes and has no bundle caller, so only the struct shape is pinned.
func TestHoistedStatusPlumbingEquivalence(t *testing.T) {
	t.Parallel()

	dir := evidencePackageDir(t)
	bundleFile := filepath.Join(dir, "bundle_live.go")
	rootMappers := filepath.Join(dir, "..", "status_mappers.go")
	rootMaterialization := filepath.Join(dir, "..", "aws_materialization_status.go")

	funcPairs := []struct {
		name       string
		bundlePath string
		rootPath   string
	}{
		{"loadStatusReportFiltered", bundleFile, rootMappers},
		{"domainBacklogBuckets", bundleFile, rootMaterialization},
		{"pendingDomainWork", bundleFile, rootMaterialization},
		{"queueBlockageCountsByDomain", bundleFile, rootMaterialization},
	}
	for _, pair := range funcPairs {
		t.Run(pair.name, func(t *testing.T) {
			t.Parallel()
			bundleSrc := printedFuncDecl(t, pair.bundlePath, pair.name)
			rootSrc := printedFuncDecl(t, pair.rootPath, pair.name)
			if bundleSrc != rootSrc {
				t.Fatalf("hoisted %s diverged from root copy:\n--- bundle %s\n%s\n--- root %s\n%s",
					pair.name, pair.bundlePath, bundleSrc, pair.rootPath, rootSrc)
			}
		})
	}

	t.Run("domainWorkBuckets", func(t *testing.T) {
		t.Parallel()
		bundleSrc := printedTypeSpec(t, bundleFile, "domainWorkBuckets")
		rootSrc := printedTypeSpec(t, rootMaterialization, "domainWorkBuckets")
		if bundleSrc != rootSrc {
			t.Fatalf("hoisted domainWorkBuckets diverged from root copy:\n--- bundle %s\n%s\n--- root %s\n%s",
				bundleFile, bundleSrc, rootMaterialization, rootSrc)
		}
	})
}

// evidencePackageDir returns the on-disk directory of this package so the
// test reads its neighboring sources without hard-coding a checkout path.
func evidencePackageDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed to locate test file")
	}
	return filepath.Dir(file)
}

// parsedFile parses a Go source file with comments dropped so comment-only
// drift does not break equivalence.
func parsedFile(t *testing.T, path string) *ast.File {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return file
}

// printedFuncDecl returns the printer-normalized source of the named top-level
// function declaration.
func printedFuncDecl(t *testing.T, path, name string) string {
	t.Helper()
	for _, decl := range parsedFile(t, path).Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && fn.Name.Name == name {
			return printNode(fn)
		}
	}
	t.Fatalf("function %s not found in %s", name, path)
	return ""
}

// printedTypeSpec returns the printer-normalized source of the named top-level
// type declaration.
func printedTypeSpec(t *testing.T, path, name string) string {
	t.Helper()
	for _, decl := range parsedFile(t, path).Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.Name == name {
				return printNode(ts)
			}
		}
	}
	t.Fatalf("type %s not found in %s", name, path)
	return ""
}

// printNode renders one AST node with canonical formatting; positions and
// comments are already absent so output is comparable across files.
func printNode(node ast.Node) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, token.NewFileSet(), node); err != nil {
		panic(err)
	}
	return buf.String()
}
