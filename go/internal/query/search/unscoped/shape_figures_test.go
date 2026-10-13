// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped

import "testing"

// TestShapeFiguresArePositive pins that every shape's figures are positive.
// The engine divides by probeRows when it predicts the first continuation
// step, so a zero there would panic at run time instead of failing a test.
func TestShapeFiguresArePositive(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		f    figures
	}{
		{"content_files", fileShape.figures},
		{"content_entities", entityShape.figures},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.f.probeRows <= 0 || tc.f.stepRows <= 0 {
				t.Errorf("window sizes must be positive, got probeRows=%d stepRows=%d", tc.f.probeRows, tc.f.stepRows)
			}
			if tc.f.phaseOneCap <= 0 || tc.f.tailCap <= 0 || tc.f.tailFloor <= 0 {
				t.Errorf("phase limits must be positive, got phaseOneCap=%v tailCap=%v tailFloor=%v", tc.f.phaseOneCap, tc.f.tailCap, tc.f.tailFloor)
			}
		})
	}
}
