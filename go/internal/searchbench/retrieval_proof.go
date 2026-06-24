// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbench

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// RetrievalProofVersion is the first #417 semantic retrieval proof schema.
const RetrievalProofVersion = "semantic-retrieval-proof/v1"

// RetrievalProof compares the current baseline against one hybrid retrieval run.
type RetrievalProof struct {
	Version               string        `json:"version"`
	Suite                 QuerySuite    `json:"suite"`
	Baseline              RetrievalRun  `json:"baseline"`
	Candidate             RetrievalRun  `json:"candidate"`
	P95Threshold          time.Duration `json:"p95_threshold_ns"`
	AcceptedLatencyReason string        `json:"accepted_latency_reason"`
	AcceptedStopReason    string        `json:"accepted_stop_reason"`
}

// RetrievalRun records accuracy, latency, and observation evidence for one run.
type RetrievalRun struct {
	Backend     Backend                     `json:"backend"`
	Mode        Mode                        `json:"mode"`
	QueryCount  int                         `json:"query_count"`
	Latency     LatencySummary              `json:"latency"`
	Metrics     RetrievalMetrics            `json:"metrics"`
	Observation RetrievalObservationSummary `json:"observation"`
}

// RetrievalObservationSummary is the low-cardinality diagnostic summary for a run.
type RetrievalObservationSummary struct {
	Mode                      Mode                          `json:"mode"`
	QueryCount                int                           `json:"query_count"`
	ResultCount               ResultCountSummary            `json:"result_count"`
	TruncatedCount            int                           `json:"truncated_count"`
	TimeoutCount              int                           `json:"timeout_count"`
	CandidateTruthLevelCounts map[searchdocs.TruthLevel]int `json:"candidate_truth_level_counts"`
	FailureClasses            []FailureClass                `json:"failure_classes,omitempty"`
}

// ResultCountSummary records the observed min/max result counts for a run.
type ResultCountSummary struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// ValidateRetrievalProof checks the issue #417 hybrid retrieval evidence gate.
func ValidateRetrievalProof(proof RetrievalProof) error {
	var problems []string
	if proof.Version != RetrievalProofVersion {
		problems = append(problems, "version must be "+RetrievalProofVersion)
	}
	if err := ValidateQuerySuite(proof.Suite); err != nil {
		problems = append(problems, "suite is invalid: "+err.Error())
	}

	queryCount := len(proof.Suite.Queries)
	queryLimit := maxSuiteQueryLimit(proof.Suite)
	acceptedStopReason := strings.TrimSpace(proof.AcceptedStopReason)
	baselinePresent := retrievalRunPresent(proof.Baseline)
	candidatePresent := retrievalRunPresent(proof.Candidate)
	if acceptedStopReason != "" {
		if !strings.Contains(strings.ToLower(acceptedStopReason), "#417") {
			problems = append(problems, "accepted_stop_reason must reference issue #417")
		}
		if baselinePresent || candidatePresent {
			problems = append(problems, "accepted_stop_reason cannot be set when measured runs are present")
		}
		if proof.P95Threshold != 0 || strings.TrimSpace(proof.AcceptedLatencyReason) != "" {
			problems = append(problems, "accepted_stop_reason cannot be set with latency evidence")
		}
	}

	if baselinePresent {
		problems = append(problems, validateRetrievalRun("baseline", proof.Baseline, queryCount, queryLimit)...)
		if proof.Baseline.Backend != BackendPostgresContentSearch {
			problems = append(problems, "baseline.backend must be postgres_content_search")
		}
		if proof.Baseline.Mode != ModeKeyword {
			problems = append(problems, "baseline.mode must be keyword")
		}
	} else if acceptedStopReason == "" {
		problems = append(problems, "baseline run is required unless accepted_stop_reason is set")
	}

	if candidatePresent {
		problems = append(problems, validateRetrievalRun("candidate", proof.Candidate, queryCount, queryLimit)...)
		if proof.Candidate.Backend != BackendNornicDBHybrid {
			problems = append(problems, "candidate.backend must be nornicdb_hybrid")
		}
		if proof.Candidate.Mode != ModeHybrid {
			problems = append(problems, "candidate.mode must be hybrid")
		}
		if !baselinePresent {
			problems = append(problems, "baseline run is required when candidate run is present")
		} else if proof.Candidate.Metrics.Recall <= proof.Baseline.Metrics.Recall &&
			acceptedStopReason == "" {
			problems = append(problems, "candidate.metrics.recall must improve baseline.metrics.recall")
		}
		if proof.P95Threshold <= 0 {
			problems = append(problems, "p95_threshold is required")
		} else if proof.Candidate.Latency.P95 > proof.P95Threshold &&
			strings.TrimSpace(proof.AcceptedLatencyReason) == "" {
			problems = append(
				problems,
				"candidate.latency.p95 exceeds p95_threshold without accepted_latency_reason",
			)
		}
	} else if acceptedStopReason == "" {
		problems = append(problems, "candidate run is required unless accepted_stop_reason is set")
	}
	return joinedValidationError(problems)
}

func retrievalRunPresent(run RetrievalRun) bool {
	return run.Backend != "" ||
		run.Mode != "" ||
		run.QueryCount != 0 ||
		run.Latency.P50 != 0 ||
		run.Latency.P95 != 0 ||
		run.Metrics.Recall != 0 ||
		run.Metrics.Precision != 0 ||
		run.Metrics.NDCG != 0 ||
		run.Metrics.FalseCanonicalClaimCount != nil ||
		retrievalObservationPresent(run.Observation)
}

