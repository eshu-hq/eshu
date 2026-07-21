// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadRemoteValidationFrozenSetParsesAndFailsClosed covers the frozen-set
// loader (FIX 2, #5407): a well-formed file parses to a slug set; a missing
// file, a read error, or any malformed line fails closed with an error so the
// atomic-swap defense can never be silently absent.
func TestLoadRemoteValidationFrozenSetParsesAndFailsClosed(t *testing.T) {
	t.Parallel()

	t.Run("well-formed parses", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, RemoteValidationFrozenFileName)
		if err := os.WriteFile(path, []byte("# header\nprod-alpha\nprod-beta-two\n\n"), 0o644); err != nil {
			t.Fatalf("write frozen: %v", err)
		}
		frozen, err := LoadRemoteValidationFrozenSet(path)
		if err != nil {
			t.Fatalf("LoadRemoteValidationFrozenSet: %v", err)
		}
		if len(frozen) != 2 {
			t.Fatalf("frozen = %+v, want 2 entries", frozen)
		}
		if _, ok := frozen["prod-alpha"]; !ok {
			t.Fatal("frozen missing prod-alpha")
		}
		if _, ok := frozen["prod-beta-two"]; !ok {
			t.Fatal("frozen missing prod-beta-two")
		}
	})

	t.Run("missing file fails closed", func(t *testing.T) {
		t.Parallel()
		if _, err := LoadRemoteValidationFrozenSet(filepath.Join(t.TempDir(), "absent.txt")); err == nil {
			t.Fatal("LoadRemoteValidationFrozenSet(missing) error = nil, want fail-closed")
		}
	})

	t.Run("malformed entry fails closed", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, RemoteValidationFrozenFileName)
		if err := os.WriteFile(path, []byte("prod-alpha\nNot A Slug\n"), 0o644); err != nil {
			t.Fatalf("write frozen: %v", err)
		}
		if _, err := LoadRemoteValidationFrozenSet(path); err == nil {
			t.Fatal("LoadRemoteValidationFrozenSet(malformed) error = nil, want fail-closed")
		}
	})
}

// TestRemoteValidationBaselineNotFrozenDetectsAtomicSwap proves the
// frozen-membership predicate (FIX 2, #5407) catches the constant-count atomic
// swap that the FROZEN_MAX ceiling alone cannot: dropping a legitimately
// baselined ref A while adding a new unbacked ref C keeps the entry count at the
// ceiling — RemoteValidationBaselineCeilingExceeded stays false — but C is not
// in the frozen set, so RemoteValidationBaselineNotFrozen names it. Burn-down
// (a strict subset of frozen) yields no offenders.
func TestRemoteValidationBaselineNotFrozenDetectsAtomicSwap(t *testing.T) {
	t.Parallel()

	frozen := map[string]struct{}{"prod-ref-a": {}, "prod-ref-b": {}}

	// Atomic swap at constant count: baseline drops A, adds C. Count 2 == the
	// frozen ceiling of 2, so the ceiling check is satisfied.
	swap := RemoteValidationBaseline{
		Entries: map[string]struct{}{"prod-ref-b": {}, "prod-ref-c": {}},
		Ceiling: 2,
	}
	if RemoteValidationBaselineCeilingExceeded(swap) {
		t.Fatal("precondition: the atomic swap must NOT exceed the ceiling (that is why the frozen set is needed)")
	}
	offenders := RemoteValidationBaselineNotFrozen(swap, frozen)
	if len(offenders) != 1 || offenders[0] != "prod-ref-c" {
		t.Fatalf("offenders = %+v, want exactly [prod-ref-c] (the smuggled ref)", offenders)
	}

	// Burn-down: baseline is a strict subset of frozen — no offenders.
	burnDown := RemoteValidationBaseline{
		Entries: map[string]struct{}{"prod-ref-a": {}},
		Ceiling: 2,
	}
	if got := RemoteValidationBaselineNotFrozen(burnDown, frozen); len(got) != 0 {
		t.Fatalf("burn-down offenders = %+v, want none (subset of frozen)", got)
	}
}

// TestRemoteValidationRealFrozenSetCoversBaseline asserts the committed baseline
// is a subset of the committed frozen set: every burn-down entry is audited.
func TestRemoteValidationRealFrozenSetCoversBaseline(t *testing.T) {
	t.Parallel()

	specsDir := repoSpecsDir(t)
	baseline, err := LoadRemoteValidationBaseline(filepath.Join(specsDir, RemoteValidationBaselineFileName))
	if err != nil {
		t.Fatalf("LoadRemoteValidationBaseline(real): %v", err)
	}
	frozen, err := LoadRemoteValidationFrozenSet(filepath.Join(specsDir, RemoteValidationFrozenFileName))
	if err != nil {
		t.Fatalf("LoadRemoteValidationFrozenSet(real): %v", err)
	}
	if offenders := RemoteValidationBaselineNotFrozen(baseline, frozen); len(offenders) != 0 {
		t.Fatalf("committed baseline holds %d entr(y/ies) not in the frozen set: %v", len(offenders), offenders)
	}
}
