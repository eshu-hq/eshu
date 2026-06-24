// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsUsesVendorRPMOSPackageEvidence(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFactWithProvenance(
			"redhat-cve-rpm",
			"CVE-2026-1045",
			"redhat",
			"RHSA-2026:1045",
			7.8,
			"CVSS:3.1/AV:L/AC:L/PR:L/UI:N/S:U/C:H/I:H/A:H",
			"HIGH",
			"2026-05-31T12:00:00Z",
		),
		vulnerabilityAffectedPackageFactWithSource(
			"redhat-affected-rpm",
			"CVE-2026-1045",
			"redhat",
			"RHSA-2026:1045",
			"pkg:rpm/redhat/openssl",
			"redhat",
			"openssl",
			"1:3.0.7-18.el9_2",
			"1:3.0.7-20.el9_2",
		),
		rpmOSPackageFact("rpm-os-openssl", map[string]any{
			"distro":                 "redhat",
			"distro_version":         "9.2",
			"package_manager":        "rpm",
			"name":                   "openssl",
			"epoch":                  "1",
			"upstream_version":       "3.0.7",
			"distro_release":         "18.el9_2",
			"arch":                   "x86_64",
			"source_package":         "openssl",
			"repository_class":       "vendor",
			"vendor_advisory_source": "redhat",
			"installed_version_raw":  "1:3.0.7-18.el9_2",
			"purl":                   "pkg:rpm/redhat/openssl@1:3.0.7-18.el9_2?arch=x86_64&distro=redhat-9.2",
		}),
	})

	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1 RPM-backed finding: %#v", len(findings), findings)
	}
	got := findings[0]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.ObservedVersion != "1:3.0.7-18.el9_2" {
		t.Fatalf("ObservedVersion = %q, want RPM EVR from os package fact", got.ObservedVersion)
	}
	if got.RuntimeReachability != "image_os_package" {
		t.Fatalf("RuntimeReachability = %q, want image_os_package", got.RuntimeReachability)
	}
	if got.FixedVersionSource != "redhat" {
		t.Fatalf("FixedVersionSource = %q, want redhat", got.FixedVersionSource)
	}
	if got.DetectionProfile != DetectionProfilePrecise {
		t.Fatalf("DetectionProfile = %q, want precise", got.DetectionProfile)
	}
	path := strings.Join(got.EvidencePath, " -> ")
	if !strings.Contains(path, facts.VulnerabilityOSPackageFactKind) {
		t.Fatalf("EvidencePath = %#v, want os package evidence", got.EvidencePath)
	}
}

func TestBuildSupplyChainImpactFindingsUsesVendorDPKGOSPackageEvidence(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFactWithProvenance(
			"debian-cve-dpkg",
			"CVE-2026-2045",
			"debian",
			"DSA-2026-2045",
			7.5,
			"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N",
			"HIGH",
			"2026-05-31T12:00:00Z",
		),
		vulnerabilityAffectedPackageFactWithSource(
			"debian-affected-dpkg",
			"CVE-2026-2045",
			"debian",
			"DSA-2026-2045",
			"pkg:deb/debian/openssl",
			"deb",
			"openssl",
			"3.0.11-1~deb12u2",
			"3.0.11-1~deb12u3",
		),
		osPackageFact("dpkg-os-openssl", "image://registry.example/debian-app@sha256:2045", map[string]any{
			"distro":                 "debian",
			"distro_version":         "12",
			"package_manager":        "dpkg",
			"name":                   "openssl",
			"arch":                   "amd64",
			"repository_class":       "vendor",
			"vendor_advisory_source": "debian",
			"installed_version_raw":  "3.0.11-1~deb12u2",
			"purl":                   "pkg:deb/debian/openssl@3.0.11-1~deb12u2?arch=amd64&distro=debian-12",
		}),
	})

	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1 DPKG-backed finding: %#v", len(findings), findings)
	}
	got := findings[0]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.RuntimeReachability != "image_os_package" {
		t.Fatalf("RuntimeReachability = %q, want image_os_package", got.RuntimeReachability)
	}
	if got.FixedVersionSource != "debian" {
		t.Fatalf("FixedVersionSource = %q, want debian", got.FixedVersionSource)
	}
}

