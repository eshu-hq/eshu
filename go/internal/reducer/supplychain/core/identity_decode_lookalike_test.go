// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// TestSupplyChainDecodedTokensKeepLookalikesApart pins #9584 where the
// supply-chain reducer decodes and selects on identity tokens: a lookalike
// ecosystem, distro, package manager, vendor source, or PyPI package ID is
// handled like any unknown token, never as the ASCII token it imitates.
func TestSupplyChainDecodedTokensKeepLookalikesApart(t *testing.T) {
	t.Parallel()

	osPackage, err := supplyChainOSPackageFromEnvelope(facts.Envelope{
		FactID:   "os-lookalike",
		FactKind: chain.VulnerabilityOSPackageFactKind,
		Payload: map[string]any{
			"distro":                 "Deb\u0130an",
			"distro_version":         "12",
			"package_manager":        "dp\u212Ag",
			"name":                   "busybox",
			"arch":                   "amd64",
			"repository_class":       "vendor",
			"vendor_advisory_source": "deb\u0130an",
			"installed_version_raw":  "1.0-1",
		},
	})
	if err != nil {
		t.Fatalf("supplyChainOSPackageFromEnvelope() error = %v", err)
	}
	if osPackage.Distro == "debian" || osPackage.PackageManager == "dpkg" || osPackage.VendorAdvisorySource == "debian" {
		t.Fatalf("decoded OS package folded lookalike tokens: %+v", osPackage)
	}

	affected, err := supplyChainAffectedPackageFromEnvelope(facts.Envelope{
		FactID:        "affected-lookalike",
		FactKind:      chain.VulnerabilityAffectedPackageFactKind,
		SchemaVersion: chain.VulnerabilityIntelligenceSchemaVersionV1,
		Payload: map[string]any{
			"cve_id":       "CVE-2026-0001",
			"advisory_id":  "CVE-2026-0001",
			"ecosystem":    "pyp\u0130",
			"package_name": "requests",
		},
	})
	if err != nil {
		t.Fatalf("supplyChainAffectedPackageFromEnvelope() error = %v", err)
	}
	if affected.Ecosystem == "pypi" {
		t.Fatalf("decoded affected package folded a lookalike ecosystem to pypi")
	}

	if supplyChainImpactHasPyPIEvidence([]facts.Envelope{{
		FactKind: correlation.PackageConsumptionFactKind,
		Payload:  map[string]any{"package_id": "pkg:pyp\u0130/requests"},
	}}) {
		t.Fatal("supplyChainImpactHasPyPIEvidence read a lookalike PURL type as PyPI evidence")
	}
	if got, want := pythonPackageNameFromPackageID("pkg:pyp\u0130/requests"), pythonPackageNameFromPackageID("pkg:other/requests"); got != want {
		t.Fatalf("pythonPackageNameFromPackageID(lookalike) = %q, want the non-PyPI result %q", got, want)
	}

	if got, want := evaluateSupplyChainVersionMatch("pyp\u0130", "1.0.0", "", "2.0.0", nil),
		evaluateSupplyChainVersionMatch("unknown-ecosystem", "1.0.0", "", "2.0.0", nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("evaluateSupplyChainVersionMatch(lookalike) = %+v, want the unknown-ecosystem decision %+v", got, want)
	}

	candidates := func(ecosystem string) []supplychainmodel.AffectedPackage {
		return []supplychainmodel.AffectedPackage{
			{Ecosystem: ecosystem, Source: "debian", AdvisoryID: "DSA-0001-1"},
			{Ecosystem: ecosystem, Source: "osv", AdvisoryID: "GHSA-aaaa-bbbb-cccc"},
		}
	}
	if got, want := representativeAffectedPackage(candidates("Deb\u0130an")).AdvisoryID,
		representativeAffectedPackage(candidates("unknown-ecosystem")).AdvisoryID; got != want {
		t.Fatalf("representativeAffectedPackage(lookalike) picked %q, want the unknown-ecosystem pick %q", got, want)
	}
}
