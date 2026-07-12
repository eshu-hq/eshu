// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/perfcontract"
)

// ScaleSlot binds one specs/scale-lab-corpus.v1.yaml corpus slot (issue #3170)
// to the concrete amplification fan-out and the perfcontract enforcement class
// Ifá's Layer 3 load/saturation harness runs it at. Ifá ADOPTS the scale-lab
// taxonomy as its load vocabulary rather than inventing a second one
// (docs/internal/design/4389-ifa-conformance-platform.md "Layer 3",
// anti-rewrite rule 9): the ID here is the spec slot id verbatim, and the
// Enforcement reuses go/internal/perfcontract's existing hermetic/operator-gated
// split rather than a new perf contract (anti-rewrite rule 5).
type ScaleSlot struct {
	// ID is the exact specs/scale-lab-corpus.v1.yaml corpus_slots[].id. A test
	// asserts every ID is present in that spec so the binding cannot drift.
	ID string
	// Scale is the slot's scale band (smoke/small/medium/large/pathological).
	Scale string
	// Scopes is the number of synthetic scopes the corpus amplifier fans the
	// base Odù across for this slot. Zero marks a slot that is not an
	// amplification target (the smoke slot is schema-only, no repository scope).
	Scopes int
	// ResourceCount is the number of resources generated per amplified scope.
	ResourceCount int
	// Enforcement is the perfcontract class the throughput Odù runs this slot
	// under: smoke and small are hermetic (credential-free CI); medium and above
	// need consistent operator hardware for a meaningful latency number.
	Enforcement perfcontract.Enforcement
}

// scaleSlots is the adopted scale-lab corpus taxonomy in scale order. The
// per-slot Scopes/ResourceCount are the P5 fan-out chosen for each adopted
// slot's proof class — small stays tiny so it runs hermetically in the make
// prove common path; medium and above are operator-gated. The slot ids and
// scale bands are the spec's, not invented here.
var scaleSlots = []ScaleSlot{
	{ID: "smoke/synthetic_contracts", Scale: "smoke", Scopes: 0, ResourceCount: 0, Enforcement: perfcontract.EnforcementHermeticGate},
	{ID: "small/single_repo_multidomain", Scale: "small", Scopes: 4, ResourceCount: 16, Enforcement: perfcontract.EnforcementHermeticGate},
	{ID: "medium/representative_20_50", Scale: "medium", Scopes: 24, ResourceCount: 32, Enforcement: perfcontract.EnforcementOperatorGated},
	{ID: "large/full_corpus_release", Scale: "large", Scopes: 64, ResourceCount: 64, Enforcement: perfcontract.EnforcementOperatorGated},
	{ID: "pathological/fanout_correlation", Scale: "pathological", Scopes: 48, ResourceCount: 128, Enforcement: perfcontract.EnforcementOperatorGated},
}

// ScaleSlots returns a copy of the adopted scale-lab slots in scale order so a
// caller cannot mutate the package-level binding.
func ScaleSlots() []ScaleSlot {
	out := make([]ScaleSlot, len(scaleSlots))
	copy(out, scaleSlots)
	return out
}

// ScaleSlotByID returns the adopted slot with the given specs/scale-lab-corpus
// id, or false when no such slot is bound.
func ScaleSlotByID(id string) (ScaleSlot, bool) {
	for _, slot := range scaleSlots {
		if slot.ID == id {
			return slot, true
		}
	}
	return ScaleSlot{}, false
}

// Amplifiable reports whether the slot is a corpus-amplification target (a
// positive scope fan-out). The smoke slot is schema-only and is not.
func (s ScaleSlot) Amplifiable() bool {
	return s.Scopes > 0 && s.ResourceCount > 0
}

// requireAmplifiable returns a fail-closed error for a non-amplification slot so
// a caller cannot silently amplify the schema-only smoke slot into an empty run.
func (s ScaleSlot) requireAmplifiable() error {
	if !s.Amplifiable() {
		return fmt.Errorf("ifa: scale slot %q is not an amplification target (scopes=%d, resources=%d)", s.ID, s.Scopes, s.ResourceCount)
	}
	return nil
}
