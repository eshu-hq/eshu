// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchretrieval

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchbench"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
)

func TestRunnerRejectsInvalidRequestBeforeBackendUse(t *testing.T) {
	t.Parallel()

	backend := &fakeRetrievalBackend{}
	observer := &recordingRetrievalObserver{}
	_, err := Runner{Backend: backend, Observer: observer}.Retrieve(context.Background(), Request{
		Query:   "   ",
		Mode:    searchbench.ModeHybrid,
		Limit:   10,
		Timeout: time.Second,
	})

	if err == nil {
		t.Fatal("Runner.Retrieve() error = nil, want validation error")
	}
	if backend.calls != 0 {
		t.Fatalf("backend calls = %d, want 0", backend.calls)
	}
	obs := observer.only(t)
	if got, want := obs.ErrorClass, ErrorClassValidation; got != want {
		t.Fatalf("obs.ErrorClass = %q, want %q", got, want)
	}
}

func TestRunnerRecordsSuccessObservation(t *testing.T) {
	t.Parallel()

	req := validRequestFixture()
	req.Limit = 2
	canonical := documentFixture("searchdoc:canonical", "service:checkout")
	canonical.TruthScope.Level = searchdocs.TruthLevel("canonical")
	backend := &fakeRetrievalBackend{candidates: []Candidate{
		{
			Document: documentFixture("searchdoc:derived-a", "workload:checkout-api"),
			Score:    0.8,
			Failures: []searchbench.FailureClass{searchbench.FailureClassLazyWarm},
		},
		{Document: canonical, Score: 0.7},
		{Document: documentFixture("searchdoc:derived-b", "repo-checkout:README.md"), Score: 0.6},
	}}
	observer := &recordingRetrievalObserver{}

	response, err := Runner{Backend: backend, Observer: observer}.Retrieve(context.Background(), req)
	if err != nil {
		t.Fatalf("Runner.Retrieve() error = %v, want nil", err)
	}
	if got, want := len(response.Results), 2; got != want {
		t.Fatalf("len(response.Results) = %d, want %d", got, want)
	}

	obs := observer.only(t)
	if got, want := obs.QueryID, req.QueryID; got != want {
		t.Fatalf("obs.QueryID = %q, want %q", got, want)
	}
	if got, want := obs.Anchor.Kind, ScopeKindRepo; got != want {
		t.Fatalf("obs.Anchor.Kind = %q, want %q", got, want)
	}
	if got, want := obs.Mode, searchbench.ModeHybrid; got != want {
		t.Fatalf("obs.Mode = %q, want %q", got, want)
	}
	if got, want := obs.CandidateCount, 3; got != want {
		t.Fatalf("obs.CandidateCount = %d, want %d", got, want)
	}
	if got, want := obs.ResultCount, 2; got != want {
		t.Fatalf("obs.ResultCount = %d, want %d", got, want)
	}
	if !obs.Truncated {
		t.Fatal("obs.Truncated = false, want true")
	}
	if obs.TimedOut {
		t.Fatal("obs.TimedOut = true, want false")
	}
	if obs.ErrorClass != "" {
		t.Fatalf("obs.ErrorClass = %q, want empty", obs.ErrorClass)
	}
	if got, want := obs.CandidateTruthLevelCounts[searchdocs.TruthLevelDerived], 2; got != want {
		t.Fatalf("derived candidate truth count = %d, want %d", got, want)
	}
	if got, want := obs.CandidateTruthLevelCounts[searchdocs.TruthLevel("canonical")], 1; got != want {
		t.Fatalf("canonical candidate truth count = %d, want %d", got, want)
	}
	if !hasFailureClass(obs.FailureClasses, searchbench.FailureClassTruncation) {
		t.Fatalf("obs.FailureClasses = %#v, want truncation", obs.FailureClasses)
	}
	if !hasFailureClass(obs.FailureClasses, searchbench.FailureClassLazyWarm) {
		t.Fatalf("obs.FailureClasses = %#v, want candidate lazy-warm failure", obs.FailureClasses)
	}
	if obs.Duration <= 0 {
		t.Fatalf("obs.Duration = %s, want positive duration", obs.Duration)
	}
}

func TestRunnerRecordsBackendError(t *testing.T) {
	t.Parallel()

	backendErr := errors.New("search backend unavailable")
	observer := &recordingRetrievalObserver{}
	_, err := Runner{
		Backend:  &fakeRetrievalBackend{err: backendErr},
		Observer: observer,
	}.Retrieve(context.Background(), validRequestFixture())

	if !errors.Is(err, backendErr) {
		t.Fatalf("Runner.Retrieve() error = %v, want backend error", err)
	}
	obs := observer.only(t)
	if got, want := obs.ErrorClass, ErrorClassBackend; got != want {
		t.Fatalf("obs.ErrorClass = %q, want %q", got, want)
	}
	if got, want := obs.ResultCount, 0; got != want {
		t.Fatalf("obs.ResultCount = %d, want %d", got, want)
	}
	if obs.CandidateTruthLevelCounts != nil {
		t.Fatalf("obs.CandidateTruthLevelCounts = %#v, want nil for no candidates", obs.CandidateTruthLevelCounts)
	}
	if obs.TimedOut {
		t.Fatal("obs.TimedOut = true, want false")
	}
}

