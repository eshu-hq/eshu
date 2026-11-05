// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import "testing"

func TestSourceForScopeIDMapsEveryScopeAuthority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		scopeID string
		want    string
		wantOK  bool
	}{
		{scopeID: "vuln-intel://osv/npm/_batch?query=lodash%404.17.11", want: SourceOSV, wantOK: true},
		{scopeID: "vuln-intel://osv/npm/lodash?version=4.17.11", want: SourceOSV, wantOK: true},
		{scopeID: "vuln-intel://osv/pypi", want: SourceOSV, wantOK: true},
		{scopeID: "vuln-intel://nvd/cve", want: SourceNVD, wantOK: true},
		{scopeID: "vuln-intel://nvd/CVE-2021-23337", want: SourceNVD, wantOK: true},
		{scopeID: "vuln-intel://cisa/kev", want: SourceCISAKEV, wantOK: true},
		{scopeID: "vuln-intel://first/epss", want: SourceFirstEPSS, wantOK: true},
		// The coordinator derives EPSS finding scopes under the first_epss
		// authority (#10009).
		{scopeID: "vuln-intel://first_epss/_findings/0", want: SourceFirstEPSS, wantOK: true},
		{scopeID: "vuln-intel://first_epss/_findings/17", want: SourceFirstEPSS, wantOK: true},
		{scopeID: "vuln-intel://glad/npm", want: SourceGLAD, wantOK: true},
		{scopeID: "vuln-intel://ghsa/reviewed", want: SourceGHSA, wantOK: true},
		{scopeID: "vuln-intel://ghsa/malware", want: SourceGHSA, wantOK: true},
		{scopeID: "vuln-intel://ghsa/reviewed?modified=1", want: SourceGHSA, wantOK: true},
		{scopeID: "  vuln-intel://cisa/kev  ", want: SourceCISAKEV, wantOK: true},
		// Unknown or malformed scopes never invent a source token.
		{scopeID: "vuln-intel://cisa/other"},
		{scopeID: "vuln-intel://first/other"},
		{scopeID: "vuln-intel://first-epss/_findings/0"},
		{scopeID: "vuln-intel://first_epss"},
		{scopeID: "vuln-intel://first_epss/other"},
		{scopeID: "vuln-intel://First_EPSS/_findings/0"},
		{scopeID: "vuln-intel://cisa"},
		{scopeID: "vuln-intel://ghsa/npm"},
		{scopeID: "vuln-intel://ghsa/unreviewed"},
		{scopeID: "vuln-intel://ghsa/reviewed/npm"},
		{scopeID: "vuln-intel://ghsa/malware/"},
		{scopeID: "vuln-intel://ghsa/reviewedx"},
		{scopeID: "vuln-intel://ghsa/Reviewed"},
		{scopeID: "vuln-intel://ghsa"},
		{scopeID: "vuln-intel://ghsa/"},
		{scopeID: "vuln-intel://"},
		{scopeID: "https://osv.dev/npm"},
		{scopeID: "osv/npm"},
		{scopeID: ""},
		{scopeID: "VULN-INTEL://osv/npm"},
		{scopeID: "vuln-intel://OSV/npm"},
	}
	for _, tc := range tests {
		got, ok := SourceForScopeID(tc.scopeID)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("SourceForScopeID(%q) = (%q, %t), want (%q, %t)", tc.scopeID, got, ok, tc.want, tc.wantOK)
		}
	}
}
