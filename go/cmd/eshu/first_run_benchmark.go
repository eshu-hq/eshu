// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// firstRunEnvelope is the canonical `{data, truth, error}` envelope emitted by
// `eshu first-run --json`. The benchmark evaluator consumes this structured
// result rather than scraping human text, so the first-answer verdict is
// derived from the same fields the command already proves.
type firstRunEnvelope struct {
	// Data is the machine-readable first-run result.
	Data firstRunResult `json:"data"`
	// Truth carries the freshness/completeness/backend labels for the answer.
	// A missing or empty Truth map means the answer lacks provenance and the
	// benchmark must not trust it.
	Truth map[string]any `json:"truth"`
	// Error is non-nil when the run failed; any error fails the benchmark.
	Error *firstRunEnvelopeError `json:"error"`
}

// firstRunEnvelopeError mirrors the error object inside the JSON envelope.
type firstRunEnvelopeError struct {
	// Message is the human-readable failure cause preserved by first-run.
	Message string `json:"message"`
}

// parseFirstRunEnvelope decodes the canonical `eshu first-run --json` output.
// It returns a descriptive error when the payload is not the expected envelope
// so the harness fails loudly rather than silently scoring malformed input.
func parseFirstRunEnvelope(raw []byte) (firstRunEnvelope, error) {
	var env firstRunEnvelope
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if err := dec.Decode(&env); err != nil {
		return firstRunEnvelope{}, fmt.Errorf("decode first-run envelope: %w", err)
	}
	return env, nil
}

// benchmarkCriterionName identifies a single measured success criterion from
// issue #1772. Each name maps to one row in the dogfood scorecard.
type benchmarkCriterionName string

const (
	// criterionFirstAnswer asserts a bounded query actually returned an answer.
	// This is the load-bearing health-only-rejection criterion.
	criterionFirstAnswer benchmarkCriterionName = "first_answer_returned"
	// criterionTruthMetadata asserts the answer carries truth provenance.
	criterionTruthMetadata benchmarkCriterionName = "answer_has_truth_metadata"
	// criterionSourceHandles asserts the answer references a concrete source.
	criterionSourceHandles benchmarkCriterionName = "answer_has_source_handles"
	// criterionRepoIndexed asserts indexing completed (not merely healthy).
	criterionRepoIndexed benchmarkCriterionName = "repository_indexed"
	// criterionTimeToAnswer records the measured time to first answer.
	criterionTimeToAnswer benchmarkCriterionName = "time_to_first_answer"
	// criterionManualSteps records the declared manual copy/paste step count.
	criterionManualSteps benchmarkCriterionName = "manual_steps"
	// criterionFailureExplanation records whether a failed run explained why.
	criterionFailureExplanation benchmarkCriterionName = "failure_explanation_quality"
)

// benchmarkCriterionStatus is the outcome of evaluating one criterion.
type benchmarkCriterionStatus string

const (
	// benchmarkCriterionPass means the criterion was proven satisfied.
	benchmarkCriterionPass benchmarkCriterionStatus = "pass"
	// benchmarkCriterionFail means the criterion was proven unsatisfied.
	benchmarkCriterionFail benchmarkCriterionStatus = "fail"
	// benchmarkCriterionNotMeasured means the metric could not be derived in
	// this environment. It records an honest gap instead of a fabricated value
	// and never on its own fails the benchmark.
	benchmarkCriterionNotMeasured benchmarkCriterionStatus = "not_measured"
)

// benchmarkCriterion is one scored row of the first-answer benchmark.
type benchmarkCriterion struct {
	// Name identifies the success criterion.
	Name benchmarkCriterionName `json:"name"`
	// Status is the evaluated outcome.
	Status benchmarkCriterionStatus `json:"status"`
	// Detail is a short human-readable justification or measured value.
	Detail string `json:"detail,omitempty"`
	// Required marks criteria whose failure fails the whole benchmark.
	Required bool `json:"required"`
}

// benchmarkMeasurements carries the environment-derived inputs that cannot be
// read from the envelope alone. Unset numeric fields are recorded as
// not-measured rather than guessed.
type benchmarkMeasurements struct {
	// Path names which onboarding path produced the envelope, e.g.
	// "local_binary", "local_compose", or "hosted".
	Path string
	// Elapsed is the wall-clock time to first answer. Zero means not measured.
	Elapsed time.Duration
	// ManualSteps is the declared count of manual copy/paste steps for this
	// path. A negative value means not declared/not measured.
	ManualSteps int
}

// notMeasuredManualSteps is the sentinel meaning the manual-step count was not
// declared for the path under test.
const notMeasuredManualSteps = -1

// benchmarkVerdict is the full scorecard for one onboarding path.
type benchmarkVerdict struct {
	// Path echoes the onboarding path that was scored.
	Path string `json:"path"`
	// Pass is true only when every required criterion passed.
	Pass bool `json:"pass"`
	// Criteria holds every scored row in stable order.
	Criteria []benchmarkCriterion `json:"criteria"`
}

