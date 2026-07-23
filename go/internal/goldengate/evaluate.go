// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// evaluate.go holds the pure assertion logic for every gate phase. Each function
// turns observed values plus the snapshot contract into Findings without any
// I/O, so the correctness of the gate is unit-testable independently of
// Postgres, the graph backend, and the HTTP API.

// DrainCounts is the observed queue state at a drain poll.
type DrainCounts struct {
	FactWorkItemsResidual int64
	// FactWorkItemsDeadLetter is the dead_letter subset of the residual. It never
	// drains on its own (the reducer treats dead_letter as terminal and does not
	// retry it), so a nonzero value is the usual reason a drain times out;
	// reporting it makes the failure diagnosable from the gate output alone.
	FactWorkItemsDeadLetter int64
	// SharedIntentsNonterminal is the total count of shared_projection_intents
	// with completed_at IS NULL, across every domain.
	SharedIntentsNonterminal int64
	// SharedIntentsRequiredNonterminal excludes the advisory domains (see
	// DrainAssertions consumers). It is the value the required drain check uses,
	// so a domain with a known not-yet-draining bug can be quarantined as advisory
	// without blocking the gate while the bug is tracked separately.
	SharedIntentsRequiredNonterminal int64
	// SharedIntentsAdvisoryNonterminal is the nonterminal count in the advisory
	// domains; reported but never blocking.
	SharedIntentsAdvisoryNonterminal int64
	// RepoDependencyNonterminal is the repo_dependency-domain subset of
	// SharedIntentsNonterminal. Per B-13 (#3859) it is the primary signal that
	// the relationship-generation activation gate drained correctly; reported as
	// detail on the shared-intents finding.
	RepoDependencyNonterminal int64
	// PopulatedDomainsPresent is the number of distinct
	// require-populated domains that have at least one shared_projection_intents
	// row (completed or not). It proves the reducer actually emitted work for
	// those domains, which is the guard against premature drain convergence: a
	// drain poll that reads 0/0 before the reducer has started would otherwise
	// pass on an unreduced pipeline.
	PopulatedDomainsPresent int64
}

// Drained reports whether the queues are within the snapshot's drain bounds. The
// shared-intents bound applies to the required (non-advisory) nonterminal count
// so a quarantined domain does not keep the poll from converging.
func (d DrainCounts) Drained(a DrainAssertions) bool {
	return d.FactWorkItemsResidual <= a.FactWorkItems.Limit() &&
		d.SharedIntentsRequiredNonterminal <= a.SharedProjectionIntents.Limit()
}

// EvaluateDrains turns observed drain counts into required findings.
// expectedPopulatedDomains is the number of domains the reducer must be proven to
// have emitted (the populated-then-drained guard); 0 disables the check.
func EvaluateDrains(d DrainCounts, a DrainAssertions, expectedPopulatedDomains int, r *Report) {
	if expectedPopulatedDomains > 0 {
		r.AddCheck("drains", "reducer_emitted_required_domains",
			d.PopulatedDomainsPresent >= int64(expectedPopulatedDomains), true,
			fmt.Sprintf("populated domains present=%d, want %d (guards against draining an unreduced pipeline)",
				d.PopulatedDomainsPresent, expectedPopulatedDomains))
	}
	factLimit := a.FactWorkItems.Limit()
	r.AddCheck("drains", "fact_work_items_residual",
		d.FactWorkItemsResidual <= factLimit, true,
		fmt.Sprintf("residual=%d (limit %d; status NOT IN succeeded,superseded; dead_letter=%d)",
			d.FactWorkItemsResidual, factLimit, d.FactWorkItemsDeadLetter))

	intentLimit := a.SharedProjectionIntents.Limit()
	r.AddCheck("drains", "shared_projection_intents_nonterminal",
		d.SharedIntentsRequiredNonterminal <= intentLimit, true,
		fmt.Sprintf("required-nonterminal=%d (limit %d; completed_at IS NULL, excl advisory domains; repo_dependency subset=%d; total=%d)",
			d.SharedIntentsRequiredNonterminal, intentLimit, d.RepoDependencyNonterminal, d.SharedIntentsNonterminal))

	// Advisory: nonterminal intents in quarantined domains (e.g. code_calls).
	// Reported so a known-held domain stays visible without blocking the gate.
	if d.SharedIntentsAdvisoryNonterminal > 0 {
		r.AddCheck("drains", "shared_projection_intents_advisory_nonterminal",
			false, false,
			fmt.Sprintf("advisory-domain nonterminal=%d (quarantined; tracked as a follow-up, not blocking)",
				d.SharedIntentsAdvisoryNonterminal))
	}
}

