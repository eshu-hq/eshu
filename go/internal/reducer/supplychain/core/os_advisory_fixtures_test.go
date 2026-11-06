// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	chain "github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// Frozen OSV/NVD converter output (eshu-hq/eshu#9999). The in-tree collector
// is deleted, and the satellite owns conversion now, so core tests cannot run
// the converter at test time. The JSON files under testdata/osv-converted are
// the in-tree converter's output frozen at the delete branch's base commit:
// every envelope below is byte-identical to what the helpers in this file
// produced from the live records before the delete, including the payload
// round-trip the fact store performs. The shapes stay producer-real (the
// #9848 lesson: hand-made purls hid that no OS advisory ever joined), frozen
// instead of live.
//
// A frozen key is either "file-<name>" for a live-record conversion or
// "built-<case>" for a hand-built record conversion. The ".reversed" variant
// holds the same record converted with its affected entries reversed.
func convertedOSVFixture(t testing.TB, key string, reverse bool) []facts.Envelope {
	t.Helper()
	path := key
	if reverse {
		path += ".reversed"
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "osv-converted", path+".json"))
	if err != nil {
		t.Fatalf("read frozen OSV fixture %s: %v", path, err)
	}
	var envelopes []facts.Envelope
	if err := json.Unmarshal(raw, &envelopes); err != nil {
		t.Fatalf("decode frozen OSV fixture %s: %v", path, err)
	}
	return envelopes
}

// osvFixtureUpstream returns the upstream id list of one live fixture file,
// frozen alongside the converted output. Sibling tests re-key records to
// these ids, so the ids stay visible after the live files are gone.
func osvFixtureUpstream(t testing.TB, name string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "osv-converted", "_upstream.json"))
	if err != nil {
		t.Fatalf("read frozen OSV upstream index: %v", err)
	}
	var index map[string][]string
	if err := json.Unmarshal(raw, &index); err != nil {
		t.Fatalf("decode frozen OSV upstream index: %v", err)
	}
	lookup := name
	if rest, ok := strings.CutPrefix(name, "file-"); ok {
		lookup = rest + ".json"
	}
	upstream, ok := index[lookup]
	if !ok {
		t.Fatalf("no frozen upstream for %s", name)
	}
	return upstream
}

// osvFixtureFirstEntryPURL returns the kept purl of the frozen conversion of
// one live fixture file. The merge-order pin uses it to assert the versioned
// Ubuntu shape is still what the ruling reasons about.
func osvFixtureFirstEntryPURL(t testing.TB, key string) string {
	t.Helper()
	for _, envelope := range convertedOSVFixture(t, key, false) {
		if envelope.FactKind != chain.VulnerabilityAffectedPackageFactKind {
			continue
		}
		purl, _ := envelope.Payload["purl"].(string)
		return purl
	}
	t.Fatalf("no affected_package envelope in frozen fixture %s", key)
	return ""
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
