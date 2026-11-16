// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replay/recordpseudo"
)

// TestPublicRegistryHostsStayReadable (P2): hosts on the gate's public list
// carry no organisation data and are kept, in a host field and as an image
// registry.
func TestPublicRegistryHostsStayReadable(t *testing.T) {
	key := mustKey(t, keyA)
	for _, host := range []string{"ghcr.io", "registry.npmjs.org", "github.com"} {
		if got := pseudonymOf(t, key, "dns_name", host); got != host {
			t.Errorf("public host was rewritten to shape %q", shapeOf(got))
		}
	}
	got := pseudonymOf(t, key, "image_uri", "ghcr.io/acme-org/payments-api:v1.2.3")
	mustMatch(t, "public registry ref", got, `^ghcr\.io/`+hexName+`:v1\.2\.3$`)
}

// TestPublicAdvisorySourceHostsAreSafe: NVD (nist.gov), CISA (www.cisa.gov)
// and FIRST (www.first.org) are the public sources the vulnerability
// collector reads, and every NVD record and source snapshot it emits names
// them. Their hosts carry no organisation data, so a recording keeps them
// readable and Verify admits them. The allow is exact, like the gate's, so a
// lookalike and a subdomain are still refused (eshu-hq/eshu#10734).
func TestPublicAdvisorySourceHostsAreSafe(t *testing.T) {
	key := mustKey(t, keyA)
	for _, host := range []string{"nist.gov", "www.cisa.gov", "www.first.org"} {
		if got := pseudonymOf(t, key, "dns_name", host); got != host {
			t.Errorf("public advisory source host was rewritten to shape %q", shapeOf(got))
		}
		if err := recordpseudo.Verify([]byte(`{"link":"https://`+host+`/page"}`), recordpseudo.Set{}); err != nil {
			t.Errorf("Verify refused public advisory source host %q: %v", host, err)
		}
	}
	for _, lookalike := range []string{"nist.gov.acme-internal.com", "vault.nist.gov"} {
		err := recordpseudo.Verify([]byte(`{"link":"https://`+lookalike+`/page"}`), recordpseudo.Set{})
		if err == nil || !strings.Contains(err.Error(), "alternative hostname") {
			t.Errorf("Verify admitted lookalike %q, err = %v, want a hostname refusal", lookalike, err)
		}
	}
}

// TestPublicVulnerabilityRecordHostsAreSafe: the vulnerability collector's
// real-record recording carries eight more public hosts (the CNA source
// identifiers cve@mitre.org and cna@vuldb.com, the KEV notes host
// nvd.nist.gov, and the reference hosts of the two KEV-listed records) and one
// CPE firmware version, 3.0.4.27, that reads as an address inside AWS
// 3.0.0.0/8. Each is an exact entry, like the gate's, so a subdomain, a
// lookalike zone and a neighbouring address are still refused
// (eshu-hq/eshu#10790).
func TestPublicVulnerabilityRecordHostsAreSafe(t *testing.T) {
	key := mustKey(t, keyA)
	hosts := []string{
		"forum.playsms.org", "mitre.org", "nvd.nist.gov", "packetstormsecurity.com",
		"playsms.org", "research.nccgroup.com", "vuldb.com", "www.youtube.com",
	}
	for _, host := range hosts {
		if got := pseudonymOf(t, key, "dns_name", host); got != host {
			t.Errorf("public vulnerability-record host was rewritten to shape %q", shapeOf(got))
		}
		if err := recordpseudo.Verify([]byte(`{"link":"https://`+host+`/page"}`), recordpseudo.Set{}); err != nil {
			t.Errorf("Verify refused public vulnerability-record host %q: %v", host, err)
		}
		for _, lookalike := range []string{"vault." + host, host + ".acme-internal.com"} {
			err := recordpseudo.Verify([]byte(`{"link":"https://`+lookalike+`/page"}`), recordpseudo.Set{})
			if err == nil || !strings.Contains(err.Error(), "alternative hostname") {
				t.Errorf("Verify admitted lookalike %q, err = %v, want a hostname refusal", lookalike, err)
			}
		}
	}
	const cpe = "cpe:2.3:o:vendor:router_firmware:3.0.4.27:*:*:*:*:*:*:*"
	if err := recordpseudo.Verify([]byte(`{"criteria":"`+cpe+`"}`), recordpseudo.Set{}); err != nil {
		t.Errorf("Verify refused the firmware version literal in a CPE: %v", err)
	}
	for _, neighbour := range []string{"3.0.4.28", "13.0.4.27", "3.0.4.2", "3.0.5.27", "3.1.4.27"} {
		err := recordpseudo.Verify([]byte(`{"value":"`+neighbour+`"}`), recordpseudo.Set{})
		if err == nil || !strings.Contains(err.Error(), "alternative ipv4") {
			t.Errorf("Verify admitted neighbouring address %q, err = %v, want an ipv4 refusal", neighbour, err)
		}
	}
}
