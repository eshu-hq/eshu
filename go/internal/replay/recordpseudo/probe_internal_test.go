// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

import "testing"

// TestIPv4SlotProbingOnCollision forces the collision the birthday bound
// makes likely at 762 slots: a slot already owned by another raw address
// must be skipped, the collision counted, and the two pseudonyms differ.
func TestIPv4SlotProbingOnCollision(t *testing.T) {
	key, err := NewKey([]byte("6965-p3-probe-key-0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	first := newDictionary(key)
	unprobed := first.ipv4Slot("10.1.2.3")
	if first.ipCollisions != 0 {
		t.Fatalf("collision counted on an empty slot table")
	}
	second := newDictionary(key)
	// Occupy exactly the slot 10.1.2.3 hashes to with a different owner.
	for idx, owner := range first.ipSlots {
		_ = owner
		second.ipSlots[idx] = "other-raw-address"
	}
	probed := second.ipv4Slot("10.1.2.3")
	if second.ipCollisions != 1 {
		t.Errorf("collisions = %d, want 1", second.ipCollisions)
	}
	if probed == unprobed {
		t.Errorf("probing did not move the address off the occupied slot")
	}
	// Re-learning the same raw address through the guarded entry point is a
	// no-op: the pseudonym stays and no further collision is counted.
	second.set(ClassIPv4, "10.1.2.3", probed)
	second.learn(ClassIPv4, "10.1.2.3")
	if second.pseudonym("10.1.2.3") != probed || second.ipCollisions != 1 {
		t.Errorf("re-learning the same address moved it or counted a collision")
	}
}
