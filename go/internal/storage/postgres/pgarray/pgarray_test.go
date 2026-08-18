// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pgarray

import (
	"database/sql/driver"
	"math"
	"reflect"
	"strings"
	"testing"
)

// stringCase is one row of the text-array encoding table. want is the exact
// driver value: a string literal, or nil for SQL NULL. The expected literals
// were captured from lib/pq v1.10.9's StringArray.Value and proven equal in
// the same process by the differential test that ran while both packages were
// present (see the package README, "How the encoding was proven").
type stringCase struct {
	name string
	in   []string
	want driver.Value
}

var stringCases = []stringCase{
	{name: "nil slice is SQL NULL", in: nil, want: nil},
	{name: "empty slice", in: []string{}, want: "{}"},
	{name: "single element", in: []string{"a"}, want: `{"a"}`},
	{name: "multi element", in: []string{"a", "b", "c"}, want: `{"a","b","c"}`},
	{name: "comma", in: []string{"a,b"}, want: `{"a,b"}`},
	{name: "double quote", in: []string{`say "hi"`}, want: `{"say \"hi\""}`},
	{name: "backslash", in: []string{`c:\temp`}, want: `{"c:\\temp"}`},
	{name: "braces", in: []string{"{x}", "}{"}, want: `{"{x}","}{"}`},
	{name: "literal NULL word", in: []string{"NULL"}, want: `{"NULL"}`},
	{name: "empty string element", in: []string{""}, want: `{""}`},
	{name: "empty among others", in: []string{"a", "", "b"}, want: `{"a","","b"}`},
	{name: "leading and trailing whitespace", in: []string{" a", "b ", " c "}, want: `{" a","b "," c "}`},
	{name: "tab and newline", in: []string{"a\tb", "c\nd"}, want: "{\"a\tb\",\"c\nd\"}"},
	{name: "utf-8", in: []string{"héllo", "日本語", "🙂"}, want: `{"héllo","日本語","🙂"}`},
	{name: "quote and backslash mixed", in: []string{`\"`, `"\`}, want: `{"\\\"","\"\\"}`},
	{name: "sql wildcard like terms", in: []string{"%alpha%", "%beta%"}, want: `{"%alpha%","%beta%"}`},
	{name: "repository ids", in: []string{"github.com/acme/alpha", "repo-b"}, want: `{"github.com/acme/alpha","repo-b"}`},
	{name: "single quote", in: []string{"it's"}, want: `{"it's"}`},
	{name: "dollar and semicolon", in: []string{"$1;", "--x"}, want: `{"$1;","--x"}`},
}

// floatCase mirrors stringCase for the double precision array Eshu stores for
// search vectors.
type floatCase struct {
	name string
	in   []float64
	want driver.Value
}

var floatCases = []floatCase{
	{name: "nil slice is SQL NULL", in: nil, want: nil},
	{name: "empty slice", in: []float64{}, want: "{}"},
	{name: "single", in: []float64{1.5}, want: "{1.5}"},
	{name: "multi", in: []float64{0, -1, 2.25}, want: "{0,-1,2.25}"},
	{name: "integers stay short", in: []float64{3, 1e6}, want: "{3,1000000}"},
	{name: "small magnitude expands", in: []float64{1e-7}, want: "{0.0000001}"},
	{name: "shortest round trip", in: []float64{0.1, 0.30000000000000004}, want: "{0.1,0.30000000000000004}"},
	{name: "negative zero", in: []float64{math.Copysign(0, -1)}, want: "{-0}"},
	{name: "large", in: []float64{1e21}, want: "{1000000000000000000000}"},
}

// identCase is one row of the QuoteIdentifier table. The doubled-quote rows
// are the injection-relevant ones.
type identCase struct {
	name, in, want string
}

var identCases = []identCase{
	{name: "plain", in: "eshu_test", want: `"eshu_test"`},
	{name: "empty", in: "", want: `""`},
	{name: "mixed case preserved", in: "MySchema", want: `"MySchema"`},
	{name: "embedded double quote doubled", in: `a"b`, want: `"a""b"`},
	{name: "leading and trailing quotes", in: `"x"`, want: `"""x"""`},
	{name: "quote injection attempt", in: `x"; DROP SCHEMA public; --`, want: `"x""; DROP SCHEMA public; --"`},
	{name: "single quote untouched", in: "it's", want: `"it's"`},
	{name: "space and dash", in: "eshu test-1", want: `"eshu test-1"`},
	{name: "utf-8", in: "схема", want: `"схема"`},
	{name: "zero byte truncates", in: "abc\x00def", want: `"abc"`},
}

// scanCase is one row of the Scan table over the text form Postgres emits.
// wantErr means Scan must fail; want is the decoded slice otherwise.
type scanCase struct {
	name    string
	src     any
	want    []string
	wantErr bool
}

var scanCases = []scanCase{
	{name: "sql null", src: nil, want: nil},
	{name: "empty array", src: "{}", want: []string{}},
	{name: "empty array bytes", src: []byte("{}"), want: []string{}},
	{name: "bare single", src: "{a}", want: []string{"a"}},
	{name: "bare multi", src: "{a,b,c}", want: []string{"a", "b", "c"}},
	{name: "quoted comma", src: `{"a,b"}`, want: []string{"a,b"}},
	{name: "quoted escaped quote", src: `{"say \"hi\""}`, want: []string{`say "hi"`}},
	{name: "quoted escaped backslash", src: `{"c:\\temp"}`, want: []string{`c:\temp`}},
	{name: "quoted braces", src: `{"{x}"}`, want: []string{"{x}"}},
	{name: "quoted NULL is a string", src: `{"NULL"}`, want: []string{"NULL"}},
	{name: "empty string element", src: `{""}`, want: []string{""}},
	{name: "quoted whitespace kept", src: `{" a","b "}`, want: []string{" a", "b "}},
	{name: "bare whitespace kept", src: "{ a,b }", want: []string{" a", "b "}},
	{name: "utf-8", src: `{"héllo",日本語}`, want: []string{"héllo", "日本語"}},
	{name: "mixed bare and quoted", src: `{a,"b,c",d}`, want: []string{"a", "b,c", "d"}},
	{name: "bare NULL element rejected", src: "{NULL}", wantErr: true},
	{name: "bare NULL among others rejected", src: "{a,NULL}", wantErr: true},
	{name: "multidimensional rejected", src: "{{a},{b}}", wantErr: true},
	{name: "empty input", src: "", wantErr: true},
	{name: "missing open brace", src: "a}", wantErr: true},
	{name: "missing close brace", src: "{a", wantErr: true},
	{name: "trailing empty element", src: "{a,}", wantErr: true},
	{name: "leading empty element", src: "{,a}", wantErr: true},
	{name: "unterminated quote", src: `{"a`, wantErr: true},
	{name: "garbage after quote", src: `{"a"b}`, wantErr: true},
	{name: "garbage after close", src: "{a}}", wantErr: true},
	{name: "nested after element", src: "{a,{b}}", wantErr: true},
	{name: "unsupported source type", src: 42, wantErr: true},
}

func TestStringArrayValue(t *testing.T) {
	for _, tc := range stringCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := StringArray(tc.in).Value()
			if err != nil {
				t.Fatalf("Value() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("Value() = %#v, want %#v", got, tc.want)
			}
			viaArray, err := Array(tc.in).Value()
			if err != nil {
				t.Fatalf("Array().Value() error = %v", err)
			}
			if viaArray != tc.want {
				t.Fatalf("Array().Value() = %#v, want %#v", viaArray, tc.want)
			}
		})
	}
}

