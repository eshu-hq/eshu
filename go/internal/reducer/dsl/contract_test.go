// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dsl

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

func TestDefaultRuntimeContract(t *testing.T) {
	t.Parallel()

	contract := DefaultRuntimeContract()
	if err := contract.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
	if got, want := contract.Checkpoints[0].Phase, gpphase.PhaseCrossSourceAnchorReady; got != want {
		t.Fatalf("Checkpoints[0].Phase = %q, want %q", got, want)
	}
	if got, want := contract.Checkpoints[len(contract.Checkpoints)-1].Phase, gpphase.PhaseWorkloadMaterialization; got != want {
		t.Fatalf("last checkpoint phase = %q, want %q", got, want)
	}
}

func TestRuntimeContractTemplateReturnsClonedSlices(t *testing.T) {
	t.Parallel()

	contract := RuntimeContractTemplate()
	contract.Components[0] = "mutated"
	contract.Checkpoints[0] = PublishedCheckpoint{
		Keyspace: gpphase.KeyspaceServiceUID,
		Phase:    gpphase.PhaseCanonicalNodesCommitted,
	}

	fresh := RuntimeContractTemplate()
	if got, want := fresh.Components[0], "evaluator"; got != want {
		t.Fatalf("fresh Components[0] = %q, want %q", got, want)
	}
	if got, want := fresh.Checkpoints[0].Phase, gpphase.PhaseCrossSourceAnchorReady; got != want {
		t.Fatalf("fresh Checkpoints[0].Phase = %q, want %q", got, want)
	}
}
