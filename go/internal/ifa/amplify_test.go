// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa_test

import (
	"bytes"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

func smallAmplifiableSlot(t *testing.T) ifa.ScaleSlot {
	t.Helper()
	slot, ok := ifa.ScaleSlotByID("small/single_repo_multidomain")
	if !ok {
		t.Fatal("small slot missing")
	}
	return slot
}

// TestAmplifyAtSlotFansAcrossDisjointScopes proves the amplifier turns one base
// Odù into slot.Scopes synthetic scopes with distinct scope ids — the N-scope
// load run the Layer 3 design calls for, produced with zero recordings.
func TestAmplifyAtSlotFansAcrossDisjointScopes(t *testing.T) {
	t.Parallel()

	slot := smallAmplifiableSlot(t)
	raw, err := ifa.AmplifyAtSlot(ifa.FamilyGCP, slot, 4592)
	if err != nil {
		t.Fatalf("AmplifyAtSlot error = %v, want nil", err)
	}
	file, err := cassette.ParseAndValidate(raw)
	if err != nil {
		t.Fatalf("amplified cassette failed validation: %v", err)
	}
	if len(file.Scopes) != slot.Scopes {
		t.Fatalf("amplified scope count = %d, want %d", len(file.Scopes), slot.Scopes)
	}
	seen := make(map[string]bool, len(file.Scopes))
	for _, scope := range file.Scopes {
		if scope.ScopeID == "" {
			t.Fatal("amplified scope has empty scope_id")
		}
		if seen[scope.ScopeID] {
			t.Fatalf("amplified scopes share scope_id %q; the fan-out is not disjoint", scope.ScopeID)
		}
		seen[scope.ScopeID] = true
	}
}

// TestAmplifyAtSlotIsDeterministic proves the same (family, slot, seed) yields
// byte-identical output, so an amplified load run is reproducible and the
// determinism matrix has a stable input — inherited from the family generator's
// seed-indexed derived identities.
func TestAmplifyAtSlotIsDeterministic(t *testing.T) {
	t.Parallel()

	slot := smallAmplifiableSlot(t)
	first, err := ifa.AmplifyAtSlot(ifa.FamilyGCP, slot, 4592)
	if err != nil {
		t.Fatalf("first AmplifyAtSlot error = %v", err)
	}
	second, err := ifa.AmplifyAtSlot(ifa.FamilyGCP, slot, 4592)
	if err != nil {
		t.Fatalf("second AmplifyAtSlot error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("AmplifyAtSlot is not deterministic: same (family, slot, seed) produced different bytes")
	}
}

// TestAmplifyAtSlotRejectsNonAmplifiableAndUnknownFamily proves the amplifier
// fails closed rather than falling back to the determinism-unsafe generic
// rewrite the ADR's Layer 3 landmine warns against, or silently amplifying a
// schema-only slot into an empty run.
func TestAmplifyAtSlotRejectsNonAmplifiableAndUnknownFamily(t *testing.T) {
	t.Parallel()

	smoke, ok := ifa.ScaleSlotByID("smoke/synthetic_contracts")
	if !ok {
		t.Fatal("smoke slot missing")
	}
	if _, err := ifa.AmplifyAtSlot(ifa.FamilyGCP, smoke, 4592); err == nil {
		t.Error("AmplifyAtSlot(smoke) error = nil, want a not-an-amplification-target error")
	}

	slot := smallAmplifiableSlot(t)
	if _, err := ifa.AmplifyAtSlot(ifa.OduFamily("aws"), slot, 4592); err == nil {
		t.Error("AmplifyAtSlot(unknown family) error = nil, want an unsupported-family error")
	}
}
