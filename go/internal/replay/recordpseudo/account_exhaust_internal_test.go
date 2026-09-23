// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

import (
	"errors"
	"testing"
)

// TestAccountSpaceExhaustionIsAnError (#6987 gating re-review P2): when
// every account slot is owned, a new raw account must fail the recording
// like IPv4 exhaustion does, never overwrite a slot and merge two accounts.
func TestAccountSpaceExhaustionIsAnError(t *testing.T) {
	key, err := NewKey([]byte("6965-p3-probe-key-0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	d := newDictionary(key)
	d.accountSpace = 2
	first := d.account("210987654321")
	second := d.account("310987654322")
	if first == second {
		t.Fatalf("two accounts shared a pseudonym before the space was full")
	}
	d.account("410987654323")
	if !errors.Is(d.failure, ErrAccountsExhausted) {
		t.Fatalf("failure = %v, want ErrAccountsExhausted", d.failure)
	}
	if d.accountSlots[d.accountByRaw["210987654321"]] != "210987654321" ||
		d.accountSlots[d.accountByRaw["310987654322"]] != "310987654322" {
		t.Errorf("an owned slot was overwritten")
	}
}