func retrievalObservationPresent(observation RetrievalObservationSummary) bool {
	return observation.Mode != "" ||
		observation.QueryCount != 0 ||
		observation.ResultCount.Min != 0 ||
		observation.ResultCount.Max != 0 ||
		observation.TruncatedCount != 0 ||
		observation.TimeoutCount != 0 ||
		len(observation.CandidateTruthLevelCounts) != 0 ||
		len(observation.FailureClasses) != 0
}

func validateRetrievalRun(
	prefix string,
	run RetrievalRun,
	expectedQueryCount int,
	queryLimit int,
) []string {
	var problems []string
	if !validBackend(run.Backend) {
		problems = append(problems, prefix+".backend is invalid")
	}
	if !validMode(run.Mode) {
		problems = append(problems, prefix+".mode is invalid")
	}
	if validBackend(run.Backend) && validMode(run.Mode) && !compatibleBackendMode(run.Backend, run.Mode) {
		problems = append(problems, fmt.Sprintf("%s.mode %s is not compatible with backend %s", prefix, run.Mode, run.Backend))
	}
	if run.QueryCount <= 0 {
		problems = append(problems, prefix+".query_count is required")
	} else if expectedQueryCount > 0 && run.QueryCount != expectedQueryCount {
		problems = append(problems, fmt.Sprintf("%s.query_count must equal suite query count", prefix))
	}
	if run.Latency.P50 <= 0 {
		problems = append(problems, prefix+".latency.p50 is required")
	}
	if run.Latency.P95 <= 0 {
		problems = append(problems, prefix+".latency.p95 is required")
	}
	if run.Latency.P50 > 0 && run.Latency.P95 > 0 && run.Latency.P95 < run.Latency.P50 {
		problems = append(problems, prefix+".latency.p95 must be greater than or equal to p50")
	}
	problems = append(problems, validateMetrics(prefix, run.Metrics)...)
	if run.Metrics.FalseCanonicalClaimCount != nil && *run.Metrics.FalseCanonicalClaimCount != 0 {
		problems = append(problems, prefix+".metrics.false_canonical_claim_count must be 0")
	}
	problems = append(problems, validateRetrievalObservation(prefix+".observation", run, queryLimit)...)
	return problems
}

func validateRetrievalObservation(prefix string, run RetrievalRun, queryLimit int) []string {
	var problems []string
	observation := run.Observation
	if observation.Mode != run.Mode {
		problems = append(problems, prefix+".mode must match run mode")
	}
	if observation.QueryCount <= 0 {
		problems = append(problems, prefix+".query_count is required")
	} else if run.QueryCount > 0 && observation.QueryCount != run.QueryCount {
		problems = append(problems, prefix+".query_count must match run query_count")
	}
	if observation.ResultCount.Min < 0 {
		problems = append(problems, prefix+".result_count.min must be non-negative")
	}
	if observation.ResultCount.Max < 0 {
		problems = append(problems, prefix+".result_count.max must be non-negative")
	}
	if observation.ResultCount.Min >= 0 &&
		observation.ResultCount.Max >= 0 &&
		observation.ResultCount.Max < observation.ResultCount.Min {
		problems = append(problems, prefix+".result_count.max must be greater than or equal to min")
	}
	if queryLimit > 0 && observation.ResultCount.Max > queryLimit {
		problems = append(problems, prefix+".result_count.max must not exceed suite query limit")
	}
	for name, count := range map[string]int{
		"truncated_count": observation.TruncatedCount,
		"timeout_count":   observation.TimeoutCount,
	} {
		if count < 0 {
			problems = append(problems, prefix+"."+name+" must be non-negative")
		}
		if run.QueryCount > 0 && count > run.QueryCount {
			problems = append(problems, prefix+"."+name+" must not exceed query_count")
		}
	}
	if observation.TruncatedCount > 0 &&
		!hasFailureClass(observation.FailureClasses, FailureClassTruncation) {
		problems = append(problems, prefix+".truncated_count requires truncation failure class")
	}
	if observation.TimeoutCount > 0 &&
		!hasFailureClass(observation.FailureClasses, FailureClassTimeout) {
		problems = append(problems, prefix+".timeout_count requires timeout failure class")
	}
	problems = append(problems, validateRetrievalTruthCounts(prefix, observation.CandidateTruthLevelCounts)...)
	for _, class := range observation.FailureClasses {
		if !validFailureClass(class) {
			problems = append(problems, fmt.Sprintf("%s.failure_classes[%s] is invalid", prefix, class))
		}
	}
	return problems
}

func maxSuiteQueryLimit(suite QuerySuite) int {
	maxLimit := 0
	for _, query := range suite.Queries {
		if query.Limit > maxLimit {
			maxLimit = query.Limit
		}
	}
	return maxLimit
}

func hasFailureClass(classes []FailureClass, want FailureClass) bool {
	for _, class := range classes {
		if class == want {
			return true
		}
	}
	return false
}

func validateRetrievalTruthCounts(
	prefix string,
	counts map[searchdocs.TruthLevel]int,
) []string {
	if len(counts) == 0 {
		return []string{prefix + ".candidate_truth_level_counts is required"}
	}
	var problems []string
	total := 0
	for level, count := range counts {
		if strings.TrimSpace(string(level)) == "" {
			problems = append(problems, prefix+".candidate_truth_level_counts level is required")
		}
		if count < 0 {
			problems = append(problems, fmt.Sprintf("%s.candidate_truth_level_counts[%s] must be non-negative", prefix, level))
			continue
		}
		total += count
	}
	if total == 0 {
		problems = append(problems, prefix+".candidate_truth_level_counts must include at least one candidate")
	}
	return problems
}