func TestRunnerRecordsNormalizationError(t *testing.T) {
	t.Parallel()

	observer := &recordingRetrievalObserver{}
	_, err := Runner{
		Backend: &fakeRetrievalBackend{candidates: []Candidate{
			{Document: documentFixture("searchdoc:nan", "service:checkout"), Score: math.NaN()},
		}},
		Observer: observer,
	}.Retrieve(context.Background(), validRequestFixture())

	if err == nil {
		t.Fatal("Runner.Retrieve() error = nil, want normalization error")
	}
	obs := observer.only(t)
	if got, want := obs.ErrorClass, ErrorClassNormalization; got != want {
		t.Fatalf("obs.ErrorClass = %q, want %q", got, want)
	}
	if got, want := obs.CandidateCount, 1; got != want {
		t.Fatalf("obs.CandidateCount = %d, want %d", got, want)
	}
}

func TestRunnerRecordsTimeout(t *testing.T) {
	t.Parallel()

	req := validRequestFixture()
	req.Timeout = 5 * time.Millisecond
	observer := &recordingRetrievalObserver{}
	_, err := Runner{
		Backend:  &fakeRetrievalBackend{waitForCancel: true},
		Observer: observer,
	}.Retrieve(context.Background(), req)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Runner.Retrieve() error = %v, want deadline exceeded", err)
	}
	obs := observer.only(t)
	if got, want := obs.ErrorClass, ErrorClassTimeout; got != want {
		t.Fatalf("obs.ErrorClass = %q, want %q", got, want)
	}
	if !obs.TimedOut {
		t.Fatal("obs.TimedOut = false, want true")
	}
	if !hasFailureClass(obs.FailureClasses, searchbench.FailureClassTimeout) {
		t.Fatalf("obs.FailureClasses = %#v, want timeout", obs.FailureClasses)
	}
}

func TestRunnerRecordsParentCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	observer := &recordingRetrievalObserver{}
	_, err := Runner{
		Backend:  &fakeRetrievalBackend{waitForCancel: true},
		Observer: observer,
	}.Retrieve(ctx, validRequestFixture())

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Runner.Retrieve() error = %v, want context canceled", err)
	}
	obs := observer.only(t)
	if got, want := obs.ErrorClass, ErrorClassCanceled; got != want {
		t.Fatalf("obs.ErrorClass = %q, want %q", got, want)
	}
	if obs.TimedOut {
		t.Fatal("obs.TimedOut = true, want false")
	}
}

func TestRunnerObservesWithNonCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	observer := &contextRecordingObserver{}

	_, err := Runner{
		Backend:  &fakeRetrievalBackend{waitForCancel: true},
		Observer: observer,
	}.Retrieve(ctx, validRequestFixture())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Runner.Retrieve() error = %v, want context canceled", err)
	}
	if observer.err != nil {
		t.Fatalf("observer context err = %v, want nil", observer.err)
	}
}

type fakeRetrievalBackend struct {
	candidates    []Candidate
	err           error
	waitForCancel bool
	calls         int
}

func (backend *fakeRetrievalBackend) Search(ctx context.Context, req Request) ([]Candidate, error) {
	backend.calls++
	if backend.waitForCancel {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if backend.err != nil {
		return nil, backend.err
	}
	return append([]Candidate(nil), backend.candidates...), nil
}

type recordingRetrievalObserver struct {
	observations []Observation
}

func (observer *recordingRetrievalObserver) ObserveRetrieval(ctx context.Context, observation Observation) {
	observer.observations = append(observer.observations, observation)
}

type contextRecordingObserver struct {
	err error
}

func (observer *contextRecordingObserver) ObserveRetrieval(ctx context.Context, observation Observation) {
	observer.err = ctx.Err()
}

func (observer *recordingRetrievalObserver) only(t *testing.T) Observation {
	t.Helper()
	if got, want := len(observer.observations), 1; got != want {
		t.Fatalf("observations = %d, want %d", got, want)
	}
	return observer.observations[0]
}

func hasFailureClass(classes []searchbench.FailureClass, want searchbench.FailureClass) bool {
	for _, class := range classes {
		if strings.TrimSpace(string(class)) == string(want) {
			return true
		}
	}
	return false
}
