// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/perfcontract"
)

// scaleLabSpecPath resolves specs/scale-lab-corpus.v1.yaml from this test file's
// own location so the lockstep check does not depend on the test working
// directory.
func scaleLabSpecPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate scale-lab spec")
	}
	// go/internal/ifa/slots_test.go -> repo root is three parents up from go/.
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	return filepath.Join(repoRoot, "specs", "scale-lab-corpus.v1.yaml")
}

// TestScaleSlotsAdoptSpecIDs is the anti-rewrite-rule-9 lockstep: every bound
// slot id must appear verbatim in specs/scale-lab-corpus.v1.yaml, so Ifá adopts
// the scale-lab taxonomy rather than drifting a private copy of it. If the spec
// renames or drops a slot, this fails until the binding is reconciled.
func TestScaleSlotsAdoptSpecIDs(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(scaleLabSpecPath(t))
	if err != nil {
		t.Fatalf("read scale-lab spec: %v", err)
	}
	spec := string(raw)

	slots := ifa.ScaleSlots()
	if len(slots) == 0 {
		t.Fatal("ScaleSlots() returned no slots")
	}
	for _, slot := range slots {
		if !strings.Contains(spec, "id: "+slot.ID) {
			t.Errorf("slot id %q not found in specs/scale-lab-corpus.v1.yaml; the adopted taxonomy drifted from the spec", slot.ID)
		}
	}
}

// TestScaleSlotEnforcementClasses pins the ADR's slot->enforcement mapping:
// smoke and small run hermetically (credential-free CI); medium and above are
// operator-gated. This is the "smoke/small hermetic, medium+ operator-gated"
// clause encoded so it cannot silently change.
func TestScaleSlotEnforcementClasses(t *testing.T) {
	t.Parallel()

	want := map[string]perfcontract.Enforcement{
		"smoke/synthetic_contracts":       perfcontract.EnforcementHermeticGate,
		"small/single_repo_multidomain":   perfcontract.EnforcementHermeticGate,
		"medium/representative_20_50":     perfcontract.EnforcementOperatorGated,
		"large/full_corpus_release":       perfcontract.EnforcementOperatorGated,
		"pathological/fanout_correlation": perfcontract.EnforcementOperatorGated,
	}
	// Drive the assertion from ScaleSlots(), not the want map, so a slot added
	// to scaleSlots without an enforcement-class assertion here fails loudly
	// (false-green guard) instead of going silently unverified.
	for _, slot := range ifa.ScaleSlots() {
		wantClass, ok := want[slot.ID]
		if !ok {
			t.Errorf("slot %q has no enforcement-class assertion; add it to want", slot.ID)
			continue
		}
		if slot.Enforcement != wantClass {
			t.Errorf("slot %q Enforcement = %q, want %q", slot.ID, slot.Enforcement, wantClass)
		}
	}
}

// TestSmallSlotIsHermeticallyAmplifiable guards the make-prove common-path
// contract: the small slot must be a real amplification target (so the hermetic
// throughput Odù has something to drive), while the schema-only smoke slot must
// not be.
func TestSmallSlotIsHermeticallyAmplifiable(t *testing.T) {
	t.Parallel()

	small, ok := ifa.ScaleSlotByID("small/single_repo_multidomain")
	if !ok {
		t.Fatal("small slot missing")
	}
	if !small.Amplifiable() {
		t.Fatalf("small slot must be amplifiable, got scopes=%d resources=%d", small.Scopes, small.ResourceCount)
	}

	smoke, ok := ifa.ScaleSlotByID("smoke/synthetic_contracts")
	if !ok {
		t.Fatal("smoke slot missing")
	}
	if smoke.Amplifiable() {
		t.Fatal("smoke slot is schema-only and must not be an amplification target")
	}
}
