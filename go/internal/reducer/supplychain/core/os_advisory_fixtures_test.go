// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"encoding/json"
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
type installedOSRow struct {
	vendor, release, manager, name, arch, version string
}

func (r installedOSRow) envelope() facts.Envelope {
	purlType := "deb"
	if r.manager == "apk" {
		purlType = "apk"
	}
	purl := "pkg:" + purlType + "/" + r.vendor + "/" + r.name + "@" + r.version +
		"?arch=" + r.arch + "&distro=" + r.vendor + "-" + r.release
	return osPackageFact("os-"+r.vendor+"-"+r.name, "image://registry.example/app@sha256:9848", map[string]any{
		"distro":                 r.vendor,
		"distro_version":         r.release,
		"package_manager":        r.manager,
		"name":                   r.name,
		"arch":                   r.arch,
		"repository_class":       "vendor",
		"vendor_advisory_source": r.vendor,
		"installed_version_raw":  r.version,
		"purl":                   purl,
	})
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
