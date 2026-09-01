// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gpphase

import "testing"

func TestPhaseKeyValidate(t *testing.T) {
	t.Parallel()

	key := PhaseKey{
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		SourceRunID:      "run-1",
		GenerationID:     "gen-1",
		Keyspace:         KeyspaceCodeEntitiesUID,
	}
	if err := key.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestPhaseKeyValidateRejectsBlankFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  PhaseKey
	}{
		{
			name: "blank scope",
			key: PhaseKey{
				AcceptanceUnitID: "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				Keyspace:         KeyspaceCodeEntitiesUID,
			},
		},
		{
			name: "blank acceptance unit",
			key: PhaseKey{
				ScopeID:      "scope-a",
				SourceRunID:  "run-1",
				GenerationID: "gen-1",
				Keyspace:     KeyspaceCodeEntitiesUID,
			},
		},
		{
			name: "blank source run",
			key: PhaseKey{
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				GenerationID:     "gen-1",
				Keyspace:         KeyspaceCodeEntitiesUID,
			},
		},
		{
			name: "blank generation",
			key: PhaseKey{
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				SourceRunID:      "run-1",
				Keyspace:         KeyspaceCodeEntitiesUID,
			},
		},
		{
			name: "blank keyspace",
			key: PhaseKey{
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
