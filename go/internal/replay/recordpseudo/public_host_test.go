// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import "testing"

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