func TestBuildSupplyChainImpactFindingsUsesVendorAPKOSPackageEvidence(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFactWithProvenance(
			"alpine-cve-apk",
			"CVE-2026-3045",
			"alpine",
			"ALPINE-2026-3045",
			8.1,
			"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:N",
			"HIGH",
			"2026-05-31T12:00:00Z",
		),
		vulnerabilityAffectedPackageFactWithSource(
			"alpine-affected-apk",
			"CVE-2026-3045",
			"alpine",
			"ALPINE-2026-3045",
			"pkg:apk/alpine/openssl",
			"apk",
			"openssl",
			"3.1.4-r5",
			"3.1.4-r6",
		),
		osPackageFact("apk-os-openssl", "image://registry.example/alpine-app@sha256:3045", map[string]any{
			"distro":                 "alpine",
			"distro_version":         "3.19.1",
			"package_manager":        "apk",
			"name":                   "openssl",
			"arch":                   "x86_64",
			"repository_class":       "vendor",
			"vendor_advisory_source": "alpine",
			"installed_version_raw":  "3.1.4-r5",
			"purl":                   "pkg:apk/alpine/openssl@3.1.4-r5?arch=x86_64&distro=alpine-3.19.1",
		}),
	})

	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1 APK-backed finding: %#v", len(findings), findings)
	}
	got := findings[0]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.ObservedVersion != "3.1.4-r5" {
		t.Fatalf("ObservedVersion = %q, want apk installed version", got.ObservedVersion)
	}
	if got.RuntimeReachability != "image_os_package" {
		t.Fatalf("RuntimeReachability = %q, want image_os_package", got.RuntimeReachability)
	}
}

