// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// osAdvisoryBenchRecords are the live OS records the join bench feeds: two
// Debian, one Alpine, two Ubuntu (one versionless purl, one versioned).
var osAdvisoryBenchRecords = []string{
	"debian-cve-2023-38039.json",
	"debian-cve-2003-1605.json",
	"alpine-cve-2023-27537.json",
	"ubuntu-cve-2022-27775.json",
	"ubuntu-cve-2016-5419.json",
}

// osAdvisoryBenchEnvelopes builds one pass: the converted advisory facts plus
// imagesPerVendor installed curl rows for each of debian, ubuntu and alpine,
// one row per image scope, versions spread across affected, ambiguous, and
// fixed so every decision branch runs.
func osAdvisoryBenchEnvelopes(b *testing.B, records []string, imagesPerVendor int, packageName string) []facts.Envelope {
	b.Helper()
	var envelopes []facts.Envelope
	for _, name := range records {
		envelopes = append(envelopes, convertedOSVFixture(b, name, false)...)
	}
	vendors := []struct {
		row      installedOSRow
		versions []string
	}{
		{debianCurl("12", ""), []string{"7.88.1-10+deb12u2", "7.88.1-10+deb12u3", "8.3.0-1"}},
		{ubuntuCurl("22.04", ""), []string{"7.68.0-1ubuntu2.7", "7.81.0-1ubuntu1", "7.81.0-1ubuntu1.1"}},
		{alpineCurl("3.18.4", ""), []string{"7.88.1-r0", "8.0.0-r0", "8.0.1-r0"}},
	}
	for _, vendor := range vendors {
		for i := 0; i < imagesPerVendor; i++ {
			row := vendor.row
			row.name = packageName
			row.version = vendor.versions[i%len(vendor.versions)]
			envelope := row.envelope()
			envelope.FactID = fmt.Sprintf("os-%s-%s-%d", row.vendor, packageName, i)
			envelope.ScopeID = fmt.Sprintf("image://registry.example/app-%d@sha256:%s%d", i, row.vendor, i)
			envelopes = append(envelopes, envelope)
		}
	}
	return envelopes
}

// BenchmarkOSAdvisoryJoin measures BuildSupplyChainImpactFindings over the
// live OS records at 1x, 5x and 10x installed rows per vendor (10, 50, 100
// images). ns/advisory divides by the advisory count. Before #9848 no row
// joins, so the base measures the miss path; after, the join, the range
// verdict, and the join count run for every advisory. Two controls join on
// neither tree: miss-10x installs a different package (wget) under the same
// vendors, registry-10x feeds an npm advisory; both must stay equal.
func BenchmarkOSAdvisoryJoin(b *testing.B) {
	for _, scale := range []struct {
		name    string
		records []string
		images  int
		pkg     string
	}{
		{"1x", osAdvisoryBenchRecords, 10, "curl"},
		{"5x", osAdvisoryBenchRecords, 50, "curl"},
		{"10x", osAdvisoryBenchRecords, 100, "curl"},
		{"miss-10x", osAdvisoryBenchRecords, 100, "wget"},
		{"registry-10x", []string{"ghsa-35jh-r3h4-6jhm.json"}, 100, "lodash"},
	} {
		b.Run(scale.name, func(b *testing.B) {
			envelopes := osAdvisoryBenchEnvelopes(b, scale.records, scale.images, scale.pkg)
			advisories := float64(len(scale.records))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = BuildSupplyChainImpactFindings(envelopes)
			}
			b.StopTimer()
			elapsed := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
			b.ReportMetric(elapsed/advisories, "ns/advisory")
		})
	}
}

