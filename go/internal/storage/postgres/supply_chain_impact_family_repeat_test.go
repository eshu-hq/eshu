// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"reflect"
	"testing"
)

// TestImpactFamilyRepeatClassifier is the hermetic RED/GREEN pair for the
// family repeat classifier: a pure function over per-execution stamps and
// attempt counts, with no database. An unstamped repeat fails, a stamped
// attempt-1 repeat passes (a legitimate fan-out reopen rerun: the reopen
// reset attempt_count to 0 and the claim incremented it to 1), and a stamped
// repeat at attempt 2 or later fails (a reclaim or double claim of the same
// cycle inherits the cycle's stamp).
func TestImpactFamilyRepeatClassifier(t *testing.T) {
	cases := []struct {
		name     string
		stamps   []bool
		attempts []int
		want     []int
	}{
		{
			name:     "single unstamped establishing pass is never unexpected",
			stamps:   []bool{false},
			attempts: []int{1},
			want:     nil,
		},
		{
			name:     "single stamped establishing pass is never unexpected",
			stamps:   []bool{true},
			attempts: []int{1},
			want:     nil,
		},
		{
			name:     "unstamped repeat fails",
			stamps:   []bool{false, false},
			attempts: []int{1, 2},
			want:     []int{1},
		},
		{
			name:     "unstamped repeat fails even at attempt 1",
			stamps:   []bool{false, false},
			attempts: []int{1, 1},
			want:     []int{1},
		},
		{
			name:     "stamped attempt-1 repeat passes",
			stamps:   []bool{false, true},
			attempts: []int{1, 1},
			want:     nil,
		},
		{
			name:     "stamped attempt-2 repeat fails",
			stamps:   []bool{false, true},
			attempts: []int{1, 2},
			want:     []int{1},
		},
		{
			name:     "stamped attempt-3 repeat fails",
			stamps:   []bool{true, true, true},
			attempts: []int{1, 1, 3},
			want:     []int{2},
		},
		{
			name:     "second repeat passes and third fails",
			stamps:   []bool{false, true, true},
			attempts: []int{1, 1, 2},
			want:     []int{2},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := unexpectedRepeatIndexes(tc.stamps, tc.attempts); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("unexpectedRepeatIndexes(%v, %v) = %v, want %v", tc.stamps, tc.attempts, got, tc.want)
			}
		})
	}
}
