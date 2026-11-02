// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"
)

// TestUnwrapNilAdmissionPortCoversAnyNilableKind proves the fail-closed
// unwrap holds for every dynamic type behind an admission port, not just
// today's adapter: a typed nil of any nilable kind becomes an untyped nil,
// while live values and non-nilable kinds pass through untouched.
func TestUnwrapNilAdmissionPortCoversAnyNilableKind(t *testing.T) {
	t.Parallel()

	type otherStore struct{ live bool }

	var nilPtr *otherStore
	if got := unwrapNilAdmissionPort[any](any(nilPtr)); got != nil {
		t.Errorf("unwrapNilAdmissionPort(typed-nil *otherStore) = %v, want nil", got)
	}
	var nilMap map[string]int
	if got := unwrapNilAdmissionPort[any](any(nilMap)); got != nil {
		t.Errorf("unwrapNilAdmissionPort(nil map) = %v, want nil", got)
	}
	var nilSlice []byte
	if got := unwrapNilAdmissionPort[any](any(nilSlice)); got != nil {
		t.Errorf("unwrapNilAdmissionPort(nil slice) = %v, want nil", got)
	}
	live := &otherStore{live: true}
	if got := unwrapNilAdmissionPort[any](any(live)); got != any(live) {
		t.Errorf("unwrapNilAdmissionPort(live store) changed the value: got %v", got)
	}
	if got := unwrapNilAdmissionPort(42); got != 42 {
		t.Errorf("unwrapNilAdmissionPort(42) = %v, want 42", got)
	}
	if got := unwrapNilAdmissionPort[any](nil); got != nil {
		t.Errorf("unwrapNilAdmissionPort(nil) = %v, want nil", got)
	}
}
