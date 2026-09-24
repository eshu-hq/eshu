// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// defaultBucketSecondsAllowlist names _seconds histograms that may stay on the
// OTEL default boundary set (0, 5, 10 ... 10000, sized for milliseconds). An
// entry needs a real justification: a seconds-unit histogram on the default
// set cannot resolve anything below 5 s, so p95 over it is meaningless for
// sub-5-second work. The list is empty on purpose (#7084); add to it only with
// a written reason that the recorded values genuinely span 0..10000 seconds.
var defaultBucketSecondsAllowlist = map[string]string{}

// histogramRegistration is one meter.Float64Histogram call found in source.
type histogramRegistration struct {
	Name          string
	File          string
	Line          int
	HasBoundaries bool
	// Unresolved is set when the instrument name is not a string literal or a
	// package-level string constant, or when the options arrive as a spread
	// slice; the guard cannot prove the boundaries in either case.
	Unresolved string
}

// scanHistogramRegistrations parses one Go source file and returns every
// Float64Histogram registration in it. consts maps package-level string
// constant names to values so a constant instrument name resolves.
func scanHistogramRegistrations(fset *token.FileSet, file *ast.File, consts map[string]string) []histogramRegistration {
	var out []histogramRegistration
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Float64Histogram" || len(call.Args) == 0 {
			return true
		}
		pos := fset.Position(call.Pos())
		reg := histogramRegistration{File: pos.Filename, Line: pos.Line}
		reg.Name = evalStringExpr(call.Args[0], consts)
		if reg.Name == "" {
			reg.Unresolved = "instrument name is not a string literal or package constant"
		}
		if call.Ellipsis.IsValid() {
			reg.Unresolved = "options passed as a spread slice; boundaries not provable"
		}
		for _, opt := range call.Args[1:] {
			c, ok := opt.(*ast.CallExpr)
			if !ok {
				continue
			}
			if s, ok := c.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "WithExplicitBucketBoundaries" {
				reg.HasBoundaries = true
			}
		}
		out = append(out, reg)
		return true
	})
	return out
}

// evalStringExpr folds a string literal, a named string constant (bare or
// package-qualified; the last identifier is the key), or a "+" concatenation of
// those. It returns "" when the expression is anything else.
func evalStringExpr(expr ast.Expr, consts map[string]string) string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			if v, err := strconv.Unquote(e.Value); err == nil {
				return v
			}
		}
	case *ast.Ident:
		return consts[e.Name]
	case *ast.SelectorExpr:
		return consts[e.Sel.Name]
	case *ast.ParenExpr:
		return evalStringExpr(e.X, consts)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return ""
		}
		l, r := evalStringExpr(e.X, consts), evalStringExpr(e.Y, consts)
		if l == "" || r == "" {
			return ""
		}
		return l + r
	}
	return ""
}

// collectStringConsts adds one file's package-level string constants to
// consts, keyed by identifier. Constants are resolved by bare name across the
// whole module, which is enough for the instrument-name constants in use.
func collectStringConsts(file *ast.File, consts map[string]string) {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if v := evalStringExpr(vs.Values[i], consts); v != "" {
					consts[name.Name] = v
				}
			}
		}
	}
}

// defaultBucketSecondsViolations returns one message per _seconds histogram
// registered without explicit boundaries and not allowlisted, plus one per
// registration the scanner cannot resolve.
func defaultBucketSecondsViolations(regs []histogramRegistration, allow map[string]string) []string {
	var out []string
	for _, r := range regs {
		loc := r.File + ":" + strconv.Itoa(r.Line)
		if r.Unresolved != "" {
			out = append(out, loc+": cannot audit histogram registration: "+r.Unresolved)
			continue
		}
		if !strings.HasSuffix(r.Name, "_seconds") || r.HasBoundaries {
			continue
		}
		if strings.TrimSpace(allow[r.Name]) != "" {
			continue
		}
		out = append(out, loc+": "+r.Name+" is a _seconds histogram registered with no WithExplicitBucketBoundaries; the default OTEL set is millisecond-scaled")
	}
	sort.Strings(out)
	return out
}

