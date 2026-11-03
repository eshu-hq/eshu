// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// listedEventsVulnerable is the walk before #9865, kept here only as the
// "before" reference for the benchmark: events applied in listed order.
func listedEventsVulnerable(
	events []supplychainmodel.AffectedRangeEvent,
	observed string,
	compare versionCompareFunc,
) (bool, bool) {
	vulnerable := false
	for _, event := range events {
		switch {
		case event.Introduced != "":
			if ok, valid := versionAtLeast(observed, event.Introduced, compare); !valid {
				return false, false
			} else if ok {
				vulnerable = true
			}
		case event.Fixed != "":
			if ok, valid := versionAtLeast(observed, event.Fixed, compare); !valid {
				return false, false
			} else if ok {
				vulnerable = false
			}
		case event.LastAffected != "":
			if ok, valid := versionGreaterThan(observed, event.LastAffected, compare); !valid {
				return false, false
			} else if ok {
				vulnerable = false
			}
		}
	}
	return vulnerable, true
}

// BenchmarkRangeEventContainment measures one advisory's containment check for
// #9865: an observed version above every range, the worst case because every
// event is triggered and compared with the running best. The 1x corpus is 4
// two-event ranges per advisory; 5x and 10x scale the ranges. "listed_before"
// is the prior walk; "version_order_after" is the shipped one.
func BenchmarkRangeEventContainment(b *testing.B) {
	for _, scale := range []int{1, 5, 10} {
		ranges := make([]supplychainmodel.AffectedRange, 0, 4*scale)
		for line := 0; line < 4*scale; line++ {
			ranges = append(ranges, semverLine(fmt.Sprintf("%d.0.0", line), fmt.Sprintf("%d.5.0", line)))
		}
		observed := fmt.Sprintf("%d.9.0", 4*scale)
		b.Run(fmt.Sprintf("%dx/listed_before", scale), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				for _, r := range ranges {
					if _, ok := listedEventsVulnerable(r.Events, observed, compareOSVSemver); !ok {
						b.Fatal("malformed")
					}
				}
			}
		})
		b.Run(fmt.Sprintf("%dx/version_order_after", scale), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				for _, r := range ranges {
					if _, ok := semverRangeContainsDecision(r, observed); !ok {
						b.Fatal("malformed")
					}
				}
			}
		})
	}
}
