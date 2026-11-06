// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"strings"
	"testing"
)

// The CVE List v5 is a well-formed readiness source for owned-CVE targets
// (eshu-hq/eshu#9880): the malformed check admits it, a target with no CVE
// ids or with a modified window is malformed (owned CVEs only, never a
// window or corpus read), and a CVE-selected cve_list target is a readiness
// candidate. It never certifies empty coverage: only an exact NVD CVE
// selector does that, and the CVE List is class C/D enrichment, not
// findings. The expected descriptor stays the NVD coverage descriptor,
// consumed only for coverage-eligible (NVD-only) targets.
func TestReadinessTargetsAdmitCVEListOwnedCVEs(t *testing.T) {
	t.Parallel()

	invalid, configured := readinessTargetsSections(t)
	if !strings.Contains(invalid, "'cve_list'") {
		t.Fatal("invalid_vulnerability_source_targets does not admit the cve_list source")
	}
	cveListArms := 0
	for _, line := range strings.Split(invalid, "\n") {
		if strings.Contains(line, "'cve_list'") {
			cveListArms++
		}
	}
	if cveListArms < 2 {
		t.Fatalf("invalid CTE names cve_list %d time(s), want the allow-list plus the owned-CVE validity arm", cveListArms)
	}
	if !strings.Contains(configured, "selector.source = 'cve_list' AND selector.cve_selected") {
		t.Fatal("configured_vulnerability_source_targets has no cve_list CVE-selected candidate arm")
	}
	// coverage_eligible must be defined from the NVD selector only: every
	// eligibility definition in the configured CTE must reference 'nvd'
	// and none may admit 'cve_list', so widening the definition to admit
	// cve_list (or dropping the NVD definition) fails this test.
	nvdDefined := false
	for _, line := range strings.Split(configured, "\n") {
		if !strings.Contains(line, "AS coverage_eligible") {
			continue
		}
		if strings.Contains(line, "cve_list") {
			t.Fatalf("coverage_eligible admits cve_list: %q; only NVD certifies empty coverage", strings.TrimSpace(line))
		}
		if strings.Contains(line, "'nvd'") {
			nvdDefined = true
		}
	}
	if !nvdDefined {
		t.Fatal("no coverage_eligible definition references 'nvd'; the NVD-only eligibility definition is missing")
	}
}
