// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkBuildSupplyChainImpactIndexWithQuarantine measures the in-memory
// classification-index build for a representative per-scope-generation batch
// of vulnerability.* plus reducer package-consumption facts: one CVE, one
// affected_package, one reducer_package_consumption_correlation, and one
// os_package fact per synthetic advisory. This is the cold, once-per-scope-generation
// path buildSupplyChainImpactIndexWithQuarantine feeds
// BuildSupplyChainImpactFindings/SupplyChainImpactHandler.Handle (not a hot
// per-edge loop), so the typed-decode cost is measured for a no-regression
// bound rather than a tight microbench, mirroring the incident family's
// BenchmarkBuildIncidentRoutingEvidenceInputs precedent.
func BenchmarkBuildSupplyChainImpactIndexWithQuarantine(b *testing.B) {
	const advisoryCount = 2000
	envelopes := make([]facts.Envelope, 0, advisoryCount*4)
	for i := 0; i < advisoryCount; i++ {
		cveID := fmt.Sprintf("CVE-2026-%05d", i)
		packageID := fmt.Sprintf("pkg:npm/bench-package-%d", i)
		envelopes = append(envelopes, facts.Envelope{
			FactID:   fmt.Sprintf("cve-%d", i),
			FactKind: facts.VulnerabilityCVEFactKind,
			Payload: map[string]any{
				"cve_id":         cveID,
				"advisory_id":    cveID,
				"source":         "osv",
				"cvss_score":     7.5,
				"cvss_vector":    "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N",
				"severity_label": "HIGH",
				"published_at":   "2026-01-01T00:00:00Z",
				"modified_at":    "2026-01-02T00:00:00Z",
			},
		})
		envelopes = append(envelopes, facts.Envelope{
			FactID:   fmt.Sprintf("affected-package-%d", i),
			FactKind: facts.VulnerabilityAffectedPackageFactKind,
			Payload: map[string]any{
				"cve_id":            cveID,
				"advisory_id":       cveID,
				"source":            "osv",
				"package_id":        packageID,
				"ecosystem":         "npm",
				"package_name":      fmt.Sprintf("bench-package-%d", i),
				"purl":              fmt.Sprintf("pkg:npm/bench-package-%d@1.0.0", i),
				"affected_versions": []any{"1.0.0"},
				"fixed_versions":    []any{"1.0.1"},
			},
		})
		envelopes = append(envelopes, facts.Envelope{
			FactID:   fmt.Sprintf("package-consumption-%d", i),
			FactKind: packageConsumptionCorrelationFactKind,
			Payload: map[string]any{
				"package_id":       packageID,
				"repository_id":    fmt.Sprintf("repo-%d", i),
				"dependency_range": "^1.0.0",
				"observed_version": "1.0.0",
				"dependency_path":  []any{fmt.Sprintf("bench-package-%d", i)},
				"dependency_depth": 1,
			},
		})
		envelopes = append(envelopes, facts.Envelope{
			FactID:   fmt.Sprintf("os-package-%d", i),
			FactKind: facts.VulnerabilityOSPackageFactKind,
			ScopeID:  fmt.Sprintf("image://registry.example/bench-app-%d@sha256:deadbeef", i),
			Payload: map[string]any{
				"distro":                 "debian",
				"distro_version":         "12",
				"package_manager":        "dpkg",
				"name":                   fmt.Sprintf("bench-package-%d", i),
				"arch":                   "amd64",
				"repository_class":       "vendor",
				"vendor_advisory_source": "debian",
				"installed_version_raw":  "1.0.0",
				"purl":                   fmt.Sprintf("pkg:deb/debian/bench-package-%d@1.0.0?arch=amd64&distro=debian-12", i),
			},
		})
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		index, quarantined, err := buildSupplyChainImpactIndexWithQuarantine(envelopes)
		if err != nil {
			b.Fatalf("buildSupplyChainImpactIndexWithQuarantine() error = %v, want nil", err)
		}
		if len(quarantined) != 0 {
			b.Fatalf("len(quarantined) = %d, want 0", len(quarantined))
		}
		if len(index.cves) != advisoryCount {
			b.Fatalf("len(index.cves) = %d, want %d", len(index.cves), advisoryCount)
		}
	}
}
