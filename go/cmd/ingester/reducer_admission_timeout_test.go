// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	queuestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/queue"
)

// TestReducerAdmissionTimeoutReleasesWaitWithoutInnerEnqueue proves the #10485
// contract: with reducer depth held above the high-water mark, Enqueue stops
// waiting once the claim-scoped admission timeout elapses and returns a
// timeout error without ever calling the inner writer. The projector turns
// that error into failWork -> Fail (attempt++), so another worker can pick the
// item up instead of the claim parking forever under renewing heartbeats.
func TestReducerAdmissionTimeoutReleasesWaitWithoutInnerEnqueue(t *testing.T) {
	t.Parallel()

	reader := &fakeReducerAdmissionDepthReader{
		depths: []map[string]map[string]int64{{"reducer": {"pending": 10}}},
	}
	writer := &recordingReducerIntentWriter{}
	admission := reducerAdmissionWriter{
		inner:       writer,
		depthReader: reader,
		config: reducerAdmissionConfig{
			HighWaterMark: 10,
			PollInterval:  time.Millisecond,
			MaxWait:       30 * time.Millisecond,
		},
		sleep: func(context.Context, time.Duration) error {
			return nil
		},
	}

	_, err := admission.Enqueue(context.Background(), []runtime.ReducerIntent{
		{Domain: reducer.DomainWorkloadIdentity},
	})
	if !errors.Is(err, ErrReducerAdmissionTimeout) {
		t.Fatalf("Enqueue() error = %v, want %v", err, ErrReducerAdmissionTimeout)
	}
	// Retryable is load-bearing: ProjectorQueue.Fail only releases the claim
	// (attempt++) for errors where failure.IsRetryable holds; anything else
	// goes terminal and the census stops turning over.
	if !failure.IsRetryable(err) {
		t.Fatalf("Enqueue() error IsRetryable = false, want true so Fail retries the item")
	}
	// Pin the production chain: runtime/projection.go wraps the writer error
	// with "enqueue reducer intents: %w" before failWork, and IsRetryable
	// must survive that wrap or the item goes terminal instead of retrying.
	wrapped := fmt.Errorf("enqueue reducer intents: %w", err)
	if !failure.IsRetryable(wrapped) {
		t.Fatalf("wrapped Enqueue() error IsRetryable = false, want true through the production %%w wrap")
	}
	if writer.calls != 0 {
		t.Fatalf("inner enqueue count = %d, want 0 (no completion, no graph write)", writer.calls)
	}
}

// TestReducerAdmissionTimeoutDeadLettersAsAdmissionTimeout proves the #10502
// contract: a reducer admission wait timeout that exhausts the projector
// attempt budget must dead-letter under its own admission_timeout class, not
// the projection_bug default. The cause is backpressure (depth held above the
// mark past the claim-scoped bound), never broken projection code, so the
// durable row and the triage detail must both name it. The test drives the
// production path end to end: the real Enqueue timeout error, the production
// "enqueue reducer intents: %w" wrap from runtime/projection.go, and the live
// dead-letter triage the projector queue Fail path runs.
func TestReducerAdmissionTimeoutDeadLettersAsAdmissionTimeout(t *testing.T) {
	t.Parallel()

	reader := &fakeReducerAdmissionDepthReader{
		depths: []map[string]map[string]int64{{"reducer": {"pending": 10}}},
	}
	admission := reducerAdmissionWriter{
		inner:       &recordingReducerIntentWriter{},
		depthReader: reader,
		config: reducerAdmissionConfig{
			HighWaterMark: 10,
			PollInterval:  time.Millisecond,
			MaxWait:       30 * time.Millisecond,
		},
		sleep: func(context.Context, time.Duration) error {
			return nil
		},
	}

	_, err := admission.Enqueue(context.Background(), []runtime.ReducerIntent{
		{Domain: reducer.DomainWorkloadIdentity},
	})
	if !errors.Is(err, ErrReducerAdmissionTimeout) {
		t.Fatalf("Enqueue() error = %v, want %v", err, ErrReducerAdmissionTimeout)
	}
	// Production wrap chain: runtime/projection.go wraps the writer error
	// before failWork, and ProjectorQueue.Fail derives retryability from the
	// wrapped error.
	wrapped := fmt.Errorf("enqueue reducer intents: %w", err)
	retryable := failure.IsRetryable(wrapped)
	if !retryable {
		t.Fatalf("wrapped Enqueue() error IsRetryable = false, want true so the item retries to exhaustion")
	}

	class, _, details := queuestore.DeadLetterTriageMetadata(wrapped, "project_work_item", retryable)
	if class != "admission_timeout" {
		t.Fatalf("dead-letter failure_class = %q, want admission_timeout (backpressure, not a projection bug)", class)
	}
	if !strings.Contains(details, "class=admission_timeout") {
		t.Fatalf("dead-letter details = %q, want the triage class=admission_timeout", details)
	}
	if strings.Contains(details, "projection_bug") {
		t.Fatalf("dead-letter details = %q, must not label a backpressure death projection_bug", details)
	}

	// The live retryable-exhausted bucket still describes the death: the item
	// retried to the budget and gave up. Only the misleading default class
	// inside the detail changes.
	record := failure.TriageFailure(wrapped, "project_work_item", true, true)
	if record.FailureClass != string(failure.TriageClassRetryExhausted) {
		t.Fatalf("triage class = %q, want %q", record.FailureClass, failure.TriageClassRetryExhausted)
	}
	if !strings.Contains(record.Details, "class=admission_timeout") {
		t.Fatalf("triage details = %q, want class=admission_timeout", record.Details)
	}
}

