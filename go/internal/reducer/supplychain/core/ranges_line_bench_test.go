// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// BenchmarkNPMKnownFixedPerLine measures the npm matcher for the #9854 shape:
// a blank scalar fix and an observed version above every release line, the
// worst case because every line is read and none contains the version. The
// 1x corpus is 4 lines per advisory (the js-yaml-like multi-line shape); 5x
// and 10x scale the ranges per advisory. "scalar" is the prior path (and the
// path the other ecosystems keep); "per_line" adds the per-line branch.
func BenchmarkNPMKnownFixedPerLine(b *testing.B) {
	for _, scale := range []int{1, 5, 10} {
		ranges := make([]supplychainmodel.AffectedRange, 0, 4*scale)
		for line := 0; line < 4*scale; line++ {
			ranges = append(ranges, semverLine(fmt.Sprintf("%d.0.0", line), fmt.Sprintf("%d.5.0", line)))
		}
		pkgs := affectedPackageWithRanges(ranges...)
		observed := fmt.Sprintf("%d.9.0", 4*scale)
		for _, mode := range []struct {
			name    string
			perLine bool
		}{{"scalar", false}, {"per_line", true}} {
			b.Run(fmt.Sprintf("%dx/%s", scale, mode.name), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					decision := evaluateSemverFamilyMatch(observed, "", pkgs, mode.perLine)
					if mode.perLine && decision.Status != SupplyChainImpactNotAffectedKnownFixed {
						b.Fatalf("status = %q, want known-fixed", decision.Status)
					}
				}
			})
		}
	}
}
