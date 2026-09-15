// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

type staleAckBatchSource struct {
	mu     sync.Mutex
	intent *reducer.Intent
}

func (s *staleAckBatchSource) Claim(ctx context.Context) (reducer.Intent, bool, error) {
	intents, err := s.ClaimBatch(ctx, 1)
	if err != nil || len(intents) == 0 {
		return reducer.Intent{}, false, err
	}
	return intents[0], true, nil
}

func (s *staleAckBatchSource) ClaimBatch(context.Context, int) ([]reducer.Intent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.intent == nil {
		return nil, nil
	}
	intent := *s.intent
	s.intent = nil
	return []reducer.Intent{intent}, nil
}

type staleAckBatchSink struct {
	mu       sync.Mutex
	ackCalls int
}

func (s *staleAckBatchSink) Ack(context.Context, reducer.Intent, reducer.Result) error {
	return nil
}

func (s *staleAckBatchSink) AckBatch(
	context.Context,
	[]reducer.Intent,
	[]reducer.Result,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ackCalls++
	return postgres.ErrReducerClaimRejected
}

func (s *staleAckBatchSink) Fail(context.Context, reducer.Intent, error) error {
	return nil
}

type staleAckExecutor struct{}

func (staleAckExecutor) Execute(_ context.Context, intent reducer.Intent) (reducer.Result, error) {
	return reducer.Result{
		IntentID: intent.IntentID,
		Domain:   intent.Domain,
		Status:   reducer.ResultStatusSucceeded,
	}, nil
}

type staleAckSingleSink struct {
	mu        sync.Mutex
	ackCalls  int
	failCalls int
}

func (s *staleAckSingleSink) Ack(context.Context, reducer.Intent, reducer.Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ackCalls++
	return postgres.ErrReducerClaimRejected
}

func (s *staleAckSingleSink) Fail(context.Context, reducer.Intent, error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failCalls++
	return nil
}

func TestServiceSingleStaleAckDoesNotStopReducer(t *testing.T) {
	t.Parallel()

	intent := reducer.Intent{
		IntentID:     "stale-single-ack",
		ScopeID:      "scope-stale-single-ack",
		GenerationID: "generation-stale-single-ack",
		Domain:       reducer.DomainRepoDependency,
		AvailableAt:  time.Now().UTC(),
	}
	source := &staleAckBatchSource{intent: &intent}
	sink := &staleAckSingleSink{}
	var logs bytes.Buffer
	service := reducer.Service{
		PollInterval: time.Millisecond,
		WorkSource:   source,
		Executor:     staleAckExecutor{},
		WorkSink:     sink,
		Wait: func(context.Context, time.Duration) error {
			return context.Canceled
		},
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	}

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil for stale single ACK", err)
	}
	sink.mu.Lock()
	ackCalls, failCalls := sink.ackCalls, sink.failCalls
	sink.mu.Unlock()
	if ackCalls != 1 {
		t.Fatalf("Ack() calls = %d, want 1", ackCalls)
	}
	if failCalls != 0 {
		t.Fatalf("Fail() calls = %d, want 0", failCalls)
	}
	logOutput := logs.String()
	for _, want := range []string{
		`"msg":"reducer ack rejected stale claim"`,
		`"failure_class":"execution_claim_rejected"`,
		`"queue":"reducer"`,
		`"pipeline_phase":"reduction"`,
		`"intent_id":"stale-single-ack"`,
	} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("logs missing %s in %s", want, logOutput)
		}
	}
}

func TestServiceBatchStaleAckDoesNotStopReducer(t *testing.T) {
	t.Parallel()

	intent := reducer.Intent{
		IntentID:     "stale-batch-ack",
		ScopeID:      "scope-stale-batch-ack",
		GenerationID: "generation-stale-batch-ack",
		Domain:       reducer.DomainRepoDependency,
		AvailableAt:  time.Now().UTC(),
	}
	source := &staleAckBatchSource{intent: &intent}
	sink := &staleAckBatchSink{}
	var logs bytes.Buffer
	service := reducer.Service{
		PollInterval:   time.Millisecond,
		WorkSource:     source,
		Executor:       staleAckExecutor{},
		WorkSink:       sink,
		Workers:        2,
		BatchClaimSize: 1,
		Wait: func(context.Context, time.Duration) error {
			return context.Canceled
		},
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	}

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil for stale batch ACK", err)
	}
	sink.mu.Lock()
	ackCalls := sink.ackCalls
	sink.mu.Unlock()
	if ackCalls != 1 {
		t.Fatalf("AckBatch() calls = %d, want 1", ackCalls)
	}
	logOutput := logs.String()
	for _, want := range []string{
		`"msg":"reducer batch ack rejected stale claim"`,
		`"failure_class":"execution_claim_rejected"`,
		`"queue":"reducer"`,
		`"pipeline_phase":"reduction"`,
		`"batch_size":1`,
	} {
		if !strings.Contains(logOutput, want) {
			t.Fatalf("logs missing %s in %s", want, logOutput)
		}
	}
}
