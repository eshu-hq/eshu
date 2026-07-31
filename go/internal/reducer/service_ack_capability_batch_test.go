// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type ackFenceRejectingBatchSink struct {
	mu         sync.Mutex
	ackCalls   int
	failCalls  int
	rejectWith error
}

func (s *ackFenceRejectingBatchSink) Ack(
	context.Context,
	Intent,
	Result,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ackCalls++
	return s.rejectWith
}

func (s *ackFenceRejectingBatchSink) AckBatch(
	_ context.Context,
	intents []Intent,
	_ []Result,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ackCalls += len(intents)
	return s.rejectWith
}

func (s *ackFenceRejectingBatchSink) Fail(
	context.Context,
	Intent,
	error,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failCalls++
	return nil
}

func TestServiceBatchAckFenceFailureDoesNotFailSucceededWork(
	t *testing.T,
) {
	t.Parallel()

	intents := makeTestIntents(3)
	for index := range intents {
		intents[index].Domain = DomainContainerImageIdentity
	}
	sink := &ackFenceRejectingBatchSink{
		rejectWith: errors.New(
			"fact_work_items_container_image_identity_v2_status_check",
		),
	}
	service := Service{
		PollInterval:   10 * time.Millisecond,
		WorkSource:     &fakeBatchWorkSource{intents: intents},
		Executor:       &countingExecutor{},
		WorkSink:       sink,
		Workers:        2,
		BatchClaimSize: len(intents),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := service.runMainLoop(ctx)
	if err == nil || !strings.Contains(
		err.Error(),
		"fact_work_items_container_image_identity_v2_status_check",
	) {
		t.Fatalf("runMainLoop() error = %v, want ACK fence failure", err)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.ackCalls != len(intents) {
		t.Fatalf("batch ACK calls = %d, want %d", sink.ackCalls, len(intents))
	}
	if sink.failCalls != 0 {
		t.Fatalf("batch Fail calls = %d, want 0", sink.failCalls)
	}
}
