// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/parser"
	"go/token"
	"slices"
	"testing"
)

// TestPackageLevelOtelMeterVarNames is the positive-case guard for the
// recognizer behind TestNoPackageLevelMeterVarInitializersAcrossModule (see
// package_var_meter_guard_test.go). That test walks every non-test file in the
// module and asserts zero hits, so it passes just as happily against a
// recognizer stubbed to `return nil`: it proves the module is clean today, never
// that the recognizer would notice if the module stopped being clean. This
// table supplies the source the module deliberately does not contain.
//
// Each case is a whole Go file parsed with go/parser — nothing here is
// compiled, so the sources only have to be syntactically valid. want lists the
// package-level var names the recognizer must report, in declaration order.
//
// The empty-want cases carry as much weight as the reporting ones. A recognizer
// that fired on a bare provider var, on a local inside a function body, or on
// any `.Meter(` selector at all would turn the module-wide guard into a gate
// that fails on correct code. The last four also pin the documented gaps, so a
// later change that closes one has to come here and say so.
func TestPackageLevelOtelMeterVarNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			name: "direct otel.Meter initializer",
			src: `package p
import "go.opentelemetry.io/otel"
var m = otel.Meter("probe")`,
			want: []string{"m"},
		},
		{
			name: "GetMeterProvider().Meter chained in one initializer",
			src: `package p
import "go.opentelemetry.io/otel"
var m = otel.GetMeterProvider().Meter("probe")`,
			want: []string{"m"},
		},
		{
			name: "meter resolved through a package-level provider var",
			src: `package p
import "go.opentelemetry.io/otel"
var p = otel.GetMeterProvider()
var m = p.Meter("probe")`,
			want: []string{"m"},
		},
		{
			name: "provider var declared after the meter var that uses it",
			src: `package p
import "go.opentelemetry.io/otel"
var m = p.Meter("probe")
var p = otel.GetMeterProvider()`,
			want: []string{"m"},
		},
		{
			name: "var block declaring provider and meters together",
			src: `package p
import "go.opentelemetry.io/otel"
var (
	p = otel.GetMeterProvider()
	a = p.Meter("a")
	b = otel.Meter("b")
)`,
			want: []string{"a", "b"},
		},
		{
			name: "tuple declarations on both sides",
			src: `package p
import "go.opentelemetry.io/otel"
var p, q = otel.GetMeterProvider(), otel.GetMeterProvider()
var a, b = p.Meter("a"), q.Meter("b")`,
			want: []string{"a", "b"},
		},
		{
			name: "aliased import",
			src: `package p
import ot "go.opentelemetry.io/otel"
var p = ot.GetMeterProvider()
var m = p.Meter("probe")`,
			want: []string{"m"},
		},
		{
			name: "dot import loses the qualifier in all three shapes",
			src: `package p
import . "go.opentelemetry.io/otel"
var a = Meter("a")
var b = GetMeterProvider().Meter("b")
var p = GetMeterProvider()
var c = p.Meter("c")`,
			want: []string{"a", "b", "c"},
		},
		{
			name: "same path imported twice under different names",
			src: `package p
import (
	"go.opentelemetry.io/otel"
	ot "go.opentelemetry.io/otel"
)
var a = otel.Meter("a")
var b = ot.Meter("b")`,
			want: []string{"a", "b"},
		},
		{
			name: "provider var never used for a package-level meter",
			src: `package p
import "go.opentelemetry.io/otel"
var p = otel.GetMeterProvider()
func use() { _ = p.Meter("probe") }`,
			want: nil,
		},
		{
			name: "meter resolved locally inside a function body",
			src: `package p
import "go.opentelemetry.io/otel"
func f() {
	m := otel.Meter("probe")
	_ = m
}`,
			want: nil,
		},
		{
			name: "local shadowing a package-level provider var",
			src: `package p
import "go.opentelemetry.io/otel"
var p = otel.GetMeterProvider()
func f() {
	p := otel.GetMeterProvider()
	_ = p.Meter("probe")
}`,
			want: nil,
		},
		{
			name: "Meter selector on an unrelated package",
			src: `package p
import "go.opentelemetry.io/otel"
import "example.com/metrics"
var _ = otel.Tracer("t")
var m = metrics.Meter("probe")`,
			want: nil,
		},
		{
			name: "gap: provider returned by a helper call",
			src: `package p
import "go.opentelemetry.io/otel"
var p = makeProvider()
var m = p.Meter("probe")
func makeProvider() any { return otel.GetMeterProvider() }`,
			want: nil,
		},
		{
			name: "gap: GetMeterProvider used as a method value",
			src: `package p
import "go.opentelemetry.io/otel"
var p = otel.GetMeterProvider
var m = p().Meter("probe")`,
			want: nil,
		},
		{
			name: "gap: provider var lives in another file of the package",
			src: `package p
var m = probeProvider.Meter("probe")`,
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			file, err := parser.ParseFile(token.NewFileSet(), "probe.go", tc.src, 0)
			if err != nil {
				t.Fatalf("parse fixture: %v", err)
			}
			got := packageLevelOtelMeterVarNames(file)
			if !slices.Equal(got, tc.want) {
				t.Errorf("packageLevelOtelMeterVarNames() = %v, want %v", got, tc.want)
			}
		})
	}
}
