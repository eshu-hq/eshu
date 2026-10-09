// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package liveness

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeGenerationLivenessRecoverer struct {
	mu       sync.Mutex
	results  []Result
	errs     []error
	calls    int
	policies []Policy
}

func (f *fakeGenerationLivenessRecoverer) RecoverWedgedGenerations(
	_ context.Context,
	policy Policy,
	_ time.Time,
) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	idx := f.calls
	f.calls++
	f.policies = append(f.policies, policy)
	if idx < len(f.errs) && f.errs[idx] != nil {
		return Result{}, f.errs[idx]
	}
	if idx < len(f.results) {
		return f.results[idx], nil
	}
	return Result{}, nil
}

func (f *fakeGenerationLivenessRecoverer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestGenerationLivenessRunnerRecoversThenWaits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	recoverer := &fakeGenerationLivenessRecoverer{
		results: []Result{
			{Recovered: 3, Superseded: 1},
			{Recovered: 0, Superseded: 0},
		},
	}
	waitCalls := 0
	runner := &Runner{
		Recoverer: recoverer,
		Config: Config{
			PollInterval: time.Hour,
			Policy: Policy{
				ActivationDeadline: 30 * time.Minute,
				MaxRecoverAttempts: 5,
				BatchLimit:         100,
			},
		},
		Wait: func(context.Context, time.Duration) error {
			waitCalls++
			cancel()
			return context.Canceled
		},
	}

	err := runner.Run(ctx)

	require.NoError(t, err)
	// First cycle recovers work (loops immediately), second finds nothing and waits.
	require.Equal(t, 2, recoverer.callCount())
	require.Equal(t, 1, waitCalls)
	require.Equal(t, 30*time.Minute, recoverer.policies[0].ActivationDeadline)
}

func TestGenerationLivenessRunnerRetriesAfterError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	recoverer := &fakeGenerationLivenessRecoverer{
		errs: []error{errors.New("connection refused")},
	}
	waitCalls := 0
	runner := &Runner{
		Recoverer: recoverer,
		Config: Config{
			PollInterval: time.Hour,
			Policy:       Policy{ActivationDeadline: time.Minute, BatchLimit: 10},
		},
		Wait: func(context.Context, time.Duration) error {
			waitCalls++
			cancel()
			return context.Canceled
		},
	}

	err := runner.Run(ctx)

	require.NoError(t, err)
	require.Equal(t, 1, recoverer.callCount())
	require.Equal(t, 1, waitCalls)
}

func TestGenerationLivenessRunnerValidation(t *testing.T) {
	runner := &Runner{}

	_, err := runner.RunOnce(context.Background())

	require.ErrorContains(t, err, "generation liveness recoverer is required")
}

func TestGenerationLivenessRunnerRunOnceReturnsResult(t *testing.T) {
	recoverer := &fakeGenerationLivenessRecoverer{
		results: []Result{{Recovered: 2, Superseded: 1}},
	}
	runner := &Runner{
		Recoverer: recoverer,
		Config: Config{
			Policy: Policy{ActivationDeadline: time.Minute, BatchLimit: 10},
		},
	}

	result, err := runner.RunOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 2, result.Recovered)
	require.Equal(t, 1, result.Superseded)
}
