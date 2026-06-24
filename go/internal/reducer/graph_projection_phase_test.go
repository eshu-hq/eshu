// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"
)

func TestGraphProjectionPhaseKeyValidate(t *testing.T) {
	t.Parallel()

	key := GraphProjectionPhaseKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-1",
		GenerationID:     "gen-1",
		Keyspace:         GraphProjectionKeyspaceCodeEntitiesUID,
	}
	if err := key.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestGraphProjectionPhaseKeyValidateRejectsBlankFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  GraphProjectionPhaseKey
	}{
		{
			name: "blank scope",
			key: GraphProjectionPhaseKey{
				AcceptanceUnitID: "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Keyspace:         GraphProjectionKeyspaceCodeEntitiesUID,
			},
		},
		{
			name: "blank acceptance unit",
			key: GraphProjectionPhaseKey{
				ScopeID:      "scope-a",
				SourceRunID:  "run-1",
				GenerationID: "gen-1",
				Keyspace:     GraphProjectionKeyspaceCodeEntitiesUID,
			},
		},
		{
			name: "blank source run",
			key: GraphProjectionPhaseKey{
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				GenerationID:     "gen-1",
				Keyspace:         GraphProjectionKeyspaceCodeEntitiesUID,
			},
		},
		{
			name: "blank generation",
			key: GraphProjectionPhaseKey{
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				SourceRunID:      "run-1",
				Keyspace:         GraphProjectionKeyspaceCodeEntitiesUID,
			},
		},
		{
			name: "blank keyspace",
			key: GraphProjectionPhaseKey{
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := tt.key.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want non-nil")
			}
		})
	}
}

func TestGraphProjectionPhaseRepairValidate(t *testing.T) {
	t.Parallel()

	repair := GraphProjectionPhaseRepair{
		Key: GraphProjectionPhaseKey{
			ScopeID:          "scope-a",
			AcceptanceUnitID: "repo-a",
			SourceRunID:      "run-1",
			GenerationID:     "gen-1",
			Keyspace:         GraphProjectionKeyspaceCodeEntitiesUID,
		},
		Phase:         GraphProjectionPhaseSemanticNodesCommitted,
		CommittedAt:   time.Date(2026, time.April, 17, 10, 0, 0, 0, time.UTC),
		EnqueuedAt:    time.Date(2026, time.April, 17, 10, 0, 1, 0, time.UTC),
		NextAttemptAt: time.Date(2026, time.April, 17, 10, 0, 1, 0, time.UTC),
	}
	if err := repair.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestGraphProjectionPhaseRepairValidateRejectsBlankPhase(t *testing.T) {
	t.Parallel()

	repair := GraphProjectionPhaseRepair{
		Key: GraphProjectionPhaseKey{
			ScopeID:          "scope-a",
			AcceptanceUnitID: "repo-a",
			SourceRunID:      "run-1",
			GenerationID:     "gen-1",
			Keyspace:         GraphProjectionKeyspaceCodeEntitiesUID,
		},
		CommittedAt:   time.Date(2026, time.April, 17, 10, 0, 0, 0, time.UTC),
		EnqueuedAt:    time.Date(2026, time.April, 17, 10, 0, 1, 0, time.UTC),
		NextAttemptAt: time.Date(2026, time.April, 17, 10, 0, 1, 0, time.UTC),
	}
	if err := repair.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

func TestSharedProjectionReadinessPhaseUsesCanonicalNodesForCodeCalls(t *testing.T) {
	t.Parallel()

	// inheritance_edges joins this set (#2867): its :Class targets commit at
	// canonical-nodes. sql_relationships joins it too (#2868): its Sql* targets are
	// canonical nodes as well. rationale_edges joins it (#2869): the EXPLAINS edge's
	// target is a canonical code entity and the :Rationale source is MERGEd inline by
	// the edge writer, so canonical-nodes is the only prerequisite. Gating any of
	// them on semantic-nodes stalls projection because that phase is published only
	// when the semantic-entity reducer runs.
	for _, domain := range []string{DomainCodeCalls, DomainInheritanceEdges, DomainSQLRelationships, DomainRationaleEdges} {
		domain := domain
		t.Run(domain, func(t *testing.T) {
			t.Parallel()

			phase, gated := sharedProjectionReadinessPhase(domain)
			if !gated {
				t.Fatal("gated = false, want true")
			}
			if got, want := phase, GraphProjectionPhaseCanonicalNodesCommitted; got != want {
				t.Fatalf("phase = %q, want %q", got, want)
			}
		})
	}
}

func TestSharedProjectionReadinessPhaseUsesSemanticNodesForSemanticEdgeDomains(t *testing.T) {
	t.Parallel()

	tests := []string{DomainDocumentationEdges}
	for _, domain := range tests {
		domain := domain
		t.Run(domain, func(t *testing.T) {
			t.Parallel()

			phase, gated := sharedProjectionReadinessPhase(domain)
			if !gated {
				t.Fatal("gated = false, want true")
			}
			if got, want := phase, GraphProjectionPhaseSemanticNodesCommitted; got != want {
				t.Fatalf("phase = %q, want %q", got, want)
			}
		})
	}
}
