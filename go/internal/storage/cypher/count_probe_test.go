// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "testing"

// TestCountProbeIntValue pins the shared Bolt integer conversion both the
// projector and ingester executors use: one implementation, one behavior.
func TestCountProbeIntValue(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		in     any
		want   int64
		wantOK bool
	}{
		{"int64", int64(42), 42, true},
		{"int", int(42), 42, true},
		{"int32", int32(42), 42, true},
		{"float64", float64(42), 42, true},
		{"string", "42", 0, false},
		{"nil", nil, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, ok := CountProbeIntValue(tc.in); got != tc.want || ok != tc.wantOK {
				t.Fatalf("CountProbeIntValue(%v) = (%d, %v), want (%d, %v)", tc.in, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
