// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"time"
)

// evaluate.go holds the pure assertion logic for every gate phase. Each function
// turns observed values plus the snapshot contract into Findings without any
// I/O, so the correctness of the gate is unit-testable independently of
// Postgres, the graph backend, and the HTTP API.

// DrainCounts is the observed queue state at a drain poll.
type DrainCounts struct {
	FactWorkItemsResidual    int64
	SharedIntentsNonterminal int64
	// RepoDependencyNonterminal is the repo_dependency-domain subset of
	// SharedIntentsNonterminal. Per B-13 (#3859) it is the primary signal that
	// the relationship-generation activation gate drained correctly; reported as
	// detail on the shared-intents finding.
	RepoDependencyNonterminal int64
}

// Drained reports whether both queues are within the snapshot's drain bounds.
func (d DrainCounts) Drained(a DrainAssertions) bool {
	return d.FactWorkItemsResidual <= a.FactWorkItems.Limit() &&
		d.SharedIntentsNonterminal <= a.SharedProjectionIntents.Limit()
}

// evaluateDrains turns observed drain counts into required findings.
func evaluateDrains(d DrainCounts, a DrainAssertions, r *Report) {
	factLimit := a.FactWorkItems.Limit()
	r.AddCheck("drains", "fact_work_items_residual",
		d.FactWorkItemsResidual <= factLimit, true,
		fmt.Sprintf("residual=%d (limit %d; status NOT IN succeeded,superseded)",
			d.FactWorkItemsResidual, factLimit))

	intentLimit := a.SharedProjectionIntents.Limit()
	r.AddCheck("drains", "shared_projection_intents_nonterminal",
		d.SharedIntentsNonterminal <= intentLimit, true,
		fmt.Sprintf("nonterminal=%d (limit %d; completed_at IS NULL; repo_dependency subset=%d)",
			d.SharedIntentsNonterminal, intentLimit, d.RepoDependencyNonterminal))
}

// evaluateRequiredCorrelation produces a required finding for an existence-style
// correlation assertion (rc-N). These are corpus-size independent.
func evaluateRequiredCorrelation(rc RequiredCorrelation, count int64) Finding {
	want := rc.MinimumCount
	if want < 1 {
		want = 1
	}
	return Finding{
		Phase:    "graph",
		Check:    rc.ID,
		OK:       count >= want,
		Required: true,
		Detail: fmt.Sprintf("(%s)-[:%s]->(%s) count=%d, want >= %d",
			rc.FromLabel, rc.Relationship, rc.ToLabel, count, want),
	}
}

// evaluateNodeCount produces an advisory finding comparing an observed node-label
// count to its snapshot tolerance. Advisory because the snapshot ranges are
// calibrated for the 20-repo corpus; the minimal gate runs fewer repos.
func evaluateNodeCount(label string, rng CountRange, count int64) Finding {
	return Finding{
		Phase:    "graph",
		Check:    "node_count_" + label,
		OK:       rng.Contains(count),
		Required: false,
		Detail:   fmt.Sprintf("%d, snapshot range [%d,%d]", count, rng.Min, rng.Max),
	}
}

// evaluateEdgeCount produces an advisory finding comparing an observed edge-type
// count to its snapshot tolerance.
func evaluateEdgeCount(rel string, rng CountRange, count int64) Finding {
	return Finding{
		Phase:    "graph",
		Check:    "edge_count_" + rel,
		OK:       rng.Contains(count),
		Required: false,
		Detail:   fmt.Sprintf("%d, snapshot range [%d,%d]", count, rng.Min, rng.Max),
	}
}

// evaluateQueryShape validates a raw JSON response body against a query shape:
// required top-level fields must be present, the first array-valued required
// field must have at least MinimumResults elements, and each element must carry
// ResultItemRequiredFields. The returned finding is required: query truth is a
// first-class B-7(c) gate.
func evaluateQueryShape(name string, shape QueryShape, body []byte) Finding {
	mk := func(ok bool, detail string) Finding {
		return Finding{Phase: "query", Check: name, OK: ok, Required: true, Detail: detail}
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(body, &resp); err != nil {
		return mk(false, "response is not a JSON object: "+err.Error())
	}

	for _, field := range shape.RequiredResponseFields {
		if _, ok := resp[field]; !ok {
			return mk(false, fmt.Sprintf("missing required field %q", field))
		}
	}

	// Locate the first array-valued required field to count results and validate
	// item shape. Many shapes (e.g. operator-control-plane) have no array result;
	// for those, presence of required fields is sufficient.
	var items []json.RawMessage
	var arrayField string
	for _, field := range shape.RequiredResponseFields {
		var arr []json.RawMessage
		if err := json.Unmarshal(resp[field], &arr); err == nil {
			items = arr
			arrayField = field
			break
		}
	}

	if shape.MinimumResults > 0 {
		if arrayField == "" {
			return mk(false, fmt.Sprintf("no array-valued result field among %v but minimum_results=%d",
				shape.RequiredResponseFields, shape.MinimumResults))
		}
		if len(items) < shape.MinimumResults {
			return mk(false, fmt.Sprintf("%q has %d results, want >= %d", arrayField, len(items), shape.MinimumResults))
		}
	}

	for _, itemField := range shape.ResultItemRequiredFields {
		if len(items) == 0 {
			return mk(false, fmt.Sprintf("no items to validate result_item_required_fields %v", shape.ResultItemRequiredFields))
		}
		var first map[string]json.RawMessage
		if err := json.Unmarshal(items[0], &first); err != nil {
			return mk(false, "first result item is not a JSON object: "+err.Error())
		}
		if _, ok := first[itemField]; !ok {
			return mk(false, fmt.Sprintf("result item missing required field %q", itemField))
		}
	}

	detail := fmt.Sprintf("fields %v present", shape.RequiredResponseFields)
	if arrayField != "" {
		detail = fmt.Sprintf("%q has %d results; item fields %v present", arrayField, len(items), shape.ResultItemRequiredFields)
	}
	return mk(true, detail)
}

// evaluateTiming produces a required finding asserting the live pipeline wall
// time stayed within budgetMultiplier × baseline. The baseline is the committed
// budget; the multiplier is the headroom factor (2x for the minimal gate).
func evaluateTiming(elapsed, baseline time.Duration, budgetMultiplier float64, r *Report) {
	ceiling := time.Duration(float64(baseline) * budgetMultiplier)
	r.AddCheck("timing", "pipeline_wall_time",
		elapsed <= ceiling, true,
		fmt.Sprintf("elapsed=%s, ceiling=%s (%.1fx baseline %s)",
			elapsed.Round(time.Second), ceiling.Round(time.Second), budgetMultiplier, baseline.Round(time.Second)))
}
