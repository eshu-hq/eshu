// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchretrieval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

// Backend is the narrow search adapter port used by the internal eval runner.
type Backend interface {
	// Search returns curated search-document candidates for one bounded request.
	Search(context.Context, Request) ([]Candidate, error)
}

// Observer records one bounded retrieval attempt.
type Observer interface {
	// ObserveRetrieval records the outcome of one retrieval request.
	ObserveRetrieval(context.Context, Observation)
}

// ErrorClass identifies where a retrieval attempt failed.
type ErrorClass string

const (
	// ErrorClassValidation means the request failed bounded-contract validation.
	ErrorClassValidation ErrorClass = "validation"
	// ErrorClassBackend means the backend adapter returned a non-timeout error.
	ErrorClassBackend ErrorClass = "backend"
	// ErrorClassTimeout means the request timeout expired before retrieval ended.
	ErrorClassTimeout ErrorClass = "timeout"
	// ErrorClassCanceled means the parent context was canceled before completion.
	ErrorClassCanceled ErrorClass = "canceled"
	// ErrorClassNormalization means backend candidates failed response normalization.
	ErrorClassNormalization ErrorClass = "normalization"
)

// Observation is the operator-facing summary of one internal retrieval attempt.
type Observation struct {
	QueryID                   string                        `json:"query_id,omitempty"`
	Anchor                    Anchor                        `json:"anchor"`
	Mode                      searchbench.Mode              `json:"mode"`
	Limit                     int                           `json:"limit"`
	Duration                  time.Duration                 `json:"duration_ns"`
	CandidateCount            int                           `json:"candidate_count"`
	ResultCount               int                           `json:"result_count"`
	Truncated                 bool                          `json:"truncated"`
	TimedOut                  bool                          `json:"timed_out"`
	FailureClasses            []searchbench.FailureClass    `json:"failure_classes,omitempty"`
	CandidateTruthLevelCounts map[searchdocs.TruthLevel]int `json:"candidate_truth_level_counts,omitempty"`
	ErrorClass                ErrorClass                    `json:"error_class,omitempty"`
}

// Runner executes one bounded internal retrieval request through a backend port.
type Runner struct {
	Backend  Backend
	Observer Observer
}

// Retrieve validates, runs, normalizes, and observes one retrieval request.
func (runner Runner) Retrieve(ctx context.Context, req Request) (Response, error) {
	start := time.Now()
	req = normalizedRequest(req)
	observation := Observation{
		QueryID: req.QueryID,
		Anchor:  req.Scope.Anchor(),
		Mode:    req.Mode,
		Limit:   req.Limit,
	}
	observe := func(response Response, candidates []Candidate, err error) (Response, error) {
		observation.Duration = time.Since(start)
		observation.CandidateCount = len(candidates)
		observation.CandidateTruthLevelCounts = candidateTruthLevelCounts(candidates)
		observation.FailureClasses = appendCandidateFailureClasses(observation.FailureClasses, candidates)
		if err == nil {
			observation.ResultCount = len(response.Results)
			observation.Truncated = response.Truncated
			if response.Truncated {
				observation.FailureClasses = appendFailureClass(
					observation.FailureClasses,
					searchbench.FailureClassTruncation,
				)
			}
		}
		runner.observe(ctx, observation)
		return response, err
	}

	if err := ValidateRequest(req); err != nil {
		observation.ErrorClass = ErrorClassValidation
		return observe(Response{}, nil, err)
	}
	if runner.Backend == nil {
		observation.ErrorClass = ErrorClassBackend
		return observe(Response{}, nil, errors.New("search retrieval backend is required"))
	}

	searchCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	candidates, err := runner.Backend.Search(searchCtx, req)
	if err != nil {
		observation.ErrorClass = classifyRetrievalError(searchCtx, err)
		if observation.ErrorClass == ErrorClassTimeout {
			observation.TimedOut = true
			observation.FailureClasses = appendFailureClass(
				observation.FailureClasses,
				searchbench.FailureClassTimeout,
			)
		}
		return observe(Response{}, candidates, fmt.Errorf("search retrieval backend: %w", err))
	}

	response, err := BuildResponse(req, candidates)
	if err != nil {
		observation.ErrorClass = ErrorClassNormalization
		return observe(Response{}, candidates, err)
	}
	return observe(response, candidates, nil)
}

func (runner Runner) observe(ctx context.Context, observation Observation) {
	if runner.Observer == nil {
		return
	}
	runner.Observer.ObserveRetrieval(context.WithoutCancel(ctx), cloneObservation(observation))
}

func normalizedRequest(req Request) Request {
	req.QueryID = strings.TrimSpace(req.QueryID)
	req.Query = strings.TrimSpace(req.Query)
	req.Scope.ServiceID = strings.TrimSpace(req.Scope.ServiceID)
	req.Scope.WorkloadID = strings.TrimSpace(req.Scope.WorkloadID)
	req.Scope.RepoID = strings.TrimSpace(req.Scope.RepoID)
	req.Scope.Environment = strings.TrimSpace(req.Scope.Environment)
	return req
}

func classifyRetrievalError(ctx context.Context, err error) ErrorClass {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrorClassTimeout
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return ErrorClassCanceled
	}
	return ErrorClassBackend
}

func candidateTruthLevelCounts(candidates []Candidate) map[searchdocs.TruthLevel]int {
	if len(candidates) == 0 {
		return nil
	}
	counts := make(map[searchdocs.TruthLevel]int)
	for _, candidate := range candidates {
		counts[candidate.Document.TruthScope.Level]++
	}
	return counts
}

func appendCandidateFailureClasses(
	classes []searchbench.FailureClass,
	candidates []Candidate,
) []searchbench.FailureClass {
	for _, candidate := range candidates {
		for _, class := range candidate.Failures {
			classes = appendFailureClass(classes, class)
		}
	}
	return classes
}

func appendFailureClass(
	classes []searchbench.FailureClass,
	class searchbench.FailureClass,
) []searchbench.FailureClass {
	for _, existing := range classes {
		if existing == class {
			return classes
		}
	}
	return append(classes, class)
}

func cloneObservation(observation Observation) Observation {
	observation.FailureClasses = append([]searchbench.FailureClass(nil), observation.FailureClasses...)
	if observation.CandidateTruthLevelCounts != nil {
		counts := make(map[searchdocs.TruthLevel]int, len(observation.CandidateTruthLevelCounts))
		for level, count := range observation.CandidateTruthLevelCounts {
			counts[level] = count
		}
		observation.CandidateTruthLevelCounts = counts
	}
	return observation
}