func TestBuildSupplyChainImpactFindingsUsesCollectorShapedOSVDebianAndAPKRanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		cveID            string
		advisoryID       string
		packageID        string
		purl             string
		packageManager   string
		distro           string
		distroVersion    string
		arch             string
		installedVersion string
		fixedVersion     string
		wantStatus       SupplyChainImpactStatus
		wantReason       string
		wantSource       string
	}{
		{
			name:             "debian vulnerable below fixed branch",
			cveID:            "CVE-2026-119303",
			advisoryID:       "DSA-2026-119303",
			packageID:        "os://debian/openssl",
			purl:             "pkg:deb/debian/openssl@3.0.11-1~deb12u2",
			packageManager:   "dpkg",
			distro:           "debian",
			distroVersion:    "12",
			arch:             "amd64",
			installedVersion: "3.0.11-1~deb12u2",
			fixedVersion:     "3.0.11-1~deb12u3",
			wantStatus:       SupplyChainImpactAffectedExact,
			wantReason:       "dpkg_affected_range",
			wantSource:       "debian",
		},
		{
			name:             "debian safe at fixed branch",
			cveID:            "CVE-2026-119304",
			advisoryID:       "DSA-2026-119304",
			packageID:        "os://debian/openssl",
			purl:             "pkg:deb/debian/openssl@3.0.11-1~deb12u3",
			packageManager:   "dpkg",
			distro:           "debian",
			distroVersion:    "12",
			arch:             "amd64",
			installedVersion: "3.0.11-1~deb12u3",
			fixedVersion:     "3.0.11-1~deb12u3",
			wantStatus:       SupplyChainImpactNotAffectedKnownFixed,
			wantReason:       "dpkg_known_fixed",
			wantSource:       "debian",
		},
		{
			name:             "apk vulnerable below fixed branch",
			cveID:            "CVE-2026-119305",
			advisoryID:       "ALPINE-2026-119305",
			packageID:        "os://alpine/openssl",
			purl:             "pkg:apk/alpine/openssl@3.1.4-r5",
			packageManager:   "apk",
			distro:           "alpine",
			distroVersion:    "3.19.1",
			arch:             "x86_64",
			installedVersion: "3.1.4-r5",
			fixedVersion:     "3.1.4-r6",
			wantStatus:       SupplyChainImpactAffectedExact,
			wantReason:       "apk_affected_range",
			wantSource:       "alpine",
		},
		{
			name:             "apk safe above fixed branch",
			cveID:            "CVE-2026-119306",
			advisoryID:       "ALPINE-2026-119306",
			packageID:        "os://alpine/openssl",
			purl:             "pkg:apk/alpine/openssl@3.1.4-r7",
			packageManager:   "apk",
			distro:           "alpine",
			distroVersion:    "3.19.1",
			arch:             "x86_64",
			installedVersion: "3.1.4-r7",
			fixedVersion:     "3.1.4-r6",
			wantStatus:       SupplyChainImpactNotAffectedKnownFixed,
			wantReason:       "apk_known_fixed",
			wantSource:       "alpine",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			findings := BuildSupplyChainImpactFindings([]facts.Envelope{
				vulnerabilityCVEFactWithProvenance(
					"osv-"+tc.cveID,
					tc.cveID,
					"osv",
					tc.advisoryID,
					7.5,
					"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N",
					"HIGH",
					"2026-06-01T12:00:00Z",
				),
				vulnerabilityAffectedPackageOSVRangeFact(
					"affected-"+tc.cveID,
					tc.cveID,
					tc.advisoryID,
					tc.packageID,
					tc.purl,
					"openssl",
					tc.fixedVersion,
				),
				osPackageFact("os-package-"+tc.cveID, "image://registry.example/osv-remediation@sha256:"+tc.cveID, map[string]any{
					"distro":                 tc.distro,
					"distro_version":         tc.distroVersion,
					"package_manager":        tc.packageManager,
					"name":                   "openssl",
					"arch":                   tc.arch,
					"repository_class":       "vendor",
					"vendor_advisory_source": tc.wantSource,
					"installed_version_raw":  tc.installedVersion,
					"purl":                   tc.purl,
				}),
			})

			if len(findings) != 1 {
				t.Fatalf("findings = %d, want one collector-shaped OSV OS package finding: %#v", len(findings), findings)
			}
			got := findings[0]
			assertSupplyChainImpactStatus(t, got, tc.wantStatus)
			if got.Ecosystem != "os" {
				t.Fatalf("Ecosystem = %q, want collector-shaped os", got.Ecosystem)
			}
			if got.MatchReason != tc.wantReason {
				t.Fatalf("MatchReason = %q, want %q", got.MatchReason, tc.wantReason)
			}
			if got.ObservedVersion != tc.installedVersion {
				t.Fatalf("ObservedVersion = %q, want %q", got.ObservedVersion, tc.installedVersion)
			}
			if got.FixedVersionSource != tc.wantSource {
				t.Fatalf("FixedVersionSource = %q, want %q", got.FixedVersionSource, tc.wantSource)
			}
			if got.Remediation.FixedVersionSource != tc.wantSource {
				t.Fatalf("Remediation.FixedVersionSource = %q, want %q", got.Remediation.FixedVersionSource, tc.wantSource)
			}
			if got.Status == SupplyChainImpactAffectedExact &&
				got.Remediation.Reason != SupplyChainRemediationReasonDirectUpgradeAllowed {
				t.Fatalf("Remediation.Reason = %q, want direct upgrade for vulnerable OS package", got.Remediation.Reason)
			}
			if got.Status == SupplyChainImpactNotAffectedKnownFixed &&
				got.Remediation.Reason == SupplyChainRemediationReasonDirectUpgradeAllowed {
				t.Fatalf("Remediation.Reason = %q, safe package must not recommend an affected upgrade", got.Remediation.Reason)
			}
			if got.Status == SupplyChainImpactNotAffectedKnownFixed &&
				got.Remediation.Reason != SupplyChainRemediationReasonAlreadyFixed {
				t.Fatalf("Remediation.Reason = %q, want already_fixed for safe package", got.Remediation.Reason)
			}
		})
	}
}

