// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pgarray

import (
	"reflect"
	"testing"

	"github.com/lib/pq"
)

// These differential tests exist only while github.com/lib/pq is still in the
// module. They prove, in one process, that this package's driver values and
// scan results are byte-for-byte what lib/pq produced for the same inputs,
// and that the frozen expectations in pgarray_test.go were captured from pq
// rather than from this package's own output. The file is deleted in the
// commit that drops the dependency.

func TestDifferentialStringArrayValue(t *testing.T) {
	for _, tc := range stringCases {
		t.Run(tc.name, func(t *testing.T) {
			ref, err := pq.StringArray(tc.in).Value()
			if err != nil {
				t.Fatalf("pq.StringArray.Value() error = %v", err)
			}
			refArr, err := pq.Array(tc.in).Value()
			if err != nil {
				t.Fatalf("pq.Array().Value() error = %v", err)
			}
			got, err := StringArray(tc.in).Value()
			if err != nil {
				t.Fatalf("StringArray.Value() error = %v", err)
			}
			if got != ref || refArr != ref {
				t.Fatalf("mismatch: pgarray=%#v pq.StringArray=%#v pq.Array=%#v", got, ref, refArr)
			}
			if ref != tc.want {
				t.Fatalf("frozen expectation %#v does not match pq output %#v", tc.want, ref)
			}
		})
	}
}

func TestDifferentialFloat64ArrayValue(t *testing.T) {
	for _, tc := range floatCases {
		t.Run(tc.name, func(t *testing.T) {
			ref, err := pq.Float64Array(tc.in).Value()
			if err != nil {
				t.Fatalf("pq.Float64Array.Value() error = %v", err)
			}
			got, err := Float64Array(tc.in).Value()
			if err != nil {
				t.Fatalf("Float64Array.Value() error = %v", err)
			}
			if got != ref {
				t.Fatalf("mismatch: pgarray=%#v pq=%#v", got, ref)
			}
			if ref != tc.want {
				t.Fatalf("frozen expectation %#v does not match pq output %#v", tc.want, ref)
			}
		})
	}
}

func TestDifferentialQuoteIdentifier(t *testing.T) {
	for _, tc := range identCases {
		t.Run(tc.name, func(t *testing.T) {
			ref := pq.QuoteIdentifier(tc.in)
			if got := QuoteIdentifier(tc.in); got != ref {
				t.Fatalf("mismatch: pgarray=%s pq=%s", got, ref)
			}
			if ref != tc.want {
				t.Fatalf("frozen expectation %s does not match pq output %s", tc.want, ref)
			}
		})
	}
}

// TestDifferentialStringArrayScan compares decoded values and, just as
// importantly, error/no-error agreement on malformed input.
func TestDifferentialStringArrayScan(t *testing.T) {
	for _, tc := range scanCases {
		t.Run(tc.name, func(t *testing.T) {
			var ref pq.StringArray
			refErr := ref.Scan(tc.src)
			var got StringArray
			gotErr := got.Scan(tc.src)
			if (refErr != nil) != (gotErr != nil) {
				t.Fatalf("error disagreement: pgarray=%v pq=%v", gotErr, refErr)
			}
			if refErr != nil {
				if !tc.wantErr {
					t.Fatalf("table says no error but pq errored: %v", refErr)
				}
				return
			}
			if tc.wantErr {
				t.Fatalf("table says error but pq decoded %#v", ref)
			}
			if !reflect.DeepEqual([]string(got), []string(ref)) {
				t.Fatalf("mismatch: pgarray=%#v pq=%#v", []string(got), []string(ref))
			}
			if !reflect.DeepEqual([]string(ref), tc.want) {
				t.Fatalf("frozen expectation %#v does not match pq output %#v", tc.want, []string(ref))
			}
		})
	}
}

// TestDifferentialFloat64ArrayScan covers the vector read path.
func TestDifferentialFloat64ArrayScan(t *testing.T) {
	for _, src := range []any{nil, "{}", "{0.1,-2,3e2}", []byte("{1.5}"), "{1,NULL}", "{abc}", "{{1},{2}}", "{1", 1} {
		var ref pq.Float64Array
		refErr := ref.Scan(src)
		var got Float64Array
		gotErr := got.Scan(src)
		if (refErr != nil) != (gotErr != nil) {
			t.Fatalf("%#v: error disagreement: pgarray=%v pq=%v", src, gotErr, refErr)
		}
		if refErr == nil && !reflect.DeepEqual([]float64(got), []float64(ref)) {
			t.Fatalf("%#v: mismatch: pgarray=%#v pq=%#v", src, got, ref)
		}
	}
}
