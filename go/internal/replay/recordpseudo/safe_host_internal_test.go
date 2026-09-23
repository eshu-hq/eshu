// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

import "testing"

// TestSafeHostSharesTheGatePublicList: safeHost admits exactly the hosts
// the Verify allow list (publicHostsList, mirrored from the gate) names, so
// the two cannot drift apart.
func TestSafeHostSharesTheGatePublicList(t *testing.T) {
	if len(publicHostsList) == 0 {
		t.Fatal("publicHostsList is empty")
	}
	for host := range publicHostsList {
		if !safeHost(host) {
			t.Errorf("safeHost rejects a gate-listed public host of length %d", len(host))
		}
	}
	if safeHost("registry.acme-org.io") {
		t.Errorf("safeHost admits a customer host")
	}
}
