// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"testing"
)

// TestTargetScopeForComponentAgreesWithWorkItem proves the claim-carried
// derivation bears the identical target identity the workflow derivation
// produces for the same scope: kind, scope, acceptance unit, source run,
// generation, and the recomputed locator hash.
func TestTargetScopeForComponentAgreesWithWorkItem(t *testing.T) {
	t.Parallel()

	item := testScannerWorkItem()
	fromItem, err := TargetScopeFromWorkItem(item)
	if err != nil {
		t.Fatalf("TargetScopeFromWorkItem() error = %v, want nil", err)
	}
	fromClaim, err := TargetScopeForComponent(item.ScopeID, item.AcceptanceUnitID, item.SourceRunID, item.GenerationID)
	if err != nil {
		t.Fatalf("TargetScopeForComponent() error = %v, want nil", err)
	}
	if fromClaim != fromItem {
		t.Fatalf("TargetScopeForComponent() = %+v, want %+v", fromClaim, fromItem)
	}
}

// TestTargetScopeForComponentRejectsBlankIdentity proves each identity field
// fails closed: the component never analyzes with a partial target.
func TestTargetScopeForComponentRejectsBlankIdentity(t *testing.T) {
	t.Parallel()

	full := []string{"scanner-worker://repository/r", "repository:r", "run-1", "gen-1"}
	names := []string{"scope_id", "acceptance_unit_id", "source_run_id", "generation_id"}
	for i, name := range names {
		args := append([]string(nil), full...)
		args[i] = "  "
		if _, err := TargetScopeForComponent(args[0], args[1], args[2], args[3]); err == nil {
			t.Fatalf("TargetScopeForComponent(blank %s) error = nil, want rejection", name)
		}
	}
}

// TestTargetScopeForComponentPinsLocatorFormula proves the locator hash is
// exactly sha256 over the scope ID: provenance identity is reducer-visible
// (stable keys, upserts), so the formula itself is pinned, not just
// agreement between the two derivations. Expected value computed
// independently (python hashlib).
func TestTargetScopeForComponentPinsLocatorFormula(t *testing.T) {
	t.Parallel()

	target, err := TargetScopeForComponent(
		"scanner-worker://repository/provenance-pin",
		"repository:provenance-pin",
		"scanner-worker:generation-1",
		"scanner-worker:generation-1",
	)
	if err != nil {
		t.Fatalf("TargetScopeForComponent() error = %v, want nil", err)
	}
	const want = "sha256:bd6017be017399706af3a46215b8c7f52966b04a6bb07f435f668832cfe0628b"
	if target.LocatorHash != want {
		t.Fatalf("LocatorHash = %q, want %q", target.LocatorHash, want)
	}
}

// TestTargetScopeForComponentDerivesImageKind proves the kind prefix rules
// apply to claim-carried identity exactly as they do to work items.
func TestTargetScopeForComponentDerivesImageKind(t *testing.T) {
	t.Parallel()

	target, err := TargetScopeForComponent(
		"image://registry.example/app@sha256:abc",
		"image:sha256:abc",
		"scanner-worker:generation-1",
		"scanner-worker:generation-1",
	)
	if err != nil {
		t.Fatalf("TargetScopeForComponent() error = %v, want nil", err)
	}
	if target.Kind != TargetImage {
		t.Fatalf("Kind = %q, want %q", target.Kind, TargetImage)
	}
}
