// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped

import "testing"

// TestExtensionRowsPinsThe8002Allowance pins the density-gated extension
// allowance per route: 35,000 further ordered rows on entities (the #8002
// cost-2 shim fills a 201-match page inside 70,000 rows), zero on files so
// the file walk stays byte-identical.
func TestExtensionRowsPinsThe8002Allowance(t *testing.T) {
	t.Parallel()

	if got := entityShape.figures.extensionRows; got != 35_000 {
		t.Errorf("content_entities extensionRows = %d, want 35000", got)
	}
	if got := fileShape.figures.extensionRows; got != 0 {
		t.Errorf("content_files extensionRows = %d, want 0 (disabled)", got)
	}
}

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