// EvaluateRequiredCorrelation produces an existence-style correlation finding
// (rc-N). These assertions are corpus-size independent. required controls
// whether a shortfall fails the gate: the minimal gate blocks only on a
// configured subset (rc-1, rc-3) and reports the rest as advisory so latent
// cassette↔code binding gaps surface without blocking until they are fixed.
func EvaluateRequiredCorrelation(rc RequiredCorrelation, count int64, required bool) Finding {
	want := rc.MinimumCount
	if want < 1 {
		want = 1
	}
	evidence := ""
	if len(rc.EvidenceKinds) > 0 {
		evidence = fmt.Sprintf(" evidence_kinds⊇%v", rc.EvidenceKinds)
	}
	return Finding{
		Phase:    "graph",
		Check:    rc.ID,
		OK:       count >= want,
		Required: required,
		Detail: fmt.Sprintf("(%s)-[:%s]->(%s)%s count=%d, want >= %d [%s]",
			rc.FromLabel, rc.Relationship, rc.ToLabel, evidence, count, want, rc.Relationship),
	}
}

// valueAllowed reports whether v is one of allowed. An empty allowed set matches
// nothing (callers that want no value pinning pass nil and skip the check).
func valueAllowed(v string, allowed []string) bool {
	for _, a := range allowed {
		if a == v {
			return true
		}
	}
	return false
}

// EvaluateEdgeProperty produces a required finding for one RequiredEdgeProperties
// entry of a correlation. values holds the property value of every matching
// (evidence-narrowed) edge ("" = absent/non-string). An edge is offending when
// its value is empty or, when allowed is non-empty, not in the allowed set; any
// offending edge fails the finding. With no matching edges the finding passes
// vacuously — the companion MinimumCount finding guards existence — so the two
// together mean "the verb's edges exist and every one of them is stamped". The
// required flag mirrors the parent correlation's blocking status.
func EvaluateEdgeProperty(rc RequiredCorrelation, prop string, values, allowed []string, required bool) Finding {
	offending := 0
	for _, v := range values {
		if v == "" || (len(allowed) > 0 && !valueAllowed(v, allowed)) {
			offending++
		}
	}
	constraint := "non-empty"
	if len(allowed) > 0 {
		constraint = fmt.Sprintf("in %v", allowed)
	}
	return Finding{
		Phase:    "graph",
		Check:    rc.ID + "_edge_prop_" + prop,
		OK:       offending == 0,
		Required: required,
		Detail: fmt.Sprintf("(%s)-[:%s]->(%s) edge property %q must be %s: %d/%d matching edges offending",
			rc.FromLabel, rc.Relationship, rc.ToLabel, prop, constraint, offending, len(values)),
	}
}

// EvaluateNodeProperty produces a required finding for one RequiredNodeProperties
// entry. values holds the property value of every node carrying the label ("" =
// absent). The check is presence-positive: at least MinimumCount nodes must carry
// a non-empty value (in the allowed set when pinned). This catches a property
// regressing to never-set without false-failing on legitimately property-less
// nodes (see RequiredNode).
//
// When rn.MaximumNodePropertyCount names prop with a positive value, the check
// is also CEILED: present must not exceed it. A floor-only check cannot tell
// "correctly scoped to a handful of nodes" from "leaked onto every node of the
// label" — both satisfy `present >= want`. Most RequiredNode entries set no
// maximum (unbounded, the pre-existing behavior); callers that know the exact
// expected breadth should set one.
func EvaluateNodeProperty(rn RequiredNode, prop string, values, allowed []string) Finding {
	present := 0
	for _, v := range values {
		if v != "" && (len(allowed) == 0 || valueAllowed(v, allowed)) {
			present++
		}
	}
	want := rn.MinimumCount
	if want < 1 {
		want = 1
	}
	maxCount := rn.MaximumNodePropertyCount[prop]
	ok := int64(present) >= want
	boundDetail := fmt.Sprintf("want >= %d", want)
	if maxCount > 0 {
		ok = ok && int64(present) <= maxCount
		boundDetail = fmt.Sprintf("want [%d,%d]", want, maxCount)
	}
	constraint := "non-empty"
	if len(allowed) > 0 {
		constraint = fmt.Sprintf("in %v", allowed)
	}
	return Finding{
		Phase:    "graph",
		Check:    rn.ID + "_node_prop_" + prop,
		OK:       ok,
		Required: true,
		Detail: fmt.Sprintf("(%s) node property %q (%s) present on %d node(s), %s",
			rn.Label, prop, constraint, present, boundDetail),
	}
}

