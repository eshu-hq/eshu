// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
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