func TestBuildSupplyChainImpactFindingsRejectsLanguageAdvisoryForDPKGOSPackage(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFactWithProvenance(
			"ghsa-cve-dpkg",
			"CVE-2026-2046",
			"ghsa",
			"GHSA-2026-2046",
			7.5,
			"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N",
			"HIGH",
			"2026-05-31T12:00:00Z",
		),
		vulnerabilityAffectedPackageFactWithSource(
			"ghsa-affected-dpkg",
			"CVE-2026-2046",
			"ghsa",
			"GHSA-2026-2046",
			"pkg:deb/debian/openssl",
			"deb",
			"openssl",
			"3.0.11-1~deb12u2",
			"3.0.11-1~deb12u3",
		),
		osPackageFact("dpkg-os-openssl-ghsa", "image://registry.example/debian-app@sha256:2046", map[string]any{
			"distro":                 "debian",
			"distro_version":         "12",
			"package_manager":        "dpkg",
			"name":                   "openssl",
			"arch":                   "amd64",
			"repository_class":       "vendor",
			"vendor_advisory_source": "debian",
			"installed_version_raw":  "3.0.11-1~deb12u2",
			"purl":                   "pkg:deb/debian/openssl@3.0.11-1~deb12u2?arch=amd64&distro=debian-12",
		}),
	})

	if got := len(findings); got != 0 {
		t.Fatalf("findings = %d, want 0 because GHSA language advisory must not apply to Debian package truth: %#v", got, findings)
	}
}

func TestBuildSupplyChainImpactFindingsSkipsAmbiguousRPMOSPackageEvidence(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFactWithProvenance(
			"redhat-cve-rpm-ambiguous",
			"CVE-2026-1046",
			"redhat",
			"RHSA-2026:1046",
			7.8,
			"CVSS:3.1/AV:L/AC:L/PR:L/UI:N/S:U/C:H/I:H/A:H",
			"HIGH",
			"2026-05-31T12:00:00Z",
		),
		vulnerabilityAffectedPackageFactWithSource(
			"redhat-affected-rpm-ambiguous",
			"CVE-2026-1046",
			"redhat",
			"RHSA-2026:1046",
			"pkg:rpm/redhat/openssl",
			"redhat",
			"openssl",
			"1:3.0.7-18.el9_2",
			"1:3.0.7-20.el9_2",
		),
		osPackageFact("rpm-os-openssl-ambiguous", "image://registry.example/rhel-app@sha256:1045", map[string]any{
			"distro":                 "redhat",
			"distro_version":         "9.2",
			"package_manager":        "rpm",
			"name":                   "openssl",
			"epoch":                  "1",
			"upstream_version":       "3.0.7",
			"distro_release":         "18.el9_2",
			"arch":                   "x86_64",
			"repository_class":       "unknown",
			"vendor_advisory_source": "",
			"installed_version_raw":  "1:3.0.7-18.el9_2",
			"purl":                   "pkg:rpm/redhat/openssl@1:3.0.7-18.el9_2?arch=x86_64&distro=redhat-9.2",
		}),
	})

	if len(findings) != 0 {
		t.Fatalf("findings = %d, want 0 because ambiguous RPM origin must not produce impact truth: %#v",
			len(findings), findings)
	}
}

func rpmOSPackageFact(factID string, payload map[string]any) facts.Envelope {
	return osPackageFact(factID, "image://registry.example/rhel-app@sha256:1045", payload)
}

func osPackageFact(factID string, scopeID string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityOSPackageFactKind,
		ScopeID:  scopeID,
		Payload:  payload,
	}
}

func vulnerabilityAffectedPackageOSVRangeFact(
	factID string,
	cveID string,
	advisoryID string,
	packageID string,
	purl string,
	name string,
	fixedVersion string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":         cveID,
			"advisory_id":    advisoryID,
			"source":         "osv",
			"package_id":     packageID,
			"ecosystem":      "os",
			"package_name":   name,
			"purl":           purl,
			"fixed_versions": []any{fixedVersion},
			"affected_ranges": []any{
				map[string]any{
					"type": "ECOSYSTEM",
					"events": []any{
						map[string]any{"introduced": "0"},
						map[string]any{"fixed": fixedVersion},
					},
				},
			},
		},
	}
}
