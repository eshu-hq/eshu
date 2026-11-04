// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// upstreamBenchEnvelopes builds a fleet-shaped advisory batch: plain counts of
// untagged language advisories (the hot path every non-OS fleet takes), OS
// distro records without an upstream list, and OS bulletins that name one
// upstream CVE each beside that CVE's NVD record.
func upstreamBenchEnvelopes(untagged, osUngrouped, osGrouped int) []facts.Envelope {
	out := make([]facts.Envelope, 0, 2*untagged+2*osUngrouped+3*osGrouped)
	pair := func(id, cveID, source, ecosystem, packageID, purl string, upstream []any) {
		cve := map[string]any{"cve_id": cveID, "advisory_id": id, "source": source}
		if upstream != nil {
			cve["upstream"] = upstream
		}
		out = append(out,
			facts.Envelope{FactID: "cve-" + id, FactKind: chain.VulnerabilityCVEFactKind, Payload: cve},
			facts.Envelope{FactID: "aff-" + id, FactKind: chain.VulnerabilityAffectedPackageFactKind, Payload: map[string]any{
				"cve_id": cveID, "advisory_id": id, "source": source, "ecosystem": ecosystem,
				"package_id": packageID, "package_name": "pkg", "purl": purl,
				"affected_versions": []any{"1.0.0"}, "fixed_versions": []any{"1.0.1"},
			}})
	}
	for i := 0; i < untagged; i++ {
		id := fmt.Sprintf("GHSA-%04d", i)
		pair(id, fmt.Sprintf("CVE-2024-%05d", i), "osv", "npm", "npm://pkg"+id, "pkg:npm/pkg"+id, nil)
	}
	for i := 0; i < osUngrouped; i++ {
		id := fmt.Sprintf("DEBIAN-CVE-2023-%05d", i)
		pair(id, id, "osv", "os", "os://debian/curl", "pkg:deb/debian/curl?arch=source&distro=bookworm", nil)
	}
	for i := 0; i < osGrouped; i++ {
		cveID := fmt.Sprintf("CVE-2022-%05d", i)
		out = append(out, nvdCVEFact(cveID))
		id := fmt.Sprintf("DSA-%05d-1", i)
		pair(id, id, "osv", "os", "os://debian/curl", "pkg:deb/debian/curl?arch=source&distro=bookworm", []any{cveID, "DEBIAN-" + cveID})
	}
	return out
}

// BenchmarkBuildSupplyChainImpactIndexUpstreamGrouping measures the index
// build with and without the OS upstream grouping pass active. untagged is the
// guard: a fleet with no OS vendor record and no upstream list must not move.
func BenchmarkBuildSupplyChainImpactIndexUpstreamGrouping(b *testing.B) {
	for _, tc := range []struct {
		name                             string
		untagged, osUngrouped, osGrouped int
	}{
		{"untagged_20k", 20000, 0, 0},
		{"os_ungrouped_5k", 0, 5000, 0},
		{"os_grouped_5k", 0, 0, 5000},
		{"mixed_20k_2k_2k", 20000, 2000, 2000},
	} {
		envelopes := upstreamBenchEnvelopes(tc.untagged, tc.osUngrouped, tc.osGrouped)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, _, err := buildSupplyChainImpactIndexWithQuarantine(envelopes); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
