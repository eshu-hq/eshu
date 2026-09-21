// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// errWrapperGraphUnavailable reports a missing graph reader on the wrapper
// track. It wraps the shared unavailable signal so WriteGraphReadError maps
// it to 503. Findings degrade with counted suppressions; investigate refuses
// because a point lookup cannot degrade.
var errWrapperGraphUnavailable = fmt.Errorf("wrapper-bypass graph reader is unavailable: %w", querycontract.ErrGraphUnavailable)

// scanWrapperCallerRow shapes one caller row from either backend into a
// WrapperCallerRow. Package derives from the caller file's directory, so
// same-package callers stay out of the bypass set exactly like the
// qualification expects.
func scanWrapperCallerRow(row map[string]any) WrapperCallerRow {
	return WrapperCallerRow{
		EntityID:       StringVal(row, "id"),
		Name:           StringVal(row, "name"),
		Package:        codedivergence.PackageOf(StringVal(row, "file_path")),
		EdgeMethod:     StringVal(row, "edge_method"),
		EdgeConfidence: querycontract.FloatVal(row, "edge_confidence"),
		Complexity:     IntVal(row, "complexity"),
	}
}

// runWrapperGraphRows runs backend-rendered Cypher through the graph port.
// NornicDB and Neo4j share the port under the shared Cypher/Bolt contract;
// only the rendered text differs, so one helper serves both.
func (h *CodeHandler) runWrapperGraphRows(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, errWrapperGraphUnavailable
	}
	return h.Neo4j.Run(ctx, cypher, params)
}

// qualifyWrapperTarget maps one target's graph evidence through the Slice A
// qualification into the finding selection. Slice A reads Stats for the
// fan-in winner alone, so the winner is pre-ranked with the shared order
// and only its thinness inputs load via statsFor (the winner's outgoing
// callees). A statsFor failure propagates: degraded graph evidence must
// fail loudly, never shape a verdict. An inferred-edge admission maps to the ambiguous selection the
// finding surfaces as a signal. TargetName rides the family callers rows
// for the reason sentence.
func qualifyWrapperTarget(
	targetID, targetName string,
	direct []WrapperCallerRow,
	fanIn map[string]int,
	statsFor func(WrapperCallerRow) (WrapperCandidateStats, error),
) (WrapperCallerRow, []WrapperCallerRow, codedivergence.BypassSelection, error) {
	sel := codedivergence.BypassSelection{TargetID: targetID, TargetName: targetName}
	ranked := rankWrapperCandidates(direct, fanIn)
	if len(ranked) == 0 {
		return WrapperCallerRow{}, nil, sel, nil
	}
	stats, err := statsFor(ranked[0])
	if err != nil {
		return WrapperCallerRow{}, nil, sel, err
	}
	canonical, bypassers, verdict := SelectCanonicalWrapper(WrapperBypassInput{
		TargetID: targetID,
		Direct:   direct,
		FanIn:    fanIn,
		Stats:    map[string]WrapperCandidateStats{ranked[0].EntityID: stats},
		Params:   DefaultWrapperBypassParams(),
	})
	if verdict.Suppressed {
		return WrapperCallerRow{}, nil, sel, nil
	}
	sel.Qualified = true
	sel.CanonicalID = canonical.EntityID
	sel.CanonicalName = canonical.Name
	sel.Confidence = verdict.Confidence
	sel.Ambiguous = verdict.Inferred
	sel.AmbiguityNote = verdict.Reason
	return canonical, bypassers, sel, nil
}
