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
