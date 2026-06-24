// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
	"time"
)

// completeBenchmarkEnvelope builds an envelope that represents a fully
// successful first-run: a bounded query returned an answer with a concrete
// source handle, truth metadata is present, and indexing completed.
func completeBenchmarkEnvelope() firstRunEnvelope {
	return firstRunEnvelope{
		Data: firstRunResult{
			Command:       "first-run",
			RuntimeShape:  firstRunShapeLocalBinaries,
			ServiceURL:    "http://localhost:8080",
			RepoIndexed:   "complete",
			RepoTarget:    "/ws/demo",
			Readiness:     "indexing complete",
			QueryAnswered: true,
			QuerySummary:  "repositories query returned 1 (e.g. demo)",
			Steps: []firstRunStep{
				{Name: "detect runtime", Status: firstRunStepOK},
				{Name: "verify runtime", Status: firstRunStepOK},
				{Name: "index repository", Status: firstRunStepOK},
				{Name: "wait for readiness", Status: firstRunStepOK},
				{Name: "first query", Status: firstRunStepOK, Detail: "repositories query returned 1 (e.g. demo)"},
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

// TestEvaluateFirstAnswerBenchmarkPassesOnCompleteProof is the positive guard:
// a result with QueryAnswered=true, truth metadata, and a source handle must
// produce a PASS verdict.
func TestEvaluateFirstAnswerBenchmarkPassesOnCompleteProof(t *testing.T) {
	env := completeBenchmarkEnvelope()
	verdict := evaluateFirstAnswerBenchmark(env, benchmarkMeasurements{Path: "local_binary", Elapsed: 42 * time.Second})

	if !verdict.Pass {
		t.Fatalf("verdict.Pass = false, want true; reasons: %v", verdict.failureReasons())
	}
	if verdict.criterion(criterionFirstAnswer).Status != benchmarkCriterionPass {
		t.Fatalf("first-answer criterion = %q, want pass", verdict.criterion(criterionFirstAnswer).Status)
	}
	if verdict.criterion(criterionTruthMetadata).Status != benchmarkCriterionPass {
		t.Fatalf("truth-metadata criterion = %q, want pass", verdict.criterion(criterionTruthMetadata).Status)
	}
	if verdict.criterion(criterionSourceHandles).Status != benchmarkCriterionPass {
		t.Fatalf("source-handles criterion = %q, want pass", verdict.criterion(criterionSourceHandles).Status)
	}
}

// TestEvaluateFirstAnswerBenchmarkFailsOnHealthOnly is the mandatory
// correctness invariant from issue #1772: a "first answer" that comes from
// health/readiness state without a returned bounded query MUST be rejected.
func TestEvaluateFirstAnswerBenchmarkFailsOnHealthOnly(t *testing.T) {
	env := completeBenchmarkEnvelope()
	// Readiness/health looks fine, but no bounded query ever returned.
	env.Data.QueryAnswered = false
	env.Data.QuerySummary = ""
	// A health-only run would still report readiness as complete.
	env.Data.Readiness = "indexing complete"
	env.Data.RepoIndexed = "complete"

	verdict := evaluateFirstAnswerBenchmark(env, benchmarkMeasurements{Path: "local_binary"})

	if verdict.Pass {
		t.Fatal("verdict.Pass = true for a health-only result, want false (health-only must be rejected)")
	}
	if got := verdict.criterion(criterionFirstAnswer).Status; got != benchmarkCriterionFail {
		t.Fatalf("first-answer criterion = %q, want fail for health-only result", got)
	}
}

// TestEvaluateFirstAnswerBenchmarkFailsOnMissingTruth proves an answer without
// truth metadata is not trustworthy and must fail.
func TestEvaluateFirstAnswerBenchmarkFailsOnMissingTruth(t *testing.T) {
	env := completeBenchmarkEnvelope()
	env.Truth = nil

	verdict := evaluateFirstAnswerBenchmark(env, benchmarkMeasurements{Path: "local_binary"})

	if verdict.Pass {
		t.Fatal("verdict.Pass = true with no truth metadata, want false")
	}
	if got := verdict.criterion(criterionTruthMetadata).Status; got != benchmarkCriterionFail {
		t.Fatalf("truth-metadata criterion = %q, want fail", got)
	}
}

// TestEvaluateFirstAnswerBenchmarkFailsOnMissingSourceHandle proves an answer
// that returned but referenced no concrete source (0 repositories) lacks a
// source handle and must fail.
func TestEvaluateFirstAnswerBenchmarkFailsOnMissingSourceHandle(t *testing.T) {
	env := completeBenchmarkEnvelope()
	env.Data.QuerySummary = "repositories query returned 0 repositories"

	verdict := evaluateFirstAnswerBenchmark(env, benchmarkMeasurements{Path: "local_binary"})

	if verdict.Pass {
		t.Fatal("verdict.Pass = true with no source handle, want false")
	}
	if got := verdict.criterion(criterionSourceHandles).Status; got != benchmarkCriterionFail {
		t.Fatalf("source-handles criterion = %q, want fail", got)
	}
}

// TestEvaluateFirstAnswerBenchmarkFailsOnEnvelopeError proves a transport/run
// error envelope is a failure regardless of other fields.
func TestEvaluateFirstAnswerBenchmarkFailsOnEnvelopeError(t *testing.T) {
	env := completeBenchmarkEnvelope()
	env.Error = &firstRunEnvelopeError{Message: "verify runtime: no reachable API"}

	verdict := evaluateFirstAnswerBenchmark(env, benchmarkMeasurements{Path: "local_binary"})

	if verdict.Pass {
		t.Fatal("verdict.Pass = true with an error envelope, want false")
	}
}

// TestEvaluateFirstAnswerBenchmarkMarksUnknownTimeNotMeasured proves that when
// no elapsed time is supplied the harness records not-measured rather than
// fabricating a duration, and that this does not by itself fail the benchmark.
func TestEvaluateFirstAnswerBenchmarkMarksUnknownTimeNotMeasured(t *testing.T) {
	env := completeBenchmarkEnvelope()

	verdict := evaluateFirstAnswerBenchmark(env, benchmarkMeasurements{Path: "local_binary"})

	if got := verdict.criterion(criterionTimeToAnswer).Status; got != benchmarkCriterionNotMeasured {
		t.Fatalf("time-to-answer criterion = %q, want not_measured when no elapsed supplied", got)
	}
	if !verdict.Pass {
		t.Fatalf("verdict.Pass = false; a not-measured timing must not fail an otherwise-complete run; reasons: %v", verdict.failureReasons())
	}
}

// TestParseFirstRunEnvelopeRoundTrips proves the parser reads the canonical
// envelope emitted by `eshu first-run --json` (top-level data/truth/error).
func TestParseFirstRunEnvelopeRoundTrips(t *testing.T) {
	raw := `{
  "data": {
    "command": "first-run",
    "runtime_shape": "local_binaries",
    "service_url": "http://localhost:8080",
    "repo_indexed": "complete",
    "repo_target": "/ws/demo",
    "readiness": "indexing complete",
    "query_answered": true,
    "query_summary": "repositories query returned 1 (e.g. demo)",
    "steps": []
  },
  "truth": {"level": "runtime", "completeness": "complete", "freshness": "current"},
  "error": null
}`
	env, err := parseFirstRunEnvelope([]byte(raw))
	if err != nil {
		t.Fatalf("parseFirstRunEnvelope error: %v", err)
	}
	if !env.Data.QueryAnswered {
		t.Fatal("Data.QueryAnswered = false, want true")
	}
	if env.Truth["completeness"] != "complete" {
		t.Fatalf("Truth completeness = %v, want complete", env.Truth["completeness"])
	}
	if env.Error != nil {
		t.Fatalf("Error = %v, want nil", env.Error)
	}
}

// TestParseFirstRunEnvelopeCapturesError proves an error envelope round-trips
// so the evaluator can reject failed runs.
func TestParseFirstRunEnvelopeCapturesError(t *testing.T) {
	raw := `{"data":{"command":"first-run","query_answered":false},"truth":{"completeness":"partial"},"error":{"message":"verify runtime: no reachable API"}}`
	env, err := parseFirstRunEnvelope([]byte(raw))
	if err != nil {
		t.Fatalf("parseFirstRunEnvelope error: %v", err)
	}
	if env.Error == nil || !strings.Contains(env.Error.Message, "no reachable API") {
		t.Fatalf("Error = %v, want message containing 'no reachable API'", env.Error)
	}
}
