// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package demo

import "strings"

// The scorecard vocabulary below is copied from go/cmd/eshu's first-run
// benchmark rather than shared with it. go/cmd/eshu is `package main`, so
// nothing can import its definitions, and the first-run benchmark, the
// answer-quality scorecard, and the evidence-packet dogfood command all still
// use the originals there. The JSON tags and the string values are identical
// on both sides on purpose: a harness reading either scorecard sees one shape.

// CriterionName identifies one scored row.
type CriterionName string

const (
	// CriterionFirstAnswer asserts a bounded query actually returned an answer.
	// This is the load-bearing health-only-rejection criterion.
	CriterionFirstAnswer CriterionName = "first_answer_returned"
	// CriterionTruthMetadata asserts the answer carries truth provenance.
	CriterionTruthMetadata CriterionName = "answer_has_truth_metadata"
	// CriterionRepoIndexed asserts indexing completed (not merely healthy).
	CriterionRepoIndexed CriterionName = "repository_indexed"
	// CriterionTimeToAnswer records the measured time to first answer.
	CriterionTimeToAnswer CriterionName = "time_to_first_answer"
	// CriterionPhaseTimings asserts every required phase was recorded.
	CriterionPhaseTimings CriterionName = "phase_timings_complete"
	// CriterionModeObserved asserts the declared cold/warm mode matches
	// what the harness observed about the image cache.
	CriterionModeObserved CriterionName = "declared_mode_matches_observed"
)

// CriterionStatus is the evaluated outcome of one row.
type CriterionStatus string

const (
	// CriterionPass means the criterion was proven satisfied.
	CriterionPass CriterionStatus = "pass"
	// CriterionFail means the criterion was proven unsatisfied.
	CriterionFail CriterionStatus = "fail"
	// CriterionNotMeasured means the metric could not be derived in this
	// environment. It records an honest gap instead of a fabricated value and
	// never on its own fails the benchmark.
	CriterionNotMeasured CriterionStatus = "not_measured"
)

// Criterion is one scored row of the demo TTFA scorecard.
type Criterion struct {
	// Name identifies the success criterion.
	Name CriterionName `json:"name"`
	// Status is the evaluated outcome.
	Status CriterionStatus `json:"status"`
	// Detail is a short human-readable justification or measured value.
	Detail string `json:"detail,omitempty"`
	// Required marks criteria whose failure fails the whole benchmark.
	Required bool `json:"required"`
}

// criterionMarker renders the terminal glyph for a status. Copied from the
// first-run benchmark's renderer for the same reason as the vocabulary above.
func criterionMarker(status CriterionStatus) string {
	switch status {
	case CriterionPass:
		return "[ok]"
	case CriterionFail:
		return "[!!]"
	default:
		return "[--]"
	}
}

// quoteIfEmpty renders a placeholder for an empty value so the scorecard line
// stays readable. Copied from go/cmd/eshu's first_run.go, whose original still
// has five callers there.
func quoteIfEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return "<repo>"
	}
	return value
}
