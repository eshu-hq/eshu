// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"
)

// BenchmarkOSFleetDigestVersions measures BuildSupplyChainImpactFindings over
// one OS advisory meeting 10, 50, and 100 image digests that each hold 1 or 3
// installed versions of the package (#10057). A digest classifies once per
// installed version, so the finding count is digests x versions and the cost
// must stay linear in it: ns/finding divides by that count, and a linear path
// keeps it flat. versions-1 is the shape the digest path had before the change
// (one row per digest), so it is the no-regression read for the common case.
func BenchmarkOSFleetDigestVersions(b *testing.B) {
	versions := []string{"7.88.1-10+deb12u2", "7.88.1-10+deb12u3", "8.3.0-1"}
	for _, perDigest := range []int{1, 3} {
		for _, digests := range []int{10, 50, 100} {
			b.Run(fmt.Sprintf("versions-%d/digests-%d", perDigest, digests), func(b *testing.B) {
				envelopes := convertedOSVFixture(b, "file-debian-cve-2023-38039", false)
				for i := range digests {
					scope := fmt.Sprintf("image://fleet/versions-%d", i)
					generation := fmt.Sprintf("versions-gen-%d", i)
					digest := fmt.Sprintf("sha256:%064x", i+1)
					envelopes = append(envelopes, scannerWorkerAnalysisFact(scope, generation, digest, "registry.example/app@"+digest))
					for v := range perDigest {
						envelopes = append(envelopes, digestVersionRow(versions[v], fmt.Sprintf("os-versions-%d-%d", i, v), scope, generation))
					}
				}
				want := digests * perDigest
				if got := BuildSupplyChainImpactFindings(envelopes); len(got) != want {
					b.Fatalf("findings = %d, want one per digest and version (%d)", len(got), want)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_ = BuildSupplyChainImpactFindings(envelopes)
				}
				b.StopTimer()
				elapsed := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
				b.ReportMetric(elapsed/float64(want), "ns/finding")
			})
		}
	}
}