// criterion returns the scored row for a name, or a zero-value criterion with
// the requested name when it was not scored. The empty status makes a missing
// row visibly distinct from a real outcome.
func (v benchmarkVerdict) criterion(name benchmarkCriterionName) benchmarkCriterion {
	for _, c := range v.Criteria {
		if c.Name == name {
			return c
		}
	}
	return benchmarkCriterion{Name: name}
}

// failureReasons lists the details of every required criterion that failed so
// the harness can print actionable output.
func (v benchmarkVerdict) failureReasons() []string {
	var reasons []string
	for _, c := range v.Criteria {
		if c.Required && c.Status == benchmarkCriterionFail {
			reasons = append(reasons, fmt.Sprintf("%s: %s", c.Name, c.Detail))
		}
	}
	return reasons
}

// evaluateFirstAnswerBenchmark scores a first-run envelope against the issue
// #1772 success criteria. It is a pure function so the health-only-rejection
// invariant is fully unit-testable.
//
// The benchmark FAILS (rejects a health-only "success") when any required
// criterion fails: no bounded query returned, the answer lacks truth metadata,
// the answer lacks a source handle, indexing did not complete, or the run
// envelope carried an error. Optional criteria (time, manual steps) only ever
// add not-measured rows and never flip an otherwise-complete run to fail.
func evaluateFirstAnswerBenchmark(env firstRunEnvelope, m benchmarkMeasurements) benchmarkVerdict {
	verdict := benchmarkVerdict{Path: m.Path}

	verdict.Criteria = append(verdict.Criteria, evaluateFirstAnswerCriterion(env))
	verdict.Criteria = append(verdict.Criteria, evaluateTruthMetadataCriterion(env))
	verdict.Criteria = append(verdict.Criteria, evaluateSourceHandlesCriterion(env))
	verdict.Criteria = append(verdict.Criteria, evaluateRepoIndexedCriterion(env))
	verdict.Criteria = append(verdict.Criteria, evaluateTimeToAnswerCriterion(env, m))
	verdict.Criteria = append(verdict.Criteria, evaluateManualStepsCriterion(m))
	verdict.Criteria = append(verdict.Criteria, evaluateFailureExplanationCriterion(env))

	verdict.Pass = true
	for _, c := range verdict.Criteria {
		if c.Required && c.Status != benchmarkCriterionPass {
			verdict.Pass = false
			break
		}
	}
	return verdict
}

// evaluateFirstAnswerCriterion is the health-only-rejection guard: success
// requires a bounded query that actually returned and a non-error envelope.
// Readiness or process health alone never satisfies it.
func evaluateFirstAnswerCriterion(env firstRunEnvelope) benchmarkCriterion {
	c := benchmarkCriterion{Name: criterionFirstAnswer, Required: true}
	if env.Error != nil {
		c.Status = benchmarkCriterionFail
		c.Detail = "run envelope reported an error: " + env.Error.Message
		return c
	}
	if !env.Data.QueryAnswered {
		c.Status = benchmarkCriterionFail
		c.Detail = "no bounded query returned (health/readiness alone is not an answer)"
		return c
	}
	if strings.TrimSpace(env.Data.QuerySummary) == "" {
		c.Status = benchmarkCriterionFail
		c.Detail = "query_answered=true but query_summary is empty; no evidence the query returned"
		return c
	}
	c.Status = benchmarkCriterionPass
	c.Detail = env.Data.QuerySummary
	return c
}

// evaluateTruthMetadataCriterion requires the answer to carry provenance: a
// non-empty truth map with the freshness and completeness labels populated.
func evaluateTruthMetadataCriterion(env firstRunEnvelope) benchmarkCriterion {
	c := benchmarkCriterion{Name: criterionTruthMetadata, Required: true}
	if len(env.Truth) == 0 {
		c.Status = benchmarkCriterionFail
		c.Detail = "answer carries no truth metadata"
		return c
	}
	missing := truthMissingKeys(env.Truth)
	if len(missing) > 0 {
		c.Status = benchmarkCriterionFail
		c.Detail = "truth metadata missing keys: " + strings.Join(missing, ", ")
		return c
	}
	c.Status = benchmarkCriterionPass
	c.Detail = fmt.Sprintf("freshness=%v completeness=%v", env.Truth["freshness"], env.Truth["completeness"])
	return c
}

// truthMissingKeys lists the required truth keys that are absent or blank.
func truthMissingKeys(truth map[string]any) []string {
	var missing []string
	for _, key := range []string{"freshness", "completeness"} {
		value, ok := truth[key]
		if !ok || strings.TrimSpace(fmt.Sprintf("%v", value)) == "" {
			missing = append(missing, key)
		}
	}
	return missing
}

