package semanticeval

import (
	"fmt"
	"math"
	"sort"
)

// Options controls semantic retrieval scoring.
type Options struct {
	K int
}

// Report summarizes retrieval quality for one run.
type Report struct {
	K         int         `json:"k"`
	CaseCount int         `json:"case_count"`
	Averages  Metrics     `json:"averages"`
	Cases     []CaseScore `json:"cases"`
}

// Metrics captures the aggregate retrieval quality values.
type Metrics struct {
	RecallAtK            float64 `json:"recall_at_k"`
	PrecisionAtK         float64 `json:"precision_at_k"`
	NDCGAtK              float64 `json:"ndcg_at_k"`
	FalseCanonicalClaims int     `json:"false_canonical_claims"`
	ForbiddenHits        int     `json:"forbidden_hits"`
	UnsupportedCaseCount int     `json:"unsupported_case_count"`
	MeanLatencyMS        float64 `json:"mean_latency_ms,omitempty"`
	P95LatencyMS         float64 `json:"p95_latency_ms,omitempty"`
}

// CaseScore captures retrieval quality for one eval case.
type CaseScore struct {
	CaseID               string  `json:"case_id"`
	RecallAtK            float64 `json:"recall_at_k"`
	PrecisionAtK         float64 `json:"precision_at_k"`
	NDCGAtK              float64 `json:"ndcg_at_k"`
	FalseCanonicalClaims int     `json:"false_canonical_claims"`
	ForbiddenHits        int     `json:"forbidden_hits"`
	Unsupported          bool    `json:"unsupported"`
	LatencyMS            float64 `json:"latency_ms,omitempty"`
}

// Score compares a run against an eval suite and returns retrieval metrics.
func Score(suite Suite, run Run, options Options) (Report, error) {
	if err := suite.Validate(); err != nil {
		return Report{}, err
	}
	k := options.K
	if k <= 0 {
		k = 10
	}
	resultsByCase, err := indexedRunResults(run)
	if err != nil {
		return Report{}, err
	}
	if err := rejectUnknownRunCases(suite, resultsByCase); err != nil {
		return Report{}, err
	}

	report := Report{K: k, CaseCount: len(suite.Cases)}
	latencies := make([]float64, 0, len(suite.Cases))
	for _, evalCase := range suite.Cases {
		caseResult := resultsByCase[evalCase.ID]
		score := scoreCase(evalCase, caseResult, k)
		report.Cases = append(report.Cases, score)
		report.Averages.RecallAtK += score.RecallAtK
		report.Averages.PrecisionAtK += score.PrecisionAtK
		report.Averages.NDCGAtK += score.NDCGAtK
		report.Averages.FalseCanonicalClaims += score.FalseCanonicalClaims
		report.Averages.ForbiddenHits += score.ForbiddenHits
		report.Averages.MeanLatencyMS += score.LatencyMS
		latencies = append(latencies, score.LatencyMS)
		if score.Unsupported {
			report.Averages.UnsupportedCaseCount++
		}
	}

	denominator := float64(len(suite.Cases))
	report.Averages.RecallAtK /= denominator
	report.Averages.PrecisionAtK /= denominator
	report.Averages.NDCGAtK /= denominator
	report.Averages.MeanLatencyMS /= denominator
	report.Averages.P95LatencyMS = percentileNearestRank(latencies, 0.95)
	return report, nil
}

func indexedRunResults(run Run) (map[string]CaseResult, error) {
	results := make(map[string]CaseResult, len(run.Results))
	for _, result := range run.Results {
		if result.CaseID == "" {
			return nil, fmt.Errorf("run result case_id must not be blank")
		}
		if _, ok := results[result.CaseID]; ok {
			return nil, fmt.Errorf("duplicate run result for case %q", result.CaseID)
		}
		for _, candidate := range result.Candidates {
			if candidate.Handle == "" {
				return nil, fmt.Errorf("case %q candidate handle must not be blank", result.CaseID)
			}
			if err := candidate.Truth.Validate(); err != nil {
				return nil, fmt.Errorf("case %q candidate %q: %w", result.CaseID, candidate.Handle, err)
			}
		}
		results[result.CaseID] = result
	}
	return results, nil
}

func rejectUnknownRunCases(suite Suite, results map[string]CaseResult) error {
	known := make(map[string]struct{}, len(suite.Cases))
	for _, evalCase := range suite.Cases {
		known[evalCase.ID] = struct{}{}
	}
	for caseID := range results {
		if _, ok := known[caseID]; !ok {
			return fmt.Errorf("run result references unknown case %q", caseID)
		}
	}
	return nil
}

func scoreCase(evalCase Case, result CaseResult, k int) CaseScore {
	expectedByHandle := make(map[string]ExpectedHandle, len(evalCase.Expected))
	requiredCount := 0
	for _, expected := range evalCase.Expected {
		expectedByHandle[expected.Handle] = expected
		if expected.Required {
			requiredCount++
		}
	}
	forbidden := make(map[string]struct{}, len(evalCase.MustNotInclude))
	for _, handle := range evalCase.MustNotInclude {
		forbidden[handle] = struct{}{}
	}

	score := CaseScore{CaseID: evalCase.ID, LatencyMS: result.LatencyMS}
	topK := min(k, len(result.Candidates))
	requiredHits := 0
	relevantHits := 0
	for rank := 0; rank < topK; rank++ {
		candidate := result.Candidates[rank]
		expected, relevant := expectedByHandle[candidate.Handle]
		if relevant {
			relevantHits++
			if expected.Required {
				requiredHits++
			}
			score.NDCGAtK += discountedGain(expected.Relevance, rank+1)
			if candidate.Truth == TruthClassExact && expected.MaxTruth != TruthClassExact {
				score.FalseCanonicalClaims++
			}
		}
		if _, ok := forbidden[candidate.Handle]; ok {
			score.ForbiddenHits++
		}
		if candidate.Truth == TruthClassUnsupported {
			score.Unsupported = true
		}
	}

	score.RecallAtK = float64(requiredHits) / float64(requiredCount)
	score.PrecisionAtK = float64(relevantHits) / float64(k)
	score.NDCGAtK = normalizedDCG(evalCase.Expected, score.NDCGAtK, k)
	return score
}

func normalizedDCG(expected []ExpectedHandle, dcg float64, k int) float64 {
	gains := make([]int, 0, len(expected))
	for _, handle := range expected {
		gains = append(gains, handle.Relevance)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(gains)))
	var ideal float64
	for idx, relevance := range gains {
		if idx >= k {
			break
		}
		ideal += discountedGain(relevance, idx+1)
	}
	if ideal == 0 {
		return 0
	}
	return dcg / ideal
}

func discountedGain(relevance int, rank int) float64 {
	return float64(relevance) / math.Log2(float64(rank+1))
}

func percentileNearestRank(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sortedValues := append([]float64(nil), values...)
	sort.Float64s(sortedValues)
	rank := int(math.Ceil(percentile * float64(len(sortedValues))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sortedValues) {
		rank = len(sortedValues)
	}
	return sortedValues[rank-1]
}
