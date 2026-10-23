// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scope

import (
	"strings"
	"testing"
	"time"
)

// #8779: DeltaUnionBaseGenerationID names the base full generation a delta's
// projection input was overlaid on. It is meaningful only on a delta and never
// names the generation itself; ValidateForScope rejects both misuses so the
// projection boundary cannot be widened by a hand-built generation.
func TestScopeGenerationValidateForScopeDeltaUnionBase(t *testing.T) {
	t.Parallel()

	repoScope := IngestionScope{
		ScopeID:       "scope-8779",
		SourceSystem:  "git",
		ScopeKind:     KindRepository,
		CollectorKind: CollectorGit,
		PartitionKey:  "repo-8779",
	}
	delta := ScopeGeneration{
		GenerationID: "gen-delta",
		ScopeID:      "scope-8779",
		ObservedAt:   time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC),
		IngestedAt:   time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC),
		Status:       GenerationStatusPending,
		TriggerKind:  TriggerKindSnapshot,
		IsDelta:      true,
	}

	accepted := delta
	accepted.DeltaUnionBaseGenerationID = "gen-base"
	if err := accepted.ValidateForScope(repoScope); err != nil {
		t.Fatalf("ValidateForScope(delta with union base) error = %v, want nil", err)
	}

	nonDelta := accepted
	nonDelta.IsDelta = false
	if err := nonDelta.ValidateForScope(repoScope); err == nil ||
		!strings.Contains(err.Error(), "delta_union_base_generation_id") {
		t.Fatalf("ValidateForScope(non-delta with union base) error = %v, want a delta_union_base_generation_id error", err)
	}

	self := delta
	self.DeltaUnionBaseGenerationID = delta.GenerationID
	if err := self.ValidateForScope(repoScope); err == nil ||
		!strings.Contains(err.Error(), "delta_union_base_generation_id") {
		t.Fatalf("ValidateForScope(union base == generation) error = %v, want a delta_union_base_generation_id error", err)
	}
}
