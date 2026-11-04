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

// BenchmarkOSFleetSBOMBaseUnattachedDigests measures the SBOM base the fleet
// pass now also classifies (#9984): one verified SBOM subject beside N image
// digests that have scan rows and no SBOM, all on one release line and installed
// version. Each unattached digest is its own group, so the finding count is N+1
// at this change and 2 before it, and the cost is linear in N. ns/digest divides
// by N; a linear path keeps it flat. The scale names match the digest-less
// benchmark: 1x is one digest, 10x is ten, and so on.
func BenchmarkOSFleetSBOMBaseUnattachedDigests(b *testing.B) {
	packageID := vendorLineScenarios[0].packageID
	attached := fmt.Sprintf("sha256:%064x", 0xa11)
	for _, scale := range []struct {
		name    string
		digests int
	}{
		{"1x", 1},
		{"10x", 10},
		{"100x", 100},
		{"1000x", 1000},
	} {
		b.Run(scale.name, func(b *testing.B) {
			envelopes := []facts.Envelope{
				vulnerabilityCVEFact("bench-cve", vendorLineCVE, 7.5),
				withDistroLine(vendorLineScenarios[0].affectedEnvelope(""), "debian-11"),
			}
			envelopes = append(envelopes,
				sbomComponentImpactFactWithPackageIDVersion("bench-component", "bench-doc", packageID+"@1:1.0-3", packageID, "1:1.0-3"),
				sbomAttachmentImpactFact("bench-attachment", "bench-doc", attached),
				containerImageIdentityImpactFact("bench-image", attached, "repository:bench"),
			)
			for i := range scale.digests {
				row := digestlessBenchRow(debianCurl("11", "1:1.0-3"), i)
				digest := fmt.Sprintf("sha256:%064x", 0xb000+i)
				envelopes = append(envelopes, row,
					scannerWorkerAnalysisFact(row.ScopeID, row.GenerationID, digest, "registry.example/bench@"+digest))
			}
			b.ReportAllocs()
			b.ResetTimer()
			var findings int
			for i := 0; i < b.N; i++ {
				findings = len(BuildSupplyChainImpactFindings(envelopes))
			}
			b.StopTimer()
			elapsed := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
			b.ReportMetric(elapsed/float64(scale.digests), "ns/digest")
			b.ReportMetric(float64(findings), "findings")
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
