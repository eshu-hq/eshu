// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
	"fmt"
	"strings"
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

// stampFrozenOSVRecord loads one frozen template record and rewrites the
// id-derived strings per (from, to) pairs, so benchmarks scale record counts
// without re-running conversion. FactID and stable-key hashes keep the
// template values; benchmarks measure classification cost, never identity.
// Callers pass pairs that name disjoint id families (template ids always
// start at zero), so no replacement ever rewrites another pair's output.
func stampFrozenOSVRecord(tb testing.TB, key string, pairs ...string) []facts.Envelope {
	tb.Helper()
	if len(pairs)%2 != 0 {
		tb.Fatalf("stampFrozenOSVRecord needs (from, to) pairs")
	}
	envs := convertedOSVFixture(tb, key, false)
	raw, err := json.Marshal(envs)
	if err != nil {
		tb.Fatalf("marshal frozen fixture %s: %v", key, err)
	}
	stamped := string(raw)
	for i := 0; i < len(pairs); i += 2 {
		stamped = strings.ReplaceAll(stamped, pairs[i], pairs[i+1])
	}
	var out []facts.Envelope
	if err := json.Unmarshal([]byte(stamped), &out); err != nil {
		tb.Fatalf("decode stamped fixture %s: %v", key, err)
	}
	return out
}

// upstreamBundleEnvelopes builds records shaped like a live USN bulletin: each
// names cves upstream CVEs and affects pkgs packages. USN-6766-1 names 90 CVEs.
// With hosts set it adds one installed row per package.
func upstreamBundleEnvelopes(tb testing.TB, records, cves, pkgs int, hosts bool) []facts.Envelope {
	tb.Helper()
	if pkgs != 3 {
		tb.Fatalf("upstreamBundleEnvelopes supports the frozen 3-package shape, got pkgs=%d", pkgs)
	}
	var out []facts.Envelope
	for r := 0; r < records; r++ {
		pairs := []string{"USN-6000-1", fmt.Sprintf("USN-%d-1", 6000+r)}
		for c := 0; c < cves; c++ {
			pairs = append(pairs,
				fmt.Sprintf("CVE-2024-%05d", c), fmt.Sprintf("CVE-2024-%05d", r*cves+c))
		}
		out = append(out, stampFrozenOSVRecord(tb, fmt.Sprintf("built-bench-usn-cves-%d", cves), pairs...)...)
	}
	if hosts {
		for p := 0; p < pkgs; p++ {
			row := ubuntuCurl("22.04", "5.15.0-90.100")
			row.name = fmt.Sprintf("linux-%d", p)
			out = append(out, row.envelope())
		}
	}
	return out
}

// BenchmarkBuildSupplyChainImpactFindingsUpstreamBundle measures the whole
// classification over bulletins naming 1, 10 and 90 CVEs. Findings grow with the
// CVE count by design: one per CVE per installed package. The reported findings
// metric lets the per-finding cost be read from the same run.
func BenchmarkBuildSupplyChainImpactFindingsUpstreamBundle(b *testing.B) {
	for _, cves := range []int{1, 10, 90} {
		envelopes := upstreamBundleEnvelopes(b, 100, cves, 3, true)
		b.Run(fmt.Sprintf("records=100_cves=%d_pkgs=3", cves), func(b *testing.B) {
			b.ReportAllocs()
			var findings int
			for i := 0; i < b.N; i++ {
				findings = len(BuildSupplyChainImpactFindings(envelopes))
			}
			b.ReportMetric(float64(findings), "findings")
		})
	}
}

// BenchmarkBuildSupplyChainImpactIndexUpstreamGrouping measures the index
// build with and without the OS upstream grouping pass active. untagged is the
// guard: a fleet with no OS vendor record and no upstream list must not move.
// The bundle cells are the fan-out: 300 bulletins of 3 packages naming 1, 10 and
// 90 CVEs. The #9781 evidence budget charges loaded envelopes and retained payload
// bytes, not these copies, so this is where their cost is measured.
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
	for _, cves := range []int{1, 10, 90} {
		envelopes := upstreamBundleEnvelopes(b, 300, cves, 3, false)
		b.Run(fmt.Sprintf("bundle_records=300_cves=%d_pkgs=3", cves), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, _, err := buildSupplyChainImpactIndexWithQuarantine(envelopes); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkBuildSupplyChainImpactFindingsTwoLiveRecords measures the whole
// classification when every CVE has two live records for the installed package,
// the only shape where osHoldingFacts does more than return. One finding per CVE.
func BenchmarkBuildSupplyChainImpactFindingsTwoLiveRecords(b *testing.B) {
	for _, cves := range []int{100, 1000} {
		var envelopes []facts.Envelope
		for at := range cves {
			cve := fmt.Sprintf("CVE-2026-%05d", at)
			envelopes = append(envelopes, stampFrozenOSVRecord(b, "built-bench-dla-0",
				"DLA-0-1", fmt.Sprintf("DLA-%d-1", at), "CVE-2026-00000", cve)...)
			envelopes = append(envelopes, stampFrozenOSVRecord(b, "built-bench-dsa-0",
				"DSA-0-1", fmt.Sprintf("DSA-%d-1", at), "CVE-2026-00000", cve)...)
		}
		envelopes = append(envelopes, debianCurl("11", "7.74.0-1.3+deb11u11").envelope())
		b.Run(fmt.Sprintf("cves=%d", cves), func(b *testing.B) {
			b.ReportAllocs()
			var findings int
			for i := 0; i < b.N; i++ {
				findings = len(BuildSupplyChainImpactFindings(envelopes))
			}
			b.ReportMetric(float64(findings), "findings")
		})
	}
}