func TestFloat64ArrayValue(t *testing.T) {
	for _, tc := range floatCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Float64Array(tc.in).Value()
			if err != nil {
				t.Fatalf("Value() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("Value() = %#v, want %#v", got, tc.want)
			}
			viaArray, err := Array(tc.in).Value()
			if err != nil {
				t.Fatalf("Array().Value() error = %v", err)
			}
			if viaArray != tc.want {
				t.Fatalf("Array().Value() = %#v, want %#v", viaArray, tc.want)
			}
		})
	}
}

func TestQuoteIdentifier(t *testing.T) {
	for _, tc := range identCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := QuoteIdentifier(tc.in); got != tc.want {
				t.Fatalf("QuoteIdentifier(%q) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestStringArrayScan(t *testing.T) {
	for _, tc := range scanCases {
		t.Run(tc.name, func(t *testing.T) {
			var got StringArray
			err := got.Scan(tc.src)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Scan(%#v) = %#v, want error", tc.src, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Scan(%#v) error = %v", tc.src, err)
			}
			if !reflect.DeepEqual([]string(got), tc.want) {
				t.Fatalf("Scan(%#v) = %#v, want %#v", tc.src, []string(got), tc.want)
			}
		})
	}
}

// TestStringArrayValueScanRoundTrip closes the loop the tables above open
// one side at a time: whatever Value renders, Scan must read back verbatim.
func TestStringArrayValueScanRoundTrip(t *testing.T) {
	for _, tc := range stringCases {
		if tc.in == nil {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			v, err := StringArray(tc.in).Value()
			if err != nil {
				t.Fatalf("Value() error = %v", err)
			}
			var back StringArray
			if err := back.Scan(v); err != nil {
				t.Fatalf("Scan(%#v) error = %v", v, err)
			}
			if !reflect.DeepEqual([]string(back), tc.in) {
				t.Fatalf("round trip = %#v, want %#v", []string(back), tc.in)
			}
		})
	}
}

func TestFloat64ArrayScan(t *testing.T) {
	cases := []struct {
		name    string
		src     any
		want    []float64
		wantErr bool
	}{
		{name: "sql null", src: nil, want: nil},
		{name: "empty", src: "{}", want: []float64{}},
		{name: "values", src: "{0.1,-2,3e2}", want: []float64{0.1, -2, 300}},
		{name: "bytes", src: []byte("{1.5}"), want: []float64{1.5}},
		{name: "null element rejected", src: "{1,NULL}", wantErr: true},
		{name: "non-numeric rejected", src: "{abc}", wantErr: true},
		{name: "unsupported source", src: 1, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got Float64Array
			err := got.Scan(tc.src)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Scan(%#v) = %#v, want error", tc.src, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Scan(%#v) error = %v", tc.src, err)
			}
			if !reflect.DeepEqual([]float64(got), tc.want) {
				t.Fatalf("Scan(%#v) = %#v, want %#v", tc.src, []float64(got), tc.want)
			}
		})
	}
}

