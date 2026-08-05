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
	"strconv"
	"strings"
	"testing"
)

// TestNoPackageLevelMeterVariablesAcrossModule is the round-7 P2-1 structural
// guard. c3ada216 (see its commit message, "fix(query): resolve image-list
// and tag-history meters inside sync.Once") fixed both the image-list and
// tag-history handlers because each cached its OTel meter in a package-level
// var initialized outside any sync.Once (the pre-fix shape was
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
func TestNoPackageLevelMeterVariablesAcrossModule(t *testing.T) {
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
			t.Errorf("%s: package-level var %q is initialized directly by "+
				"otel.Meter(...); the OTel global proxy binds a meter resolved "+
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
// (top-level, non-const) var in file whose initializer resolves an OTel
// meter directly — either `<otelPkg>.Meter(...)` or
// `<otelPkg>.GetMeterProvider().Meter(...)`, the same delegate-once failure
// mode under a different call shape (`otel.Meter` and
// `otel.GetMeterProvider().Meter` both resolve through the process-global
// delegating proxy, see isOtelMeterCall). It only inspects file-scope
// declarations, so a local `meter := otel.Meter(...)` inside a function body
// (the correct, in-once pattern every current handler in this module uses)
// never matches.
func packageLevelOtelMeterVarNames(file *ast.File) []string {
	otelPkg := otelImportName(file)
	if otelPkg == "" {
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
				if !isOtelMeterCall(value, otelPkg) {
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

// otelImportName returns the identifier a file uses to reference
// "go.opentelemetry.io/otel": the import's explicit alias when the import is
// aliased, otherwise the package's default name "otel". Returns "" when the
// file does not import the package at all, so callers can skip files that
// cannot possibly contain a package-level otel.Meter var.
func otelImportName(file *ast.File) string {
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != "go.opentelemetry.io/otel" {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "otel"
	}
	return ""
}

// isOtelMeterCall reports whether expr resolves an OTel meter through
// otelPkg (the file's actual import name for "go.opentelemetry.io/otel",
// alias-resolved by otelImportName) in either of the two shapes that bind a
// meter's delegate permanently via the OTel global proxy:
// `<otelPkg>.Meter(...)`, and `<otelPkg>.GetMeterProvider().Meter(...)` —
// the latter is the same delegate-once failure mode reached through
// GetMeterProvider() instead of the Meter(...) shortcut, since both resolve
// through global.MeterProvider() under the hood.
func isOtelMeterCall(expr ast.Expr, otelPkg string) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Meter" {
		return false
	}
	switch x := sel.X.(type) {
	case *ast.Ident:
		// <otelPkg>.Meter(...)
		return x.Name == otelPkg
	case *ast.CallExpr:
		// <otelPkg>.GetMeterProvider().Meter(...)
		innerSel, ok := x.Fun.(*ast.SelectorExpr)
		if !ok || innerSel.Sel.Name != "GetMeterProvider" {
			return false
		}
		ident, ok := innerSel.X.(*ast.Ident)
		return ok && ident.Name == otelPkg
	default:
		return false
	}
}
