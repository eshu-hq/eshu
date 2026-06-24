// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// retryableCommitError is a test error that opts into bounded retry via the
// collector RetryableError convention.
type retryableCommitError struct{ msg string }

func (e retryableCommitError) Error() string   { return e.msg }
func (e retryableCommitError) Retryable() bool { return true }

// recordingDeadLetterSink captures dead-letter records so tests can assert a
// retryable commit failure was quarantined for durable replay.
type recordingDeadLetterSink struct {
	records []GenerationDeadLetter
	err     error
}

func (s *recordingDeadLetterSink) RecordGenerationDeadLetter(_ context.Context, record GenerationDeadLetter) error {
	s.records = append(s.records, record)
	return s.err
}

// TestServiceRunRetriesInsteadOfTearingDownOnRetryableCommitError proves L1:
// a retryable commit failure is dead-lettered for durable replay and the Run
// loop continues (does not return), so a transient fault in one service cannot
// tear down the ingester. After the first retryable failure the source is
// exhausted, so Run exits cleanly via context cancellation.
func TestServiceRunRetriesInsteadOfTearingDownOnRetryableCommitError(t *testing.T) {
	t.Parallel()

	scopeValue, generationValue, envelopes := testCollectedGeneration()
	deadLetters := &recordingDeadLetterSink{}

	commitCalls := 0
	ctx, cancel := context.WithCancel(context.Background())
	service := Service{
		Source: &stubSource{
			collected: []CollectedGeneration{
				FactsFromSlice(scopeValue, generationValue, envelopes),
			},
		},
		Committer: &stubCommitter{
			commit: func(
				_ context.Context,
				_ scope.IngestionScope,
				_ scope.ScopeGeneration,
				factStream <-chan facts.Envelope,
			) error {
				for range factStream {
				}
				commitCalls++
				// Cancel after the first retryable failure so the bounded
				// continue loop exits cleanly instead of spinning forever.
				cancel()
				return retryableCommitError{msg: "transient commit failure"}
			},
		},
		DeadLetters:  deadLetters,
		PollInterval: time.Millisecond,
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil (retryable commit must not tear down)", err)
	}
	if commitCalls != 1 {
		t.Fatalf("commit call count = %d, want 1", commitCalls)
	}
	if len(deadLetters.records) != 1 {
		t.Fatalf("dead-letter record count = %d, want 1", len(deadLetters.records))
	}
}

// TestServiceRunStillPropagatesNonRetryableCommitError proves the fatal path is
// unchanged: a commit error that does not opt into retry still tears down the
// service so genuine faults are not hidden.
func TestServiceRunStillPropagatesNonRetryableCommitError(t *testing.T) {
	t.Parallel()

	scopeValue, generationValue, envelopes := testCollectedGeneration()
	wantErr := errors.New("fatal commit failure")

	service := Service{
		Source: &stubSource{
			collected: []CollectedGeneration{
				FactsFromSlice(scopeValue, generationValue, envelopes),
			},
		},
		Committer: &stubCommitter{
			commit: func(
				_ context.Context,
				_ scope.IngestionScope,
				_ scope.ScopeGeneration,
				factStream <-chan facts.Envelope,
			) error {
				for range factStream {
				}
				return wantErr
			},
		},
		PollInterval: time.Millisecond,
	}

	if err := service.Run(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want wrapped %v", err, wantErr)
	}
}

// TestServiceRunRetryableCommitErrorIsFatalWithoutDeadLetterSink proves that
// when Service has no DeadLetters sink, a retryable commit error is NOT
// silently swallowed. Without a sink there is no durable quarantine record, so
// the generation cannot be replayed; Run must return the commit error (fatal)
// instead of continuing the loop.
func TestServiceRunRetryableCommitErrorIsFatalWithoutDeadLetterSink(t *testing.T) {
	t.Parallel()

	scopeValue, generationValue, envelopes := testCollectedGeneration()
	wantErr := retryableCommitError{msg: "transient commit failure no sink"}

	service := Service{
		Source: &stubSource{
			collected: []CollectedGeneration{
				FactsFromSlice(scopeValue, generationValue, envelopes),
			},
		},
		Committer: &stubCommitter{
			commit: func(
				_ context.Context,
				_ scope.IngestionScope,
				_ scope.ScopeGeneration,
				factStream <-chan facts.Envelope,
			) error {
				for range factStream {
				}
				return wantErr
			},
		},
		// DeadLetters intentionally nil — no durable quarantine available.
		PollInterval: time.Millisecond,
	}

	if err := service.Run(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want wrapped %v (retryable must be fatal without dead-letter sink)", err, wantErr)
	}
}

// TestIsRetryableCollectorError covers the retry classifier across the variants
// the collector commit path can produce.
func TestIsRetryableCollectorError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "plain", err: errors.New("boom"), want: false},
		{name: "explicit retryable", err: retryableCommitError{msg: "x"}, want: true},
		{
			name: "registry retryable class",
			err:  RegistryFailure{Class: RegistryFailureRetryable},
			want: true,
		},
		{
			name: "registry terminal class",
			err:  RegistryFailure{Class: RegistryFailureTerminal},
			want: false,
		},
		{
			name: "wrapped retryable",
			err:  errors.Join(errors.New("ctx"), retryableCommitError{msg: "x"}),
			want: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsRetryable(tc.err); got != tc.want {
				t.Fatalf("IsRetryable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
