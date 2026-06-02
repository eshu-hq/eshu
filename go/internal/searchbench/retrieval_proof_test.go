package searchbench

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

func TestValidateRetrievalProofRejectsMissingGuardrails(t *testing.T) {
	t.Parallel()

	proof := validRetrievalProofFixture()
	proof.Version = ""
	proof.Candidate.Metrics.Recall = proof.Baseline.Metrics.Recall
	proof.P95Threshold = 50 * time.Millisecond
	proof.AcceptedLatencyReason = ""
	oneFalseClaim := 1
	proof.Candidate.Metrics.FalseCanonicalClaimCount = &oneFalseClaim
	proof.Candidate.Observation.CandidateTruthLevelCounts = nil

	err := ValidateRetrievalProof(proof)
	if err == nil {
		t.Fatal("ValidateRetrievalProof() error = nil, want guardrail errors")
	}
	for _, want := range []string{
		"version must be semantic-retrieval-proof/v1",
		"candidate.metrics.recall must improve baseline.metrics.recall",
		"candidate.latency.p95 exceeds p95_threshold without accepted_latency_reason",
		"candidate.metrics.false_canonical_claim_count must be 0",
		"candidate.observation.candidate_truth_level_counts is required",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateRetrievalProof() error = %q, want substring %q", err, want)
		}
	}
}

func TestValidateRetrievalProofAcceptsImprovedHybridProof(t *testing.T) {
	t.Parallel()

	if err := ValidateRetrievalProof(validRetrievalProofFixture()); err != nil {
		t.Fatalf("ValidateRetrievalProof() error = %v, want nil", err)
	}
}

func TestValidateRetrievalProofAllowsAcceptedLatencyReason(t *testing.T) {
	t.Parallel()

	proof := validRetrievalProofFixture()
	proof.P95Threshold = 50 * time.Millisecond
	proof.AcceptedLatencyReason = "hybrid first-query proof exceeded the stop threshold; follow-up profiling is linked."

	if err := ValidateRetrievalProof(proof); err != nil {
		t.Fatalf("ValidateRetrievalProof() error = %v, want nil", err)
	}
}

func TestValidateRetrievalProofRejectsWrongBackends(t *testing.T) {
	t.Parallel()

	proof := validRetrievalProofFixture()
	proof.Baseline.Backend = BackendNornicDBBM25
	proof.Candidate.Backend = BackendNornicDBVector
	proof.Candidate.Mode = ModeSemantic

	err := ValidateRetrievalProof(proof)
	if err == nil {
		t.Fatal("ValidateRetrievalProof() error = nil, want backend errors")
	}
	for _, want := range []string{
		"baseline.backend must be postgres_content_search",
		"candidate.backend must be nornicdb_hybrid",
		"candidate.mode must be hybrid",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateRetrievalProof() error = %q, want substring %q", err, want)
		}
	}
}

func TestValidateRetrievalProofRejectsUnboundedObservationCounts(t *testing.T) {
	t.Parallel()

	proof := validRetrievalProofFixture()
	proof.Candidate.Observation.ResultCount.Max = MaximumQueryLimit + 1
	proof.Candidate.Observation.TimeoutCount = 1
	proof.Candidate.Observation.FailureClasses = nil

	err := ValidateRetrievalProof(proof)
	if err == nil {
		t.Fatal("ValidateRetrievalProof() error = nil, want observation count errors")
	}
	for _, want := range []string{
		"candidate.observation.result_count.max must not exceed suite query limit",
		"candidate.observation.truncated_count requires truncation failure class",
		"candidate.observation.timeout_count requires timeout failure class",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ValidateRetrievalProof() error = %q, want substring %q", err, want)
		}
	}
}

func TestRetrievalProofJSONUsesVersionedFieldNames(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(validRetrievalProofFixture())
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	body := string(payload)
	for _, want := range []string{
		`"semantic-retrieval-proof/v1"`,
		`"p95_threshold_ns"`,
		`"accepted_latency_reason"`,
		`"candidate_truth_level_counts"`,
		`"false_canonical_claim_count"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("json payload = %s, want field/value %s", body, want)
		}
	}
	if strings.Contains(body, "P95Threshold") ||
		strings.Contains(body, "CandidateTruthLevelCounts") {
		t.Fatalf("json payload = %s, want stable evidence field names", body)
	}
}

func validRetrievalProofFixture() RetrievalProof {
	falseClaims := 0
	return RetrievalProof{
		Version:      RetrievalProofVersion,
		Suite:        validQuerySuiteFixture(),
		P95Threshold: 100 * time.Millisecond,
		Baseline: RetrievalRun{
			Backend:    BackendPostgresContentSearch,
			Mode:       ModeKeyword,
			QueryCount: MinimumQuerySuiteSize,
			Latency:    LatencySummary{P50: 35 * time.Millisecond, P95: 90 * time.Millisecond},
			Metrics: RetrievalMetrics{
				Recall:                   0.60,
				Precision:                0.70,
				NDCG:                     0.68,
				FalseCanonicalClaimCount: &falseClaims,
			},
			Observation: RetrievalObservationSummary{
				Mode:       ModeKeyword,
				QueryCount: MinimumQuerySuiteSize,
				ResultCount: ResultCountSummary{
					Min: 3,
					Max: 10,
				},
				CandidateTruthLevelCounts: map[searchdocs.TruthLevel]int{
					searchdocs.TruthLevelDerived: 150,
				},
			},
		},
		Candidate: RetrievalRun{
			Backend:    BackendNornicDBHybrid,
			Mode:       ModeHybrid,
			QueryCount: MinimumQuerySuiteSize,
			Latency:    LatencySummary{P50: 22 * time.Millisecond, P95: 80 * time.Millisecond},
			Metrics: RetrievalMetrics{
				Recall:                   0.80,
				Precision:                0.72,
				NDCG:                     0.84,
				FalseCanonicalClaimCount: &falseClaims,
			},
			Observation: RetrievalObservationSummary{
				Mode:           ModeHybrid,
				QueryCount:     MinimumQuerySuiteSize,
				TruncatedCount: 2,
				ResultCount: ResultCountSummary{
					Min: 5,
					Max: 10,
				},
				CandidateTruthLevelCounts: map[searchdocs.TruthLevel]int{
					searchdocs.TruthLevelDerived: 180,
				},
				FailureClasses: []FailureClass{FailureClassTruncation},
			},
		},
	}
}
