// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbenchrun

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchretrieval"
)

// BackendDescriptor carries the operator-measured backend metadata that the
// live executor cannot derive from query execution alone. RunSuite fills the
// query-derived fields (query count, latency, accuracy metrics) and merges these
// descriptor fields into the returned BackendRun.
type BackendDescriptor struct {
	// Backend identifies the measured search backend. It also determines the
	// retrieval mode, because each backend serves exactly one compatible mode.
	Backend searchbench.Backend
	// BackendImage and BackendCommit identify the backend build under test.
	// ValidateEvidence requires at least one to be set.
	BackendImage  string
	BackendCommit string
	// SearchFlags records the effective NornicDB search controls. It is required
	// for NornicDB backends and ignored for the Postgres baseline.
	SearchFlags *searchbench.NornicDBSearchFlags
	// Startup, MemoryHighWaterBytes, IndexArtifactBytes, and RebuildBehavior are
	// process-level measurements supplied by the harness, not the query loop.
	Startup              searchbench.StartupSummary
	MemoryHighWaterBytes int64
	IndexArtifactBytes   int64
	RebuildBehavior      string
	// QueryTimeout bounds every retrieval attempt. It is required.
	QueryTimeout time.Duration
}

// SuiteRun is the measured output of executing one suite against one backend.
type SuiteRun struct {
	// Run is the assembled benchmark backend measurement.
	Run searchbench.BackendRun
	// Score is the aggregate and per-query accuracy score for the run.
	Score searchbench.QuerySuiteScore
	// Observations are the bounded retrieval observations, in suite order.
	Observations []searchretrieval.Observation
}

// captureObserver records every retrieval observation in call order.
type captureObserver struct {
	observations []searchretrieval.Observation
}

func (c *captureObserver) ObserveRetrieval(_ context.Context, observation searchretrieval.Observation) {
	c.observations = append(c.observations, observation)
}

// RunSuite executes every suite query against backend through a bounded
// retrieval runner, measures p50/p95 query latency from runner observations,
// scores the normalized results, and merges descriptor metadata into a
// populated BackendRun. A per-query backend or timeout error is recorded as a
// failed query (no results) rather than aborting the run; only parent context
// cancellation aborts.
func RunSuite(
	ctx context.Context,
	suite searchbench.QuerySuite,
	backend searchretrieval.Backend,
	desc BackendDescriptor,
) (SuiteRun, error) {
	if err := searchbench.ValidateQuerySuite(suite); err != nil {
		return SuiteRun{}, fmt.Errorf("searchbenchrun: invalid suite: %w", err)
	}
	if backend == nil {
		return SuiteRun{}, errors.New("searchbenchrun: backend is required")
	}
	if desc.QueryTimeout <= 0 {
		return SuiteRun{}, errors.New("searchbenchrun: query timeout is required")
	}
	mode := modeForBackend(desc.Backend)
	if mode == "" {
		return SuiteRun{}, fmt.Errorf("searchbenchrun: unknown backend %q", desc.Backend)
	}

	capture := &captureObserver{observations: make([]searchretrieval.Observation, 0, len(suite.Queries))}
	runner := searchretrieval.Runner{Backend: backend, Observer: capture}
	resultsByID := make(map[string][]searchbench.Result, len(suite.Queries))

	for _, query := range suite.Queries {
		// Abort the whole run on parent cancellation, even if a backend ignores
		// its context. A per-query timeout is handled below as benchmark data.
		if err := ctx.Err(); err != nil {
			return SuiteRun{}, fmt.Errorf("searchbenchrun: run canceled: %w", err)
		}
		req := searchretrieval.Request{
			QueryID: query.ID,
			Query:   query.Text,
			Scope: searchretrieval.Scope{
				ServiceID:   query.ServiceID,
				WorkloadID:  query.WorkloadID,
				RepoID:      query.RepoID,
				Environment: query.Environment,
			},
			Mode:    mode,
			Limit:   query.Limit,
			Timeout: desc.QueryTimeout,
		}
		response, err := runner.Retrieve(ctx, req)
		if err != nil {
			// A canceled parent context means the whole run is abandoned; a
			// per-query timeout or backend error is benchmark data captured in
			// the observation, so the run continues with zero results scored.
			if ctxErr := ctx.Err(); ctxErr != nil {
				return SuiteRun{}, fmt.Errorf("searchbenchrun: run canceled: %w", ctxErr)
			}
			continue
		}
		resultsByID[query.ID] = response.SearchbenchResults()
	}

	score, err := searchbench.ScoreQuerySuite(suite, resultsByID)
	if err != nil {
		return SuiteRun{}, fmt.Errorf("searchbenchrun: scoring failed: %w", err)
	}

	durations := observedDurations(capture.observations)
	run := searchbench.BackendRun{
		Backend:       desc.Backend,
		Mode:          mode,
		BackendImage:  desc.BackendImage,
		BackendCommit: desc.BackendCommit,
		SearchFlags:   desc.SearchFlags,
		Startup:       desc.Startup,
		QueryCount:    len(suite.Queries),
		Latency: searchbench.LatencySummary{
			P50: percentile(durations, 50),
			P95: percentile(durations, 95),
		},
		Metrics:              score.Metrics,
		MemoryHighWaterBytes: desc.MemoryHighWaterBytes,
		IndexArtifactBytes:   desc.IndexArtifactBytes,
		RebuildBehavior:      desc.RebuildBehavior,
	}
	return SuiteRun{Run: run, Score: score, Observations: capture.observations}, nil
}

// modeForBackend returns the single retrieval mode each backend serves. It
// mirrors searchbench.compatibleBackendMode so assembled runs never fail
// backend/mode validation. Unknown backends return the empty mode.
func modeForBackend(backend searchbench.Backend) searchbench.Mode {
	switch backend {
	case searchbench.BackendPostgresContentSearch, searchbench.BackendNornicDBBM25:
		return searchbench.ModeKeyword
	case searchbench.BackendNornicDBVector:
		return searchbench.ModeSemantic
	case searchbench.BackendNornicDBHybrid:
		return searchbench.ModeHybrid
	default:
		return ""
	}
}

// observedDurations extracts the measured duration of every retrieval attempt,
// including timed-out and failed attempts, so latency reflects real wall time.
func observedDurations(observations []searchretrieval.Observation) []time.Duration {
	durations := make([]time.Duration, 0, len(observations))
	for _, observation := range observations {
		durations = append(durations, observation.Duration)
	}
	return durations
}

// percentile returns the nearest-rank percentile of durations for p in [0,100].
// It returns zero for an empty input.
func percentile(durations []time.Duration, p int) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), durations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	switch {
	case p <= 0:
		return sorted[0]
	case p >= 100:
		return sorted[len(sorted)-1]
	}
	rank := int(math.Ceil(float64(p) / 100 * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}
