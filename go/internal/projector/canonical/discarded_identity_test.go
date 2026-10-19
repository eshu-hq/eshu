// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package canonical

import (
	"testing"
)

// TestDiscardsIncomingEntityID pins which entity types canonical projection
// re-derives (ignoring the fact's entity_id) versus preserves. The semantic
// writer's MATCH-miss telemetry (#8359) reads this so it only flags rows the
// projector actually re-identifies: a divergent uid on a preserved type still
// MATCHes, so warning there would be noise.
func TestDiscardsIncomingEntityID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		entityType string
		want       bool
	}{
		{entityType: "Function", want: true},
		{entityType: "Annotation", want: true},
		{entityType: "TypeAnnotation", want: true},
		// Typedef is MATCH-owned by the semantic writer on NornicDB yet
		// keeps its incoming uid through projection: a divergent uid still
		// MATCHes, so it must NOT count as unmatched.
		{entityType: "Typedef", want: false},
		{entityType: "TypeAlias", want: false},
		{entityType: "Component", want: false},
		{entityType: "Module", want: false},
		// Variable is semantic-owned (#8588): phase E skips the label and the
		// semantic writer MERGEs it under the incoming uid, so the projector
		// preserves rather than re-derives. Flagging it must-derive would be
		// MATCH-miss telemetry noise.
		{entityType: "Variable", want: false},
		{entityType: "Bogus", want: false},
		{entityType: "", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.entityType, func(t *testing.T) {
			t.Parallel()
			if got := DiscardsIncomingEntityID(tc.entityType); got != tc.want {
				t.Errorf("DiscardsIncomingEntityID(%q) = %v, want %v", tc.entityType, got, tc.want)
			}
		})
	}
}
