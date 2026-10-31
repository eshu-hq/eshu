// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// retractionBenchEnvelopes builds groups CVE groups, each with three source
// observations (NVD, GHSA, OSV) and one affected package, so the benchmark
// walks the per-group retraction decision a realistic number of times. With
// status set, every observation carries it; with it empty the group is live.
func retractionBenchEnvelopes(groups int, status string) []facts.Envelope {
	envelopes := make([]facts.Envelope, 0, groups*5)
	for i := 0; i < groups; i++ {
		cveID := fmt.Sprintf("CVE-2026-%05d", i)
		packageID := fmt.Sprintf("npm://registry.npmjs.org/widget-%d", i)
		for _, source := range []struct{ name, advisory string }{
			{"nvd", cveID},
			{"osv", fmt.Sprintf("GHSA-%04d-aaaa-bbbb", i)},
			{"osv", fmt.Sprintf("OSV-%05d", i)},
		} {
			cve := vulnerabilityCVEFactWithProvenance(
				fmt.Sprintf("cve-%d-%s", i, source.advisory), cveID, source.name, source.advisory,
				7.5, "", "HIGH", "2026-05-18T10:00:00Z",
			)
			if status != "" {
				cve.Payload["status"] = status
			}
			envelopes = append(envelopes, cve)
		}
		envelopes = append(envelopes,
			vulnerabilityAffectedPackageFactWithSource(
				fmt.Sprintf("affected-%d", i), cveID, "osv", fmt.Sprintf("GHSA-%04d-aaaa-bbbb", i),
				packageID, "npm", fmt.Sprintf("widget-%d", i), "1.0.0", "1.0.1",
			),
			packageConsumptionFactWithRange(fmt.Sprintf("consume-%d", i), packageID, testImpactRepositoryID, "1.0.0"),
		)
	}
	return envelopes
}

// BenchmarkBuildFindingsMultiSourceGroups measures the whole finding build over
// 500 three-source CVE groups, live and fully retracted, so the cost of the
// per-group retraction decision (#9695) shows against the build it rides on.
func BenchmarkBuildFindingsMultiSourceGroups(b *testing.B) {
	for name, status := range map[string]string{"live": "", "all_rejected": "Rejected"} {
		envelopes := retractionBenchEnvelopes(500, status)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if got := len(BuildSupplyChainImpactFindings(envelopes)); got == 0 {
					b.Fatal("no findings built")
				}
			}
		})
	}
}