// TestArrayPointerAliasesCaller pins the Scan-through-pointer contract the
// read sites depend on: Array(&slice) writes into the caller's variable, and
// scanning an empty array into a non-nil slice truncates rather than
// reallocating.
func TestArrayPointerAliasesCaller(t *testing.T) {
	var strs []string
	if err := Array(&strs).Scan(`{"x","y"}`); err != nil {
		t.Fatalf("Scan error = %v", err)
	}
	if !reflect.DeepEqual(strs, []string{"x", "y"}) {
		t.Fatalf("strs = %#v, want [x y]", strs)
	}
	if err := Array(&strs).Scan("{}"); err != nil {
		t.Fatalf("Scan empty error = %v", err)
	}
	if strs == nil || len(strs) != 0 {
		t.Fatalf("strs after empty scan = %#v, want non-nil empty", strs)
	}
	if err := Array(&strs).Scan(nil); err != nil {
		t.Fatalf("Scan nil error = %v", err)
	}
	if strs != nil {
		t.Fatalf("strs after NULL scan = %#v, want nil", strs)
	}

	var floats []float64
	if err := Array(&floats).Scan("{0.5,1}"); err != nil {
		t.Fatalf("Scan error = %v", err)
	}
	if !reflect.DeepEqual(floats, []float64{0.5, 1}) {
		t.Fatalf("floats = %#v, want [0.5 1]", floats)
	}
}

// TestArrayRejectsUnsupportedTypes proves an element type this package does
// not encode fails at the statement, with the Go type named, instead of
// producing a literal nobody verified.
func TestArrayRejectsUnsupportedTypes(t *testing.T) {
	for _, in := range []any{[]int64{1}, []bool{true}, "not a slice", nil, []any{"x"}} {
		w := Array(in)
		if _, err := w.Value(); err == nil || !strings.Contains(err.Error(), "unsupported array type") {
			t.Fatalf("Array(%T).Value() error = %v, want unsupported-type error", in, err)
		}
		if err := w.Scan("{}"); err == nil || !strings.Contains(err.Error(), "unsupported array scan target") {
			t.Fatalf("Array(%T).Scan() error = %v, want unsupported-target error", in, err)
		}
	}
}