// EvaluateRequiredNode produces a required finding asserting at least
// MinimumCount nodes carry the label. It is the node-axis counterpart to
// EvaluateRequiredCorrelation: corpus-size independent and always blocking.
func EvaluateRequiredNode(rn RequiredNode, count int64) Finding {
	want := rn.MinimumCount
	if want < 1 {
		want = 1
	}
	return Finding{
		Phase:    "graph",
		Check:    rn.ID,
		OK:       count >= want,
		Required: true,
		Detail:   fmt.Sprintf("(%s) count=%d, want >= %d", rn.Label, count, want),
	}
}

// EvaluateRequiredSelfLoop produces a required finding asserting the number of
// (n:Label {NodeProperty: NodePropertyValue})-[:Relationship]->(n) self-loop
// edges falls within [MinimumCount, MaximumCount]. Unlike
// EvaluateRequiredCorrelation (an existence-only floor), this bounds both
// sides: it is how the gate pins that a language's genuine recursive self-calls
// survive (the floor) while a re-introduced declaration-vs-call-site self-loop
// bug (eshu-hq/eshu#5332) — which inflates the SAME count, one spurious
// self-loop per declaration — fails the gate instead of silently passing a
// floor-only check.
func EvaluateRequiredSelfLoop(rsl RequiredSelfLoop, count int64) Finding {
	return Finding{
		Phase:    "graph",
		Check:    rsl.ID,
		OK:       count >= rsl.MinimumCount && count <= rsl.MaximumCount,
		Required: true,
		Detail: fmt.Sprintf("(%s {%s=%q})-[:%s]->(self) count=%d, want [%d,%d]",
			rsl.Label, rsl.NodeProperty, rsl.NodePropertyValue, rsl.Relationship,
			count, rsl.MinimumCount, rsl.MaximumCount),
	}
}

// EvaluateNodePresent produces a required finding asserting at least one node of
// label exists. This is the minimal "the pipeline projected something to the
// graph" smoke check — it holds for any non-empty corpus while the richer
// correlation assertions grow as the corpus and cassettes mature.
func EvaluateNodePresent(label string, count int64) Finding {
	return Finding{
		Phase:    "graph",
		Check:    "node_present_" + label,
		OK:       count >= 1,
		Required: true,
		Detail:   fmt.Sprintf("count=%d, want >= 1", count),
	}
}

// EvaluateNodeCount compares an observed node-label count to its snapshot
// tolerance. required controls whether a shortfall blocks the gate: the
// full-corpus mode (-graph-required-only=false) runs all 20 repos and promotes
// these tolerances to required (#3866 criterion 3) so a regression that drops a
// node count out of range fails the gate (a required File-count floor would have
// caught the #4019 nested-file drop). The minimal/required-only mode never
// reaches this check.
func EvaluateNodeCount(label string, rng CountRange, count int64, required bool) Finding {
	return Finding{
		Phase:    "graph",
		Check:    "node_count_" + label,
		OK:       rng.Contains(count),
		Required: required,
		Detail:   fmt.Sprintf("%d, snapshot range [%d,%d]", count, rng.Min, rng.Max),
	}
}

// EvaluateEdgeCount compares an observed edge-type count to its snapshot
// tolerance. required mirrors EvaluateNodeCount: blocking in the full-corpus
// mode, where the ranges are calibrated for the 20-repo corpus.
func EvaluateEdgeCount(rel string, rng CountRange, count int64, required bool) Finding {
	return Finding{
		Phase:    "graph",
		Check:    "edge_count_" + rel,
		OK:       rng.Contains(count),
		Required: required,
		Detail:   fmt.Sprintf("%d, snapshot range [%d,%d]", count, rng.Min, rng.Max),
	}
}

