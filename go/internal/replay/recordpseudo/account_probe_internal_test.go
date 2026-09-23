// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

import "testing"

// TestAccountSlotProbingOnCollision: two raw accounts whose HMAC lands on
// one 8-digit slot must not share a pseudonym; the second is probed to the
// next slot and the collision is counted, as ipv4Slot does.
func TestAccountSlotProbingOnCollision(t *testing.T) {
	key, err := NewKey([]byte("6965-p3-probe-key-0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	first := newDictionary(key)
	unprobed := first.account("210987654321")
	if first.accountCollisions != 0 {
		t.Fatalf("collision counted on an empty slot table")
	}
	second := newDictionary(key)
	for idx := range first.accountSlots {
		second.accountSlots[idx] = "other-raw-account"
	}
	probed := second.account("210987654321")
	if second.accountCollisions != 1 {
		t.Errorf("collisions = %d, want 1", second.accountCollisions)
	}
	if probed == unprobed || len(probed) != 12 || probed[:4] != "0000" {
		t.Errorf("probing did not move the account off the occupied slot, or broke the 0000 form")
	}
	if again := second.account("210987654321"); again != probed || second.accountCollisions != 1 {
		t.Errorf("re-minting the same account moved it or counted a collision")
	}
}
