// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// ProjectionFacts is one claimed generation's projection input as the fact
// store loaded it. For a delta generation projected over its #7797 delta-base
// union, UnionBaseGenerationID names the base full generation the overlay was
// built from and UnionOutcome records how the union read resolved; base rows
// in Facts keep that base generation id. The id comes out of the same load
// that built the overlay, so the projection boundary checks exactly the base
// the facts came from and never re-resolves it (#8779). Both are empty for a
// single-generation load.
type ProjectionFacts struct {
	// Facts is the projection input in (observed_at, fact_id) order.
	Facts []facts.Envelope
	// UnionBaseGenerationID names the union base, or is empty when the load
	// read one generation.
	UnionBaseGenerationID string
	// UnionOutcome is the union read's outcome; only DeltaUnionBaseFound is
	// projectable when UnionBaseGenerationID is set.
	UnionOutcome DeltaUnionOutcome
	// UnionChained reports that the delta was diffed over a prior delta: its
	// fence commit is not its union base's commit (#8780). Diagnostic only.
	UnionChained bool
}

// ProjectionGeneration returns generation prepared for Project. A union load
// marks the copy as a delta and names its union base on
// DeltaUnionBaseGenerationID, the one field the fact boundary widens on; a
// single-generation load returns generation with no union base. A union load
// whose outcome is not DeltaUnionBaseFound (the additive union_scope_mismatch
// overlay) is refused here, before any write.
func (f ProjectionFacts) ProjectionGeneration(generation scope.ScopeGeneration) (scope.ScopeGeneration, error) {
	base := strings.TrimSpace(f.UnionBaseGenerationID)
	generation.DeltaUnionBaseGenerationID = ""
	generation.DeltaUnionChained = false
	if base == "" {
		return generation, nil
	}
	if f.UnionOutcome != DeltaUnionBaseFound {
		return scope.ScopeGeneration{}, fmt.Errorf(
			"delta union load for generation %q over base %q resolved %q, not %q; refusing to project it",
			generation.GenerationID, base, f.UnionOutcome, DeltaUnionBaseFound)
	}
	generation.IsDelta = true
	generation.DeltaUnionBaseGenerationID = base
	generation.DeltaUnionChained = f.UnionChained
	return generation, nil
}
