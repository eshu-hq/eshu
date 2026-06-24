// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestServiceRunRecordsGenerationDeadLetterWhenCommitFails(t *testing.T) {
	t.Parallel()

	scopeValue, generationValue, envelopes := testCollectedGeneration()
	wantErr := errors.New("commit failed")
	sink := &stubGenerationDeadLetterSink{}

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
		DeadLetters:  sink,
		PollInterval: time.Millisecond,
	}

	err := service.Run(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want wrapped %v", err, wantErr)
	}
	if got, want := len(sink.records), 1; got != want {
		t.Fatalf("dead-letter records = %d, want %d", got, want)
	}
	record := sink.records[0]
	if record.Scope.ScopeID != scopeValue.ScopeID {
		t.Fatalf("dead-letter scope_id = %q, want %q", record.Scope.ScopeID, scopeValue.ScopeID)
	}
	if record.Generation.GenerationID != generationValue.GenerationID {
		t.Fatalf("dead-letter generation_id = %q, want %q", record.Generation.GenerationID, generationValue.GenerationID)
	}
	if record.FailureClass != "commit_failure" {
		t.Fatalf("dead-letter failure_class = %q, want commit_failure", record.FailureClass)
	}
	if !strings.Contains(record.FailureMessage, wantErr.Error()) {
		t.Fatalf("dead-letter failure_message = %q, want wrapped commit error", record.FailureMessage)
	}
	if got := record.PayloadReference["partition_key"]; got != scopeValue.PartitionKey {
		t.Fatalf("dead-letter payload partition_key = %q, want %q", got, scopeValue.PartitionKey)
	}
}

func TestServiceRunCompletesGenerationDeadLetterReplayAfterCommitSucceeds(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scopeValue, generationValue, envelopes := testCollectedGeneration()
	sink := &stubGenerationDeadLetterSink{}

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
				cancel()
				return nil
			},
		},
		DeadLetters:  sink,
		PollInterval: time.Millisecond,
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(sink.completions), 1; got != want {
		t.Fatalf("replay completions = %d, want %d", got, want)
	}
	completion := sink.completions[0]
	if completion.Scope.ScopeID != scopeValue.ScopeID {
		t.Fatalf("completion scope_id = %q, want %q", completion.Scope.ScopeID, scopeValue.ScopeID)
	}
	if completion.Generation.GenerationID != generationValue.GenerationID {
		t.Fatalf("completion generation_id = %q, want %q", completion.Generation.GenerationID, generationValue.GenerationID)
	}
	if got := sink.completionContextErrs[0]; got != nil {
		t.Fatalf("completion context error = %v, want nil", got)
	}
}

type stubGenerationDeadLetterSink struct {
	records               []GenerationDeadLetter
	completions           []GenerationDeadLetterReplayCompletion
	completionContextErrs []error
	err                   error
	completeErr           error
}

func (s *stubGenerationDeadLetterSink) RecordGenerationDeadLetter(
	_ context.Context,
	record GenerationDeadLetter,
) error {
	s.records = append(s.records, record)
	return s.err
}

func (s *stubGenerationDeadLetterSink) CompleteGenerationDeadLetterReplay(
	ctx context.Context,
	completion GenerationDeadLetterReplayCompletion,
) error {
	s.completions = append(s.completions, completion)
	s.completionContextErrs = append(s.completionContextErrs, ctx.Err())
	return s.completeErr
}
