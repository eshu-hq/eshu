// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// This file holds fixture-builder and assertion helpers for
// supply_chain_impact_version_match_test.go, split out to keep both files
// under the repo's 500-line cap.

// vulnerabilityAffectedPackageMavenRangeFact builds a
// vulnerability.affected_package fixture. advisory_id is set equal to cveID:
// every real collector source always sets it, so a fixture without it was
// never realistic collector output.
func vulnerabilityAffectedPackageMavenRangeFact(
	factID string,
	cveID string,
	packageID string,
	affectedRange string,
	fixedVersion string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":         cveID,
			"advisory_id":    cveID,
			"package_id":     packageID,
			"ecosystem":      "maven",
			"package_name":   "org.apache.maven:maven-core",
			"affected_range": affectedRange,
			"fixed_versions": []any{fixedVersion},
		},
	}
}

// vulnerabilityAffectedPackageMalformedRangeFact builds a
// vulnerability.affected_package fixture. advisory_id is set equal to cveID:
// every real collector source always sets it, so a fixture without it was
// never realistic collector output.
func vulnerabilityAffectedPackageMalformedRangeFact(
	factID string,
	cveID string,
	packageID string,
	ecosystem string,
	name string,
	introduced string,
	fixedVersion string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":       cveID,
			"advisory_id":  cveID,
			"package_id":   packageID,
			"ecosystem":    ecosystem,
			"package_name": name,
			"fixed_versions": []any{
				fixedVersion,
			},
			"affected_ranges": []any{
				map[string]any{
					"type": "SEMVER",
					"events": []any{
						map[string]any{"introduced": introduced},
						map[string]any{"fixed": fixedVersion},
					},
				},
			},
		},
	}
}

// vulnerabilityAffectedPackageRawRangeFact builds a
// vulnerability.affected_package fixture. advisory_id is set equal to cveID:
// every real collector source always sets it, so a fixture without it was
// never realistic collector output.
func vulnerabilityAffectedPackageRawRangeFact(
	factID string,
	cveID string,
	packageID string,
	ecosystem string,
	name string,
	affectedRange string,
	fixedVersion string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":         cveID,
			"advisory_id":    cveID,
			"package_id":     packageID,
			"ecosystem":      ecosystem,
			"package_name":   name,
			"affected_range": affectedRange,
			"fixed_versions": []any{fixedVersion},
		},
	}
}

func assertContainsString(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return
		}
	}
	t.Fatalf("%#v does not contain %q", values, want)
}

func compareSign(value int) int {
	switch {
	case value < 0:
		return -1
	case value > 0:
		return 1
	default:
		return 0
	}
}
