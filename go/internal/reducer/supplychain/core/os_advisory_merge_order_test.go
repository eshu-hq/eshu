// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestOSVUbuntuJoinIgnoresWhichEntryPurlTheMergeKeeps pins ruling C.1 for
// #9848. The converter merges a package's release entries into one fact and
// keeps one entry's purl. Before #9848 an Ubuntu fact joined an installed row
// only when that kept purl carried an @version, so the answer depended on
// OSV's per-entry purl choice and on entry order.
//
// The mixed record is the live UBUNTU-CVE-2016-5419 with the version removed
// from its trusty entry's purl, the versionless shape OSV also publishes for
// Ubuntu. The merge sorts entries by purl and '?' sorts before '@', so the
// mixed fact keeps the versionless purl beside a versioned sibling, and the
// unmodified record keeps a versioned one; the precondition pins both. Each
// record is fed in both entry orders, and every shape and order must give the
// same answer.
func TestOSVUbuntuJoinIgnoresWhichEntryPurlTheMergeKeeps(t *testing.T) {
	t.Parallel()

	const (
		trustyVersioned   = "pkg:deb/ubuntu/curl@7.35.0-1ubuntu2.8?arch=source&distro=trusty"
		trustyVersionless = "pkg:deb/ubuntu/curl?arch=source&distro=trusty"
	)
	versioned := osvFixtureRecord(t, "ubuntu-cve-2016-5419.json")
	if got := versioned.Affected[0].Package.PURL; got != trustyVersioned {
		t.Fatalf("fixture drift: first entry purl = %q, want %q", got, trustyVersioned)
	}
	mixed := osvFixtureRecord(t, "ubuntu-cve-2016-5419.json")
	mixed.Affected[0].Package.PURL = trustyVersionless

	shapes := []struct {
		name          string
		reverse       bool
		wantVersioned bool
	}{
		{name: "mixed", reverse: false, wantVersioned: false},
		{name: "mixed, entries reversed", reverse: true, wantVersioned: false},
		{name: "versioned", reverse: false, wantVersioned: true},
		{name: "versioned, entries reversed", reverse: true, wantVersioned: true},
	}
	rows := []struct {
		name       string
		installed  installedOSRow
		wantStatus SupplyChainImpactStatus
		wantReason string
	}{
		{"trusty below its fix", ubuntuCurl("14.04", "7.35.0-1ubuntu2.7"), SupplyChainImpactPossiblyAffected, "os_release_unverified"},
		{"trusty at its fix", ubuntuCurl("14.04", "7.35.0-1ubuntu2.8"), SupplyChainImpactPossiblyAffected, "os_release_ambiguous"},
		{"xenial at its fix", ubuntuCurl("16.04", "7.47.0-1ubuntu2.1"), SupplyChainImpactNotAffectedKnownFixed, "dpkg_known_fixed"},
	}
	for _, shape := range shapes {
		record := versioned
		if strings.HasPrefix(shape.name, "mixed") {
			record = mixed
		}
		for _, row := range rows {
			t.Run(shape.name+"/"+row.name, func(t *testing.T) {
				t.Parallel()

				advisory := convertedOSVRecord(t, record, shape.reverse)
				if got := keptAffectedPackagePURLHasVersion(t, advisory); got != shape.wantVersioned {
					t.Fatalf("precondition: kept affected purl versioned = %v, want %v", got, shape.wantVersioned)
				}
				findings := BuildSupplyChainImpactFindings(append(advisory, row.installed.envelope()))
				if len(findings) != 1 {
					t.Fatalf("findings = %d, want 1 OS package finding", len(findings))
				}
				assertSupplyChainImpactStatus(t, findings[0], row.wantStatus)
				if findings[0].MatchReason != row.wantReason {
					t.Fatalf("MatchReason = %q, want %q", findings[0].MatchReason, row.wantReason)
				}
			})
		}
	}
}

// keptAffectedPackagePURLHasVersion reports whether the one affected-package
// fact the converter emitted kept a purl with an @version.
func keptAffectedPackagePURLHasVersion(t *testing.T, envelopes []facts.Envelope) bool {
	t.Helper()
	var purls []string
	for _, envelope := range envelopes {
		if envelope.FactKind != "vulnerability.affected_package" {
			continue
		}
		purl, _ := envelope.Payload["purl"].(string)
		purls = append(purls, purl)
	}
	if len(purls) != 1 {
		t.Fatalf("affected package facts = %d (%v), want 1 merged fact", len(purls), purls)
	}
	return strings.Contains(purls[0], "@")
}
