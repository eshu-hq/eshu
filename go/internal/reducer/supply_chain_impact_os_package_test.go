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
		rpmOSPackageFact("rpm-os-openssl-ambiguous", map[string]any{
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
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityOSPackageFactKind,
		ScopeID:  "image://registry.example/rhel-app@sha256:1045",
		Payload:  payload,
	}
}
