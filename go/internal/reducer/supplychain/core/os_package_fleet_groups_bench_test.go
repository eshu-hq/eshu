// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkOSFleetDigestlessFanout measures BuildSupplyChainImpactFindings over
// one OS advisory meeting 10, 50, and 100 digest-less rows (#9984). same-group
// puts every row on one (release line, installed version), so the pass still
// classifies once and pays only the partition. spread rotates three lines and
// three versions, the most groups these rows can form, so it classifies nine
// times. ns/row divides by the row count; a linear path keeps it flat.
func BenchmarkOSFleetDigestlessFanout(b *testing.B) {
	versions := []string{"7.88.1-10+deb12u2", "7.88.1-10+deb12u3", "8.3.0-1"}
	lines := []string{"11", "12", "13"}
	for _, scale := range []struct {
		name   string
		rows   int
		spread bool
	}{
		{"same-group-1x", 10, false},
		{"same-group-5x", 50, false},
		{"same-group-10x", 100, false},
		{"spread-1x", 10, true},
		{"spread-5x", 50, true},
		{"spread-10x", 100, true},
	} {
		b.Run(scale.name, func(b *testing.B) {
			envelopes := convertedOSVFixture(b, "debian-cve-2023-38039.json", false)
			for i := range scale.rows {
				row := debianCurl("12", versions[0])
				if scale.spread {
					row = debianCurl(lines[i%len(lines)], versions[(i/len(lines))%len(versions)])
				}
				envelopes = append(envelopes, digestlessBenchRow(row, i))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = BuildSupplyChainImpactFindings(envelopes)
			}
			b.StopTimer()
			elapsed := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
			b.ReportMetric(elapsed/float64(scale.rows), "ns/row")
		})
	}
}

// digestlessBenchRow gives row its own scope and generation and no sibling
// analysis, so it lands in the digest-less base.
func digestlessBenchRow(row installedOSRow, i int) facts.Envelope {
	envelope := row.envelope()
	envelope.FactID = fmt.Sprintf("os-bench-%d", i)
	envelope.ScopeID = fmt.Sprintf("image://fleet/bench-%d", i)
	envelope.GenerationID = fmt.Sprintf("fleet-bench-gen-%d", i)
	return envelope
}
