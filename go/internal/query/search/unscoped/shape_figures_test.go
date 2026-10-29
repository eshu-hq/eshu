// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped

import (
	"testing"
	"time"
)

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

// TestNewPlanScalesLargeBudgetsWithoutOverflow pins the #9306 CI failure:
// at an explicit 10 s budget the nanosecond product atDefault*budget
// overflows int64, wrapping the phase caps negative so the walk ends in the
// probe with ended_in=budget. Scaling in whole milliseconds keeps every
// MaxBudget product far inside int64 while matching the nanosecond quotient
// exactly for the millisecond budgets the API accepts.
func TestNewPlanScalesLargeBudgetsWithoutOverflow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                            string
		f                               figures
		phaseOneCap, tailCap, tailFloor time.Duration
	}{
		{"content_files", fileShape.figures, 5500 * time.Millisecond, 5000 * time.Millisecond, 1875 * time.Millisecond},
		{"content_entities", entityShape.figures, 2000 * time.Millisecond, 8000 * time.Millisecond, 1875 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := newPlan(MaxBudget, tc.f)
			if p.phaseOneCap != tc.phaseOneCap || p.tailCap != tc.tailCap || p.tailFloor != tc.tailFloor {
				t.Errorf("newPlan(MaxBudget) = %v/%v/%v, want %v/%v/%v (overflow wraps caps negative)",
					p.phaseOneCap, p.tailCap, p.tailFloor, tc.phaseOneCap, tc.tailCap, tc.tailFloor)
			}
		})
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
