// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firstrunbench

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cli/firstrun"
)

// completeEnvelope builds an envelope that represents a fully successful
// first-run: a bounded query returned an answer with a concrete source handle,
// truth metadata is present, and indexing completed.
func completeEnvelope() firstrun.Envelope {
	return firstrun.Envelope{
		Data: firstrun.Result{
			Command:       "first-run",
			RuntimeShape:  "local_binaries",
			ServiceURL:    "http://localhost:8080",
			RepoIndexed:   "complete",
			RepoTarget:    "/ws/demo",
			Readiness:     "indexing complete",
			QueryAnswered: true,
			QuerySummary:  "repositories query returned 1 (e.g. demo)",
			Steps: []firstrun.Step{
				{Name: "detect runtime", Status: firstrun.StepOK},
				{Name: "verify runtime", Status: firstrun.StepOK},
				{Name: "index repository", Status: firstrun.StepOK},
				{Name: "wait for readiness", Status: firstrun.StepOK},
				{Name: "first query", Status: firstrun.StepOK, Detail: "repositories query returned 1 (e.g. demo)"},
			},
		},
		Truth: map[string]any{
			"level":        "runtime",
			"freshness":    "current",
			"completeness": "complete",
			"profile":      "",
			"backend":      "nornicdb",
		},
		Error: nil,
	}
}

// TestEvaluatePassesOnCompleteProof is the positive guard: a result with
// QueryAnswered=true, truth metadata, and a source handle must produce a PASS
// verdict.
func TestEvaluatePassesOnCompleteProof(t *testing.T) {
	env := completeEnvelope()
	verdict := Evaluate(env, Measurements{Path: "local_binary", Elapsed: 42 * time.Second})

	if !verdict.Pass {
		t.Fatalf("verdict.Pass = false, want true; reasons: %v", verdict.FailureReasons())
	}
	if verdict.Criterion(CriterionFirstAnswer).Status != CriterionPass {
		t.Fatalf("first-answer criterion = %q, want pass", verdict.Criterion(CriterionFirstAnswer).Status)
	}
	if verdict.Criterion(CriterionTruthMetadata).Status != CriterionPass {
		t.Fatalf("truth-metadata criterion = %q, want pass", verdict.Criterion(CriterionTruthMetadata).Status)
	}
	if verdict.Criterion(CriterionSourceHandles).Status != CriterionPass {
		t.Fatalf("source-handles criterion = %q, want pass", verdict.Criterion(CriterionSourceHandles).Status)
	}
}

// TestEvaluateFailsOnHealthOnly is the mandatory correctness invariant from
// issue #1772: a "first answer" that comes from health/readiness state without
// a returned bounded query MUST be rejected.
func TestEvaluateFailsOnHealthOnly(t *testing.T) {
	env := completeEnvelope()
	// Readiness/health looks fine, but no bounded query ever returned.
	env.Data.QueryAnswered = false
	env.Data.QuerySummary = ""
	// A health-only run would still report readiness as complete.
	env.Data.Readiness = "indexing complete"
	env.Data.RepoIndexed = "complete"

	verdict := Evaluate(env, Measurements{Path: "local_binary"})

	if verdict.Pass {
		t.Fatal("verdict.Pass = true for a health-only result, want false (health-only must be rejected)")
	}
	if got := verdict.Criterion(CriterionFirstAnswer).Status; got != CriterionFail {
		t.Fatalf("first-answer criterion = %q, want fail for health-only result", got)
	}
}

// TestEvaluateFailsOnMissingTruth proves an answer without truth metadata is
// not trustworthy and must fail.
func TestEvaluateFailsOnMissingTruth(t *testing.T) {
	env := completeEnvelope()
	env.Truth = nil

	verdict := Evaluate(env, Measurements{Path: "local_binary"})

	if verdict.Pass {
		t.Fatal("verdict.Pass = true with no truth metadata, want false")
	}
	if got := verdict.Criterion(CriterionTruthMetadata).Status; got != CriterionFail {
		t.Fatalf("truth-metadata criterion = %q, want fail", got)
	}
}

// TestEvaluateFailsOnMissingSourceHandle proves an answer that returned but
// referenced no concrete source (0 repositories) lacks a source handle and
// must fail.
func TestEvaluateFailsOnMissingSourceHandle(t *testing.T) {
	env := completeEnvelope()
	env.Data.QuerySummary = "repositories query returned 0 repositories"

	verdict := Evaluate(env, Measurements{Path: "local_binary"})

	if verdict.Pass {
		t.Fatal("verdict.Pass = true with no source handle, want false")
	}
	if got := verdict.Criterion(CriterionSourceHandles).Status; got != CriterionFail {
		t.Fatalf("source-handles criterion = %q, want fail", got)
	}
}