// evaluateSourceHandlesCriterion requires the answer to reference at least one
// concrete source. A query that returned zero repositories, or a blank repo
// target with no summarized handle, has no source handle to cite.
func evaluateSourceHandlesCriterion(env firstRunEnvelope) benchmarkCriterion {
	c := benchmarkCriterion{Name: criterionSourceHandles, Required: true}
	if !env.Data.QueryAnswered {
		c.Status = benchmarkCriterionFail
		c.Detail = "no answer returned, so no source handle is present"
		return c
	}
	summary := strings.TrimSpace(env.Data.QuerySummary)
	if answerLacksSourceHandle(summary, env.Data.RepoTarget) {
		c.Status = benchmarkCriterionFail
		c.Detail = "answer returned no concrete source handle (0 repositories / no repo target)"
		return c
	}
	c.Status = benchmarkCriterionPass
	c.Detail = sourceHandleDetail(summary, env.Data.RepoTarget)
	return c
}

// answerLacksSourceHandle reports whether the bounded answer references no
// concrete source. A "returned 0" summary or an empty repo target with no
// example handle means there is nothing to cite.
func answerLacksSourceHandle(summary, repoTarget string) bool {
	if strings.Contains(summary, "returned 0") {
		return true
	}
	if strings.Contains(summary, "e.g. ") {
		return false
	}
	return strings.TrimSpace(repoTarget) == ""
}

// sourceHandleDetail renders the cited source handle for the scorecard.
func sourceHandleDetail(summary, repoTarget string) string {
	if idx := strings.Index(summary, "e.g. "); idx >= 0 {
		handle := strings.TrimRight(summary[idx+len("e.g. "):], ")")
		return "source handle: " + strings.TrimSpace(handle)
	}
	return "source handle: " + strings.TrimSpace(repoTarget)
}

// evaluateRepoIndexedCriterion requires indexing to have completed. A partial
// or unknown index is not a completed-indexing proof.
func evaluateRepoIndexedCriterion(env firstRunEnvelope) benchmarkCriterion {
	c := benchmarkCriterion{Name: criterionRepoIndexed, Required: true}
	indexed := strings.TrimSpace(env.Data.RepoIndexed)
	if indexed == "complete" {
		c.Status = benchmarkCriterionPass
		c.Detail = "repository indexing completed"
		return c
	}
	c.Status = benchmarkCriterionFail
	c.Detail = "repo_indexed=" + quoteIfEmpty(indexed) + " (indexing not proven complete)"
	return c
}

// evaluateTimeToAnswerCriterion records the measured time to first answer. It
// is informational: a missing measurement is not-measured, never a failure.
func evaluateTimeToAnswerCriterion(env firstRunEnvelope, m benchmarkMeasurements) benchmarkCriterion {
	c := benchmarkCriterion{Name: criterionTimeToAnswer}
	if m.Elapsed <= 0 {
		c.Status = benchmarkCriterionNotMeasured
		c.Detail = "elapsed time not captured in this environment"
		return c
	}
	if !env.Data.QueryAnswered || env.Error != nil {
		c.Status = benchmarkCriterionNotMeasured
		c.Detail = "no answer returned; time-to-answer is undefined"
		return c
	}
	c.Status = benchmarkCriterionPass
	c.Detail = "time to first answer: " + m.Elapsed.Round(time.Millisecond).String()
	return c
}

// evaluateManualStepsCriterion records the declared manual copy/paste step
// count for the path. The count is a declared constant per path, not derived
// from the envelope, so an undeclared count is not-measured.
func evaluateManualStepsCriterion(m benchmarkMeasurements) benchmarkCriterion {
	c := benchmarkCriterion{Name: criterionManualSteps}
	if m.ManualSteps < 0 {
		c.Status = benchmarkCriterionNotMeasured
		c.Detail = "manual step count not declared for this path"
		return c
	}
	c.Status = benchmarkCriterionPass
	c.Detail = fmt.Sprintf("declared manual copy/paste steps: %d", m.ManualSteps)
	return c
}

// evaluateFailureExplanationCriterion checks that a failed run explained why.
// On a successful run it is not-measured (there is nothing to explain).
func evaluateFailureExplanationCriterion(env firstRunEnvelope) benchmarkCriterion {
	c := benchmarkCriterion{Name: criterionFailureExplanation}
	failed := env.Error != nil || !env.Data.QueryAnswered
	if !failed {
		c.Status = benchmarkCriterionNotMeasured
		c.Detail = "run succeeded; no failure to explain"
		return c
	}
	if failureExplanationPresent(env) {
		c.Status = benchmarkCriterionPass
		c.Detail = "failure included a cause and next steps"
		return c
	}
	c.Status = benchmarkCriterionFail
	c.Detail = "failure did not explain the missing dependency or next steps"
	return c
}

// failureExplanationPresent reports whether a failed run gave the operator an
// actionable cause: a failing step with detail or a populated next-steps list.
func failureExplanationPresent(env firstRunEnvelope) bool {
	if env.Error != nil && strings.TrimSpace(env.Error.Message) != "" {
		return len(env.Data.NextSteps) > 0
	}
	for _, step := range env.Data.Steps {
		if step.Status == firstRunStepFailed && strings.TrimSpace(step.Detail) != "" {
			return len(env.Data.NextSteps) > 0
		}
	}
	return false
}
