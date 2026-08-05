// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestNoPackageLevelMeterVarInitializersAcrossModule is the round-7 P2-1
// structural guard. c3ada216 (see its commit message, "fix(query): resolve
// image-list and tag-history meters inside sync.Once") fixed both the
// image-list and tag-history handlers because each cached its OTel meter in a
// package-level var initialized outside any sync.Once (the pre-fix shape was
// `var imageQueryMeter = otel.Meter(imageQueryMeterName)`, evaluated once at
// package-init time). The OTel global proxy binds such a meter's delegate
// permanently to whichever provider first calls otel.SetMeterProvider in the
// process, so a test that later installs its own manual-reader provider
// silently records onto the wrong (possibly already shut down) provider
// instead — see initImageQueryInstruments's doc comment in
// images_telemetry.go for the full mechanics. request_metrics.go was never
// exposed to this bug: apiRequestMetrics calls otel.Meter(apiRequestMeterName)
// from *inside* apiRequestInstrumentsOnce.Do (request_metrics.go), not from a
// package var — see TestRequestMetricsMiddlewareEmitsPerEndpointMetrics's
// setup comment in request_metrics_test.go for why that in-once resolution,
// not "this is the only Prometheus-backed test in the package", is what makes
// that test immune to file-order luck.
//
// This walks every non-test .go file in the module from go.mod outward
// (mirroring TestWriteGraphReadErrorHasNoCallersOutsideThisPackage's
// module-wide walk in graph_read_error_capability_sweep_test.go) and fails if
// any package-level `var` declaration is initialized directly by a call to
// otel.Meter(...), so a future handler that copies the pre-fix pattern is
// caught here — by CI, deterministically — instead of by review.
//
// The name says "var initializers" because that is exactly the scope: a
// package-level var whose own initializer expression resolves the meter. Two
// other ways to reach the same bug are outside it and are not caught here — a
// var assigned from an `init()` function body, and a var initialized from a
// local helper that returns otel.Meter(...) — because both put the resolving
// call somewhere other than the var's initializer, and following them would
// need type resolution rather than a single-file AST walk. They are a
// deliberate gap, not an oversight: the pre-fix shape c3ada216 removed, and
// the one a future handler is most likely to copy, is the direct initializer.
func TestNoPackageLevelMeterVarInitializersAcrossModule(t *testing.T) {
	t.Parallel()

	packageDir := queryPackageDir(t)
	moduleRoot := findModuleRoot(t, packageDir)
	fset := token.NewFileSet()

	walkErr := filepath.WalkDir(moduleRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != moduleRoot && strings.HasPrefix(entry.Name(), ".") {
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
		for _, name := range packageLevelOtelMeterVarNames(file) {
			t.Errorf("%s: package-level var %q is initialized directly by an "+
				"otel Meter(...) call; the OTel global proxy binds a meter resolved "+
				"outside a sync.Once permanently to whichever provider first "+
				"calls otel.SetMeterProvider in the process (the bug class "+
				"c3ada216 fixed) — resolve the meter from inside a sync.Once "+
				"instead, see initImageQueryInstruments in "+
				"go/internal/query/images_telemetry.go", path, name)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", moduleRoot, walkErr)
	}
}

// packageLevelOtelMeterVarNames returns the names of every package-level
// (top-level, non-const) var in file whose own initializer expression
// resolves an OTel meter directly — either `<otelPkg>.Meter(...)` or
// `<otelPkg>.GetMeterProvider().Meter(...)`, the same delegate-once failure
// mode under a different call shape (`otel.Meter` and
// `otel.GetMeterProvider().Meter` both resolve through the process-global
// delegating proxy, see isOtelMeterCall). It only inspects file-scope
// declarations, so a local `meter := otel.Meter(...)` inside a function body
// (the correct, in-once pattern every current handler in this module uses)
// never matches. A var assigned in an `init()` body or from a helper function
// that returns the meter is likewise not an initializer and is not reported;
// see the gap note on TestNoPackageLevelMeterVarInitializersAcrossModule.
func packageLevelOtelMeterVarNames(file *ast.File) []string {
	otelPkgs := otelImportNames(file)
	if len(otelPkgs) == 0 {
		return nil
	}
	var names []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, value := range valueSpec.Values {
				if !isOtelMeterCall(value, otelPkgs) {
					continue
				}
				name := "?"
				if i < len(valueSpec.Names) {
					name = valueSpec.Names[i].Name
				}
				names = append(names, name)
			}
		}
	}
	return names
}