// TestEvaluateFailsOnEnvelopeError proves a transport/run error envelope is a
// failure regardless of other fields.
func TestEvaluateFailsOnEnvelopeError(t *testing.T) {
	env := completeEnvelope()
	env.Error = &firstrun.EnvelopeError{Message: "verify runtime: no reachable API"}

	verdict := Evaluate(env, Measurements{Path: "local_binary"})

	if verdict.Pass {
		t.Fatal("verdict.Pass = true with an error envelope, want false")
	}
}

// TestEvaluateMarksUnknownTimeNotMeasured proves that when no elapsed time is
// supplied the harness records not-measured rather than fabricating a
// duration, and that this does not by itself fail the benchmark.
func TestEvaluateMarksUnknownTimeNotMeasured(t *testing.T) {
	env := completeEnvelope()

	verdict := Evaluate(env, Measurements{Path: "local_binary"})

	if got := verdict.Criterion(CriterionTimeToAnswer).Status; got != CriterionNotMeasured {
		t.Fatalf("time-to-answer criterion = %q, want not_measured when no elapsed supplied", got)
	}
	if !verdict.Pass {
		t.Fatalf("verdict.Pass = false; a not-measured timing must not fail an otherwise-complete run; reasons: %v", verdict.FailureReasons())
	}
}

// TestEvaluateExplainsFailure proves a failed run with a populated next-steps
// list scores the failure-explanation criterion as pass, and one without any
// explanation scores it as fail. Both walk the real failureExplanationPresent
// path so the two outcomes exercise the same production code.
func TestEvaluateExplainsFailure(t *testing.T) {
	explained := completeEnvelope()
	explained.Data.QueryAnswered = false
	explained.Data.QuerySummary = ""
	explained.Data.Steps = append(explained.Data.Steps, firstrun.Step{
		Name: "first query", Status: firstrun.StepFailed, Detail: "query timed out",
	})
	explained.Data.NextSteps = []string{"Re-run: eshu first-run"}

	verdict := Evaluate(explained, Measurements{Path: "local_binary"})
	if got := verdict.Criterion(CriterionFailureExplanation).Status; got != CriterionPass {
		t.Fatalf("failure-explanation criterion = %q, want pass when a failed step has detail and next steps", got)
	}

	unexplained := completeEnvelope()
	unexplained.Data.QueryAnswered = false
	unexplained.Data.QuerySummary = ""
	unexplained.Data.NextSteps = nil

	verdict = Evaluate(unexplained, Measurements{Path: "local_binary"})
	if got := verdict.Criterion(CriterionFailureExplanation).Status; got != CriterionFail {
		t.Fatalf("failure-explanation criterion = %q, want fail when nothing explains the failure", got)
	}
}

// TestVerdictCriterionMissingRowIsDistinct proves an unscored criterion comes
// back as a zero-value row with the requested name, visibly distinct from any
// real outcome.
func TestVerdictCriterionMissingRowIsDistinct(t *testing.T) {
	verdict := Verdict{Path: "local_binary"}
	row := verdict.Criterion(CriterionFirstAnswer)
	if row.Name != CriterionFirstAnswer {
		t.Fatalf("row.Name = %q, want %q", row.Name, CriterionFirstAnswer)
	}
	if row.Status != "" {
		t.Fatalf("row.Status = %q, want empty for a missing row", row.Status)
	}
}

// TestManualStepsRecordedWhenDeclared proves a declared manual-step count is
// recorded as a pass row and an undeclared (negative) count records
// not-measured instead of a fabricated number.
func TestManualStepsRecordedWhenDeclared(t *testing.T) {
	env := completeEnvelope()

	declared := Evaluate(env, Measurements{Path: "local_binary", ManualSteps: 3})
	if got := declared.Criterion(CriterionManualSteps); got.Status != CriterionPass || !strings.Contains(got.Detail, "3") {
		t.Fatalf("manual-steps criterion = %+v, want pass recording 3", got)
	}

	undeclared := Evaluate(env, Measurements{Path: "local_binary", ManualSteps: NotMeasuredManualSteps})
	if got := undeclared.Criterion(CriterionManualSteps).Status; got != CriterionNotMeasured {
		t.Fatalf("manual-steps criterion = %q, want not_measured for an undeclared count", got)
	}
}
