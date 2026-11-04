// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/vulnerabilityintelligence"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// osvLiveFixtureDir holds live OSV records committed by the converter
// package. These tests drive them through the real converter so the reducer
// sees the exact purl and range shapes OSV publishes, not hand-made ones
// (#9848: hand-made purls hid that no OS advisory ever joined).
const osvLiveFixtureDir = "../../../collector/vulnerabilityintelligence/testdata/osv"

// convertedOSVFixture converts one committed OSV record with the in-tree
// converter and round-trips each payload through JSON, as the fact store
// does. reverse feeds the record's affected entries in reverse order.
func convertedOSVFixture(t testing.TB, name string, reverse bool) []facts.Envelope {
	t.Helper()
	return convertedOSVRecord(t, osvFixtureRecord(t, name), reverse)
}

// osvFixtureRecord decodes one committed OSV record, for a test that must
// change one field of a live record before converting it.
func osvFixtureRecord(t testing.TB, name string) vulnerabilityintelligence.OSVVulnerability {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(osvLiveFixtureDir, name))
	if err != nil {
		t.Fatalf("read OSV fixture %s: %v", name, err)
	}
	var record vulnerabilityintelligence.OSVVulnerability
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("decode OSV fixture %s: %v", name, err)
	}
	return record
}

// convertedOSVRecord converts record with the in-tree converter and
// round-trips each payload through JSON. reverse feeds the affected entries in
// reverse order; record itself is not modified.
func convertedOSVRecord(t testing.TB, record vulnerabilityintelligence.OSVVulnerability, reverse bool) []facts.Envelope {
	t.Helper()
	record.Affected = slices.Clone(record.Affected)
	if reverse {
		slices.Reverse(record.Affected)
	}
	envelopes, _, err := vulnerabilityintelligence.OSVRecordEnvelopesReport(vulnerabilityintelligence.OSVEnvelopeContext{
		ScopeID:             "vuln-intel://osv/fixture",
		GenerationID:        "osv:fixture:2026-11-02T00:00:00Z",
		CollectorInstanceID: "public-osv",
		ObservedAt:          time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
	}, record)
	if err != nil {
		t.Fatalf("convert OSV record %s: %v", record.ID, err)
	}
	for i := range envelopes {
		encoded, err := json.Marshal(envelopes[i].Payload)
		if err != nil {
			t.Fatalf("encode payload: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(encoded, &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		envelopes[i].Payload = payload
	}
	return envelopes
}

// installedOSRow is one vendor os_package row in the producer's documented
// shape: pkg:<type>/<vendor>/<name>@<version>?arch=<arch>&distro=<vendor>-<release>.
//
// source is the installed database's Source field (the source package the
// binary was built from, for example "openssl" for "libssl3"); empty means
// the row carries no source_package key, as rows written before #9859 do.
type installedOSRow struct {
	vendor, release, manager, name, arch, version string
	source                                        string
	// distro overrides the distro the row reports and its purl names, for a
	// derivative distribution of vendor. Empty means the vendor itself.
	distro string
}

func (r installedOSRow) envelope() facts.Envelope {
	purlType := "deb"
	switch r.manager {
	case "apk":
		purlType = "apk"
	case "rpm":
		purlType = "rpm"
	}
	distro := r.vendor
	if r.distro != "" {
		distro = r.distro
	}
	purl := "pkg:" + purlType + "/" + r.vendor + "/" + r.name + "@" + r.version +
		"?arch=" + r.arch + "&distro=" + distro + "-" + r.release
	payload := map[string]any{
		"distro":                 distro,
		"distro_version":         r.release,
		"package_manager":        r.manager,
		"name":                   r.name,
		"arch":                   r.arch,
		"repository_class":       "vendor",
		"vendor_advisory_source": r.vendor,
		"installed_version_raw":  r.version,
		"purl":                   purl,
	}
	if r.source != "" {
		payload["source_package"] = r.source
	}
	return osPackageFact("os-"+r.vendor+"-"+r.name, "image://registry.example/app@sha256:9848", payload)
}

// authorizedOSVAdvisories converts committed OSV records for the authorized
// per-target path. It keeps only the impact kinds the authorized loader
// selects (reference facts never enter that boundary) and returns the one
// public_registration dependency that authorizes them all.
func authorizedOSVAdvisories(tb testing.TB, names ...string) ([]facts.Envelope, []AdvisoryAuthorityDependency) {
	tb.Helper()
	var advisories []facts.Envelope
	for _, name := range names {
		for _, envelope := range convertedOSVFixture(tb, name, false) {
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
	return advisories, authority
}

// authorizedOSVTarget builds derived scan target i: one installed row and one
// completed analysis on image://derived/<i+1 as 64 hex digits>, sharing the
// given advisories and authority.
func authorizedOSVTarget(
	i int,
	advisories []facts.Envelope,
	authority []AdvisoryAuthorityDependency,
	row installedOSRow,
) AuthorizedScanImpactEvidence {
	target := fmt.Sprintf("image://derived/%064x", i+1)
	generation := fmt.Sprintf("scan-generation-%d", i)
	digest := fmt.Sprintf("sha256:%064x", i+1)
	osPackage := row.envelope()
	osPackage.FactID = fmt.Sprintf("os-%s-%d", row.name, i)
	osPackage.ScopeID = target
	osPackage.GenerationID = generation
	osPackage.SourceRef.SourceRecordID = target
	analysis := scannerWorkerAnalysisFact(target, generation, digest, "registry.example/app@"+digest)
	analysis.SourceRef.SourceRecordID = target
	return AuthorizedScanImpactEvidence{
		ScanTargetID: target, ScanGenerationID: generation,
		OSPackages: []facts.Envelope{osPackage}, Analyses: []facts.Envelope{analysis},
		Advisories: advisories, AuthorityDependencies: authority, Complete: true,
	}
}

func debianCurl(release, version string) installedOSRow {
	return installedOSRow{vendor: "debian", release: release, manager: "dpkg", name: "curl", arch: "amd64", version: version}
}

func ubuntuCurl(release, version string) installedOSRow {
	return installedOSRow{vendor: "ubuntu", release: release, manager: "dpkg", name: "curl", arch: "amd64", version: version}
}

func alpineCurl(release, version string) installedOSRow {
	return installedOSRow{vendor: "alpine", release: release, manager: "apk", name: "curl", arch: "x86_64", version: version}
}

func almaCurl(release, version string) installedOSRow {
	return installedOSRow{vendor: "alma", release: release, manager: "rpm", name: "curl", arch: "x86_64", version: version}
}

func rockyCurl(release, version string) installedOSRow {
	return installedOSRow{vendor: "rocky", release: release, manager: "rpm", name: "curl", arch: "x86_64", version: version}
}