// EvaluateQueryShape validates a raw JSON response body against a query shape:
// required top-level fields must be present, the first array-valued required
// field must have at least MinimumResults elements, and each element must carry
// ResultItemRequiredFields. The returned finding is required: query truth is a
// first-class B-7(c) gate.
func EvaluateQueryShape(name string, shape QueryShape, body []byte) Finding {
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

	pathDetailOK, pathDetail := evaluateJSONPathRequirements(shape, body)
	if !pathDetailOK {
		return mk(false, pathDetail)
	}

	detail := fmt.Sprintf("fields %v present", shape.RequiredResponseFields)
	if len(shape.RequiredResponseFields) == 0 {
		detail = fmt.Sprintf("fields [] present; response keys %v", responseKeys(resp))
	}
	if arrayField != "" {
		detail = fmt.Sprintf("%q has %d results; item fields %v present", arrayField, len(items), shape.ResultItemRequiredFields)
	}
	if pathDetail != "" {
		detail += "; " + pathDetail
	}
	return mk(true, detail)
}

func responseKeys(resp map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(resp))
	for key := range resp {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// EvaluateQuerySurfaceParity validates the offline API/MCP/CLI parity metadata
// carried by query_shapes. CLI shapes must name their eshu argv and a truth
// class, and every parity peer must exist with the same truth class. The actual
// API/MCP response bodies are still checked by EvaluateQueryShape through the
// live gate; this evaluator prevents the committed golden contract from claiming
// CLI parity without naming the shared truth boundary.
func EvaluateQuerySurfaceParity(snap Snapshot, r *Report) {
	keys := make([]string, 0, len(snap.QueryShapes.CLI))
	for key := range snap.QueryShapes.CLI {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		shape := snap.QueryShapes.CLI[key]
		r.Add(evaluateCLIParityShape(key, shape, snap))
	}
}

func evaluateCLIParityShape(key string, shape QueryShape, snap Snapshot) Finding {
	check := "cli:" + key + ":parity"
	if len(shape.Command) == 0 {
		return Finding{Phase: "query", Check: check, OK: false, Required: true, Detail: "CLI shape missing command argv"}
	}
	if strings.TrimSpace(shape.TruthClass) == "" {
		return Finding{Phase: "query", Check: check, OK: false, Required: true, Detail: "CLI shape missing truth class"}
	}
	for _, peer := range shape.ParityWith {
		peerShape, ok := queryShapeByRef(peer, snap)
		if !ok {
			return Finding{Phase: "query", Check: check, OK: false, Required: true, Detail: "missing parity peer " + peer}
		}
		if strings.TrimSpace(peerShape.TruthClass) == "" {
			return Finding{Phase: "query", Check: check, OK: false, Required: true, Detail: "parity peer " + peer + " missing truth class"}
		}
		if peerShape.TruthClass != shape.TruthClass {
			return Finding{
				Phase:    "query",
				Check:    check,
				OK:       false,
				Required: true,
				Detail:   fmt.Sprintf("truth class mismatch: cli=%q peer %s=%q", shape.TruthClass, peer, peerShape.TruthClass),
			}
		}
	}
	return Finding{
		Phase:    "query",
		Check:    check,
		OK:       true,
		Required: true,
		Detail:   fmt.Sprintf("CLI command %v shares truth_class=%q with %d peer(s)", shape.Command, shape.TruthClass, len(shape.ParityWith)),
	}
}

func queryShapeByRef(ref string, snap Snapshot) (QueryShape, bool) {
	kind, key, ok := strings.Cut(ref, ":")
	if !ok {
		return QueryShape{}, false
	}
	switch kind {
	case "http":
		shape, exists := snap.QueryShapes.HTTP[key]
		return shape, exists
	case "mcp":
		shape, exists := snap.QueryShapes.MCP[key]
		return shape, exists
	case "cli":
		shape, exists := snap.QueryShapes.CLI[key]
		return shape, exists
	default:
		return QueryShape{}, false
	}
}

// EvaluateTiming produces a required finding asserting the live pipeline wall
// time stayed within budgetMultiplier × baseline. The baseline is the committed
// budget; the multiplier is the headroom factor (2x for the minimal gate).
func EvaluateTiming(elapsed, baseline time.Duration, budgetMultiplier float64, r *Report) {
	ceiling := time.Duration(float64(baseline) * budgetMultiplier)
	r.AddCheck("timing", "pipeline_wall_time",
		elapsed <= ceiling, true,
		fmt.Sprintf("elapsed=%s, ceiling=%s (%.1fx baseline %s)",
			elapsed.Round(time.Second), ceiling.Round(time.Second), budgetMultiplier, baseline.Round(time.Second)))
}