// otelDotImportQualifier is the sentinel otelImportNames returns for a dot
// import of "go.opentelemetry.io/otel". A dot import has no qualifier at all,
// so the empty string is the only honest stand-in: the calls it must match are
// the bare `Meter(...)` and `GetMeterProvider().Meter(...)` handled explicitly
// in isOtelMeterCall and isOtelMeterReceiver. Note that "." itself would be
// the wrong sentinel — it is the import spec's name, but no AST identifier in
// the file is ever spelled ".", so comparing against it matches nothing.
const otelDotImportQualifier = ""

// otelImportNames returns every identifier a file can use to reference
// "go.opentelemetry.io/otel". A plain import contributes "otel", an aliased
// import contributes its alias, and a dot import contributes
// otelDotImportQualifier. Go permits importing the same path more than once
// under different names, so all of them are collected: returning only the
// first match would leave a var written against the second name invisible.
// An empty result means the file does not import the package and cannot
// contain a package-level otel.Meter var.
func otelImportNames(file *ast.File) []string {
	var names []string
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != "go.opentelemetry.io/otel" {
			continue
		}
		switch {
		case imp.Name == nil:
			names = append(names, "otel")
		case imp.Name.Name == ".":
			names = append(names, otelDotImportQualifier)
		default:
			names = append(names, imp.Name.Name)
		}
	}
	return names
}

// isOtelMeterCall reports whether expr resolves an OTel meter through one of
// otelPkgs (the file's import names for "go.opentelemetry.io/otel", collected
// by otelImportNames) in either of the two shapes that bind a meter's delegate
// permanently via the OTel global proxy: `<otelPkg>.Meter(...)`, and
// `<otelPkg>.GetMeterProvider().Meter(...)`. The latter is the same
// delegate-once failure mode reached through GetMeterProvider() instead of the
// Meter(...) shortcut, since both resolve through global.MeterProvider() under
// the hood. Under a dot import both shapes lose their qualifier and appear as
// bare `Meter(...)` and `GetMeterProvider().Meter(...)`.
func isOtelMeterCall(expr ast.Expr, otelPkgs []string) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		// Dot import: bare Meter(...). A package-level `func Meter` in a file
		// that dot-imports otel would not compile (the file and package blocks
		// may not declare the same identifier), so this cannot collide with a
		// locally defined Meter.
		return fun.Name == "Meter" && slices.Contains(otelPkgs, otelDotImportQualifier)
	case *ast.SelectorExpr:
		return fun.Sel.Name == "Meter" && isOtelMeterReceiver(fun.X, otelPkgs)
	default:
		return false
	}
}

// isOtelMeterReceiver reports whether receiver is the left half of an OTel
// meter resolution: the otel package identifier itself, or a
// GetMeterProvider() call on it — qualified under a normal or aliased import,
// bare under a dot import.
func isOtelMeterReceiver(receiver ast.Expr, otelPkgs []string) bool {
	switch value := receiver.(type) {
	case *ast.Ident:
		// <otelPkg>.Meter(...)
		return slices.Contains(otelPkgs, value.Name)
	case *ast.CallExpr:
		switch inner := value.Fun.(type) {
		case *ast.SelectorExpr:
			// <otelPkg>.GetMeterProvider().Meter(...)
			if inner.Sel.Name != "GetMeterProvider" {
				return false
			}
			ident, ok := inner.X.(*ast.Ident)
			return ok && slices.Contains(otelPkgs, ident.Name)
		case *ast.Ident:
			// Dot import: GetMeterProvider().Meter(...)
			return inner.Name == "GetMeterProvider" &&
				slices.Contains(otelPkgs, otelDotImportQualifier)
		default:
			return false
		}
	default:
		return false
	}
}