// TestReducerAdmissionBelowMarkIgnoresTimeout proves existing admission
// behavior is unchanged when depth is below the mark: Enqueue proceeds on the
// first decision even with a timeout configured.
func TestReducerAdmissionBelowMarkIgnoresTimeout(t *testing.T) {
	t.Parallel()

	reader := &fakeReducerAdmissionDepthReader{
		depths: []map[string]map[string]int64{{"reducer": {"pending": 4}}},
	}
	writer := &recordingReducerIntentWriter{}
	admission := reducerAdmissionWriter{
		inner:       writer,
		depthReader: reader,
		config: reducerAdmissionConfig{
			HighWaterMark: 10,
			PollInterval:  time.Millisecond,
			MaxWait:       30 * time.Millisecond,
		},
		sleep: func(context.Context, time.Duration) error {
			t.Fatal("sleep should not run when depth is below the mark")
			return nil
		},
	}

	result, err := admission.Enqueue(context.Background(), []runtime.ReducerIntent{
		{Domain: reducer.DomainWorkloadIdentity},
	})
	if err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if result.Count != 1 {
		t.Fatalf("Enqueue() count = %d, want 1", result.Count)
	}
	if writer.calls != 1 {
		t.Fatalf("inner enqueue count = %d, want 1", writer.calls)
	}
}

// TestReducerAdmissionZeroMaxWaitDisablesTimeout pins the escape hatch: an
// explicit zero MaxWait keeps the legacy unbounded wait, so direct struct
// construction without the loader is unaffected.
func TestReducerAdmissionZeroMaxWaitDisablesTimeout(t *testing.T) {
	t.Parallel()

	reader := &fakeReducerAdmissionDepthReader{
		depths: []map[string]map[string]int64{
			{"reducer": {"pending": 10}},
			{"reducer": {"pending": 4}},
		},
	}
	writer := &recordingReducerIntentWriter{}
	sleeps := 0
	admission := reducerAdmissionWriter{
		inner:       writer,
		depthReader: reader,
		config: reducerAdmissionConfig{
			HighWaterMark: 10,
			PollInterval:  time.Millisecond,
		},
		sleep: func(context.Context, time.Duration) error {
			sleeps++
			return nil
		},
	}

	if _, err := admission.Enqueue(context.Background(), []runtime.ReducerIntent{
		{Domain: reducer.DomainWorkloadIdentity},
	}); err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", err)
	}
	if writer.calls != 1 {
		t.Fatalf("inner enqueue count = %d, want 1", writer.calls)
	}
	if sleeps != 1 {
		t.Fatalf("sleep count = %d, want 1", sleeps)
	}
}

func TestLoadReducerAdmissionConfigDefaultsMaxWait(t *testing.T) {
	t.Parallel()

	config, err := loadReducerAdmissionConfig(mapGetenv(nil))
	if err != nil {
		t.Fatalf("loadReducerAdmissionConfig() error = %v, want nil", err)
	}
	if got, want := config.MaxWait, defaultReducerAdmissionMaxWait; got != want {
		t.Fatalf("MaxWait = %s, want default %s", got, want)
	}
}

func TestLoadReducerAdmissionConfigParsesMaxWait(t *testing.T) {
	t.Parallel()

	config, err := loadReducerAdmissionConfig(mapGetenv(map[string]string{
		"ESHU_REDUCER_ADMISSION_MAX_WAIT": "90s",
	}))
	if err != nil {
		t.Fatalf("loadReducerAdmissionConfig() error = %v, want nil", err)
	}
	if got, want := config.MaxWait, 90*time.Second; got != want {
		t.Fatalf("MaxWait = %s, want %s", got, want)
	}
}

func TestLoadReducerAdmissionConfigZeroMaxWaitDisablesTimeout(t *testing.T) {
	t.Parallel()

	config, err := loadReducerAdmissionConfig(mapGetenv(map[string]string{
		"ESHU_REDUCER_ADMISSION_MAX_WAIT": "0s",
	}))
	if err != nil {
		t.Fatalf("loadReducerAdmissionConfig() error = %v, want nil", err)
	}
	if got := config.MaxWait; got != 0 {
		t.Fatalf("MaxWait = %s, want explicit disabled value 0", got)
	}
}

func TestLoadReducerAdmissionConfigRejectsNegativeMaxWait(t *testing.T) {
	t.Parallel()

	_, err := loadReducerAdmissionConfig(mapGetenv(map[string]string{
		"ESHU_REDUCER_ADMISSION_MAX_WAIT": "-1s",
	}))
	if err == nil {
		t.Fatal("loadReducerAdmissionConfig() error = nil, want invalid config error")
	}
}
