package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/searchhybrid"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

type capSweepResult struct {
	Cap      int
	Indexed  int
	Overflow int
	Build    time.Duration
	Latency  latency
	Score    searchbench.QuerySuiteScore
}

type capSweepObserver struct {
	observations []searchretrieval.Observation
}

func (o *capSweepObserver) ObserveRetrieval(_ context.Context, observation searchretrieval.Observation) {
	o.observations = append(o.observations, observation)
}

func loadQuerySuite(path string) (searchbench.QuerySuite, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return searchbench.QuerySuite{}, fmt.Errorf("read query suite: %w", err)
	}
	var wrapped struct {
		Suite searchbench.QuerySuite `json:"suite"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return searchbench.QuerySuite{}, fmt.Errorf("decode query suite: %w", err)
	}
	if wrapped.Suite.Version != "" || len(wrapped.Suite.Queries) != 0 {
		if err := searchbench.ValidateQuerySuite(wrapped.Suite); err != nil {
			return searchbench.QuerySuite{}, fmt.Errorf("invalid query suite: %w", err)
		}
		return wrapped.Suite, nil
	}
	var suite searchbench.QuerySuite
	if err := json.Unmarshal(raw, &suite); err != nil {
		return searchbench.QuerySuite{}, fmt.Errorf("decode query suite: %w", err)
	}
	if err := searchbench.ValidateQuerySuite(suite); err != nil {
		return searchbench.QuerySuite{}, fmt.Errorf("invalid query suite: %w", err)
	}
	return suite, nil
}

func parseCorpusCaps(raw string, available int) ([]int, error) {
	if available <= 0 {
		return nil, fmt.Errorf("available corpus size must be positive")
	}
	parts := strings.Split(raw, ",")
	seen := make(map[int]struct{}, len(parts))
	caps := make([]int, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		capSize := available
		if !strings.EqualFold(value, "all") {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("invalid corpus cap %q: %w", value, err)
			}
			if parsed <= 0 {
				return nil, fmt.Errorf("corpus cap must be positive: %d", parsed)
			}
			capSize = parsed
			if capSize > available {
				capSize = available
			}
		}
		if _, ok := seen[capSize]; ok {
			continue
		}
		seen[capSize] = struct{}{}
		caps = append(caps, capSize)
	}
	if len(caps) == 0 {
		return nil, fmt.Errorf("at least one corpus cap is required")
	}
	sort.Ints(caps)
	return caps, nil
}

func runCapSweep(
	ctx context.Context,
	docs []searchdocs.Document,
	suite searchbench.QuerySuite,
	caps []int,
	timeout time.Duration,
) ([]capSweepResult, error) {
	results := make([]capSweepResult, 0, len(caps))
	for _, capSize := range caps {
		start := time.Now()
		index, err := searchhybrid.NewIndex(docs, searchhybrid.Options{MaxDocuments: capSize})
		if err != nil {
			return nil, fmt.Errorf("build index for cap %d: %w", capSize, err)
		}
		buildDuration := time.Since(start)
		observer := &capSweepObserver{observations: make([]searchretrieval.Observation, 0, len(suite.Queries))}
		runner := searchretrieval.Runner{
			Backend:  searchhybrid.Backend{Index: index},
			Observer: observer,
		}
		resultsByID := make(map[string][]searchbench.Result, len(suite.Queries))
		totalResults := 0
		for _, query := range suite.Queries {
			req := searchretrieval.Request{
				QueryID: query.ID,
				Query:   query.Text,
				Scope: searchretrieval.Scope{
					ServiceID:   query.ServiceID,
					WorkloadID:  query.WorkloadID,
					RepoID:      query.RepoID,
					Environment: query.Environment,
				},
				Mode:    query.Mode,
				Limit:   query.Limit,
				Timeout: timeout,
			}
			response, err := runner.Retrieve(ctx, req)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return nil, fmt.Errorf("cap sweep canceled: %w", ctxErr)
				}
				continue
			}
			resultsByID[query.ID] = response.SearchbenchResults()
			totalResults += len(response.Results)
		}
		score, err := searchbench.ScoreQuerySuite(suite, resultsByID)
		if err != nil {
			return nil, fmt.Errorf("score suite for cap %d: %w", capSize, err)
		}
		durations := make([]time.Duration, 0, len(observer.observations))
		for _, observation := range observer.observations {
			durations = append(durations, observation.Duration)
		}
		results = append(results, capSweepResult{
			Cap:      capSize,
			Indexed:  index.Size(),
			Overflow: index.Overflow(),
			Build:    buildDuration,
			Latency: latency{
				Queries: len(suite.Queries),
				Results: totalResults,
				P50:     percentile(durations, 50),
				P95:     percentile(durations, 95),
				Max:     percentile(durations, 100),
			},
			Score: score,
		})
	}
	return results, nil
}

func printCapSweepReport(stats corpusStats, suitePath string, results []capSweepResult) {
	fmt.Printf("# search-bench: semantic search cap sweep\n\n")
	fmt.Printf("Corpus (repo %s): %d curated documents\n", stats.RepoID, stats.Documents)
	fmt.Printf("Suite: %s\n\n", suitePath)
	fmt.Printf("%10s %10s %10s %12s %12s %12s %8s %8s %8s %8s\n",
		"cap", "indexed", "overflow", "build", "p50", "p95", "recall", "prec", "ndcg", "false")
	for _, result := range results {
		falseClaims := 0
		if result.Score.Metrics.FalseCanonicalClaimCount != nil {
			falseClaims = *result.Score.Metrics.FalseCanonicalClaimCount
		}
		fmt.Printf(
			"%10d %10d %10d %12s %12s %12s %8.3f %8.3f %8.3f %8d\n",
			result.Cap,
			result.Indexed,
			result.Overflow,
			result.Build.Round(time.Millisecond),
			result.Latency.P50.Round(time.Microsecond),
			result.Latency.P95.Round(time.Microsecond),
			result.Score.Metrics.Recall,
			result.Score.Metrics.Precision,
			result.Score.Metrics.NDCG,
			falseClaims,
		)
	}
	fmt.Printf("\nBenchmark Evidence: cap sweep uses a validated searchbench.QuerySuite with expected graph handles; recall, precision, nDCG, latency, indexed count, overflow, and build time are measured per corpus cap.\n")
}
