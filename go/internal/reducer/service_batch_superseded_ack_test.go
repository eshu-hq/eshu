// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// supersededAckBatchSink replays the #8007 shape: the queue committed the
// surviving claim and rejected the batch for the superseded one, reporting
// the dropped-claim identities in a BatchAckSupersededError.
type supersededAckBatchSink struct {
	err       error
	ackCalls  int
	ackSizes  []int
	failCalls int
}

func (*supersededAckBatchSink) Ack(context.Context, Intent, Result) error { return nil }

func (s *supersededAckBatchSink) AckBatch(_ context.Context, intents []Intent, _ []Result) error {
	s.ackCalls++
	s.ackSizes = append(s.ackSizes, len(intents))
	return s.err
}

func (s *supersededAckBatchSink) Fail(context.Context, Intent, error) error {
	s.failCalls++
	return nil
}

// TestServiceBatchSupersededClaimAckSplitsTelemetry is the #8007 regression:
// one superseded claim and its successor share one flush batch. The committed
// successor must be logged and counted as succeeded with its result, and the
// superseded claim must get its own ack_claim_rejected record carrying the
// work item id. Mapping the batch error onto both items logs
// ack_outcome_unknown twice and drops the committed result.
func TestServiceBatchSupersededClaimAckSplitsTelemetry(t *testing.T) {
	t.Parallel()

	const intentID = "intent-8007-superseded-ack"
	staleClaim := time.Date(2026, time.October, 12, 12, 0, 0, 0, time.UTC)
	freshClaim := staleClaim.Add(900 * time.Millisecond)
	stale := Intent{
		IntentID:        intentID,
		ScopeID:         "scope-8007",
		GenerationID:    "gen-8007",
		SourceSystem:    "git",
		Domain:          DomainCodeCallMaterialization,
		Cause:           "test",
		EntityKeys:      []string{"key-8007"},
		RelatedScopeIDs: []string{"scope-8007"},
		Status:          IntentStatusClaimed,
		EnqueuedAt:      staleClaim,
		AvailableAt:     staleClaim,
		ClaimedAt:       &staleClaim,
	}
	fresh := stale
	fresh.AvailableAt = freshClaim
	fresh.ClaimedAt = &freshClaim

	instruments, reader := batchTelemetry(t)
	sink := &supersededAckBatchSink{
		err: &BatchAckSupersededError{
			Superseded:    []Intent{stale},
			KeptCommitted: true,
		},
	}
	var logs bytes.Buffer
	service := Service{
		PollInterval:   time.Millisecond,
		WorkSource:     &fakeBatchWorkSource{intents: []Intent{stale, fresh}},
		Executor:       &countingExecutor{},
		WorkSink:       sink,
		Workers:        2,
		BatchClaimSize: 2,
		Wait:           func(context.Context, time.Duration) error { return context.Canceled },
		Logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
		Instruments:    instruments,
	}

	if err := service.runMainLoop(context.Background()); err != nil {
		t.Fatalf("runMainLoop() error = %v, want nil: a superseded-claim batch must not stop the reducer", err)
	}
	if sink.ackCalls != 1 || len(sink.ackSizes) != 1 || sink.ackSizes[0] != 2 || sink.failCalls != 0 {
		t.Fatalf("AckBatch calls/sizes = %d/%v, Fail calls = %d, want one two-item ACK and no Fail", sink.ackCalls, sink.ackSizes, sink.failCalls)
	}
	counts, _ := batchMetricStatusCounts(t, reader)
	if counts["succeeded"] != 1 || counts["ack_claim_rejected"] != 1 || len(counts) != 2 {
		t.Fatalf("reducer execution statuses = %v, want succeeded:1 and ack_claim_rejected:1", counts)
	}
	logText := logs.String()
	if !strings.Contains(logText, `"msg":"reducer execution succeeded"`) {
		t.Fatalf("committed successor ACK missing its succeeded record: %s", logText)
	}
	if !strings.Contains(logText, `"status":"ack_claim_rejected"`) ||
		!strings.Contains(logText, `"intent_id":"`+intentID+`"`) {
		t.Fatalf("superseded claim missing its ack_claim_rejected record with the work item id: %s", logText)
	}
	if strings.Contains(logText, `"status":"ack_outcome_unknown"`) {
		t.Fatalf("attributed batch ACK must not log ack_outcome_unknown: %s", logText)
	}
}