// scanModuleHistograms walks the Go module at root and returns every
// Float64Histogram registration in non-test production sources.
func scanModuleHistograms(t *testing.T, root string) []histogramRegistration {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); path != root && (n == "testdata" || n == "vendor" || strings.HasPrefix(n, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			return err
		}
		files = append(files, file)
		return nil
	})
	if err != nil {
		t.Fatalf("scan module for histograms: %v", err)
	}
	// Repeat the collection so a const defined from another const
	// (MetricPrefix + "x") resolves regardless of file order.
	consts := map[string]string{}
	for pass := 0; pass < 3; pass++ {
		for _, f := range files {
			collectStringConsts(f, consts)
		}
	}
	var regs []histogramRegistration
	for _, f := range files {
		regs = append(regs, scanHistogramRegistrations(fset, f, consts)...)
	}
	return regs
}

// TestSecondsHistogramsHaveExplicitBuckets fails when any production
// meter.Float64Histogram named *_seconds is registered without
// metric.WithExplicitBucketBoundaries (#7084). Unlike bucketAuditTable, this
// scans the source of the whole module, so a new histogram cannot escape by
// lacking a row in the table.
func TestSecondsHistogramsHaveExplicitBuckets(t *testing.T) {
	regs := scanModuleHistograms(t, filepath.Join("..", ".."))
	if len(regs) < 50 {
		t.Fatalf("scanner found only %d histogram registrations; module walk is broken", len(regs))
	}
	for _, v := range defaultBucketSecondsViolations(regs, defaultBucketSecondsAllowlist) {
		t.Error(v)
	}
}

// TestSecondsHistogramGuardSeededViolation proves the guard is not vacuous:
// a planted default-bucket _seconds histogram is flagged, while explicit
// boundaries, a non-_seconds name, and a justified allowlist entry are not.
func TestSecondsHistogramGuardSeededViolation(t *testing.T) {
	const src = `package seeded

import "go.opentelemetry.io/otel/metric"

const constName = "eshu_dp_seeded_const_seconds"

func register(meter metric.Meter) {
	meter.Float64Histogram("eshu_dp_seeded_default_seconds", metric.WithUnit("s"))
	meter.Float64Histogram(constName)
	meter.Float64Histogram("eshu_dp_seeded_explicit_seconds", metric.WithExplicitBucketBoundaries(0.1, 1))
	meter.Float64Histogram("eshu_dp_seeded_batch_size")
	meter.Float64Histogram("eshu_dp_seeded_allowed_seconds")
	meter.Float64Histogram("eshu_dp_seeded_spread_seconds", opts...)
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "seeded.go", src, 0)
	if err != nil {
		t.Fatalf("parse seeded source: %v", err)
	}
	consts := map[string]string{}
	collectStringConsts(file, consts)
	regs := scanHistogramRegistrations(fset, file, consts)
	allow := map[string]string{"eshu_dp_seeded_allowed_seconds": "values genuinely span hours"}
	got := strings.Join(defaultBucketSecondsViolations(regs, allow), "\n")

	for _, want := range []string{"eshu_dp_seeded_default_seconds", "eshu_dp_seeded_const_seconds", "cannot audit histogram registration"} {
		if !strings.Contains(got, want) {
			t.Errorf("guard missed planted violation %q; violations:\n%s", want, got)
		}
	}
	for _, clean := range []string{"eshu_dp_seeded_explicit_seconds", "eshu_dp_seeded_batch_size", "eshu_dp_seeded_allowed_seconds"} {
		if strings.Contains(got, clean) {
			t.Errorf("guard flagged clean registration %q; violations:\n%s", clean, got)
		}
	}
}

// TestBucketAuditRowRejectsDefaultSecondsHistogram proves the table audit
// marks a nil-Buckets _seconds row as failing while a nil-Buckets count row
// keeps its "default" verdict.
func TestBucketAuditRowRejectsDefaultSecondsHistogram(t *testing.T) {
	if got := auditEntry(bucketAuditEntry{MetricName: "eshu_dp_seeded_default_seconds"}).Verdict; got != "fails" {
		t.Errorf("nil-bucket _seconds row verdict = %q, want fails", got)
	}
	if got := auditEntry(bucketAuditEntry{MetricName: "eshu_dp_seeded_batch_size"}).Verdict; got != "default" {
		t.Errorf("nil-bucket count row verdict = %q, want default", got)
	}
}