// osAdvisoryBenchTargets builds count authorized scan targets for the
// per-target path. Each target holds one installed curl row (vendors and
// versions rotate as in osAdvisoryBenchEnvelopes) and one completed analysis;
// every target shares the five converted OSV records under one
// public_registration dependency, so the advisory index is built once and
// cached by advisoryIndexKey.
func osAdvisoryBenchTargets(b *testing.B, count int) []AuthorizedScanImpactEvidence {
	b.Helper()
	var advisories []facts.Envelope
	for _, name := range osAdvisoryBenchRecords {
		for _, envelope := range convertedOSVFixture(b, name, false) {
			// The authorized loader selects impact kinds only; reference
			// facts never enter this boundary.
			if authorizedImpactAdvisoryKind(envelope.FactKind) {
				advisories = append(advisories, envelope)
			}
		}
	}
	authority := []AdvisoryAuthorityDependency{{
		Kind: "public_registration", ID: "registration-osv", RevisionID: "revision-osv",
		AdvisoryScopeID: advisories[0].ScopeID, AdvisoryGenerationID: advisories[0].GenerationID,
		ConfigFingerprint: "config-osv", ProviderID: "osv", TrustedSourceClass: "osv",
		ContentProfile: "supply_chain_impact_v1",
	}}
	rows := []installedOSRow{
		debianCurl("12", "7.88.1-10+deb12u2"), debianCurl("12", "7.88.1-10+deb12u3"), debianCurl("12", "8.3.0-1"),
		ubuntuCurl("22.04", "7.68.0-1ubuntu2.7"), ubuntuCurl("22.04", "7.81.0-1ubuntu1"), ubuntuCurl("22.04", "7.81.0-1ubuntu1.1"),
		alpineCurl("3.18.4", "7.88.1-r0"), alpineCurl("3.18.4", "8.0.0-r0"), alpineCurl("3.18.4", "8.0.1-r0"),
	}
	targets := make([]AuthorizedScanImpactEvidence, 0, count)
	for i := 0; i < count; i++ {
		target := fmt.Sprintf("image://derived/%064x", i+1)
		generation := fmt.Sprintf("scan-generation-%d", i)
		digest := fmt.Sprintf("sha256:%064x", i+1)
		osPackage := rows[i%len(rows)].envelope()
		osPackage.FactID = fmt.Sprintf("os-curl-%d", i)
		osPackage.ScopeID = target
		osPackage.GenerationID = generation
		osPackage.SourceRef.SourceRecordID = target
		analysis := scannerWorkerAnalysisFact(target, generation, digest, "registry.example/app@"+digest)
		analysis.SourceRef.SourceRecordID = target
		targets = append(targets, AuthorizedScanImpactEvidence{
			ScanTargetID: target, ScanGenerationID: generation,
			OSPackages: []facts.Envelope{osPackage}, Analyses: []facts.Envelope{analysis},
			Advisories: advisories, AuthorityDependencies: authority, Complete: true,
		})
	}
	return targets
}

// BenchmarkOSAdvisoryJoinTargets measures BuildAuthorizedScanImpactFindings,
// the path that scales with images (one finding per target, #9848 ruling D
// Q4), at 10, 50 and 100 authorized targets. ns/target divides by the target
// count; a linear path keeps it flat across the rows.
func BenchmarkOSAdvisoryJoinTargets(b *testing.B) {
	for _, scale := range []struct {
		name    string
		targets int
	}{{"targets-1x", 10}, {"targets-5x", 50}, {"targets-10x", 100}} {
		b.Run(scale.name, func(b *testing.B) {
			targets := osAdvisoryBenchTargets(b, scale.targets)
			findings, err := BuildAuthorizedScanImpactFindings(nil, targets)
			if err != nil {
				b.Fatalf("authorized pass: %v", err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = BuildAuthorizedScanImpactFindings(nil, targets)
			}
			b.StopTimer()
			elapsed := float64(b.Elapsed().Nanoseconds()) / float64(b.N)
			b.ReportMetric(elapsed/float64(scale.targets), "ns/target")
			b.ReportMetric(float64(len(findings)), "findings")
		})
	}
}

// BenchmarkPURLPackageKey pins the advisory-side key derivation at zero
// allocations beside packageIDFromPURL on the same OSV purls.
func BenchmarkPURLPackageKey(b *testing.B) {
	purls := []string{
		"pkg:deb/debian/curl?arch=source&distro=bookworm",
		"pkg:apk/alpine/curl?arch=source",
		"pkg:deb/ubuntu/curl@7.58.0-2ubuntu3.15?arch=source&distro=bionic",
	}
	for _, fn := range []struct {
		name string
		key  func(string) string
	}{{"purlPackageKey", purlPackageKey}, {"packageIDFromPURL", packageIDFromPURL}} {
		b.Run(fn.name, func(b *testing.B) {
			b.ReportAllocs()
			var sink string
			for i := 0; i < b.N; i++ {
				sink = fn.key(purls[i%len(purls)])
			}
			_ = sink
		})
	}
}
