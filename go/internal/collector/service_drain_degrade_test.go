// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

// Drain-boundary backstop proofs (arbiter ruling, combination A+C): a
// budget-sentinel error escaping the deferred-maintenance pass must skip
// this drain pass without killing the pod, while any other drain-hook error
// stays fatal.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// TestServiceRunSkipsDrainOnBudgetSentinel is the rung-1c RED test: when
// AfterBatchDrained returns a wrapped budget-sentinel error, Run must swallow
// it as skip-this-pass and keep polling instead of returning "after
// collector batch drained".
func TestServiceRunSkipsDrainOnBudgetSentinel(t *testing.T) {
	t.Parallel()

	scopeValue, generationValue, envelopes := testCollectedGeneration()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sentinel := fmt.Errorf(
		"load anchor-scoped facts for deferred relationship backfill: %w",
		errors.Join(
			errors.New("load argocd generator config facts for deferred relationship backfill"),
			backfill.ErrRelationshipFactLoadBudgetExceeded,
		),
	)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	// One committed batch, then idle polls: the AfterEmptyBatchDrained escape
	// drains on every idle poll, so successive drains observe the skip counter
	// incrementing and then resetting on the clean drain.
	hookCalls := 0
	empties := 0
	service := Service{
		Source: &stubSource{
			collected: []CollectedGeneration{
				FactsFromSlice(scopeValue, generationValue, envelopes),
			},
			empty: func() {
				empties++
				if empties >= 4 {
					cancel()
				}
			},
		},
		Committer:              &stubCommitter{},
		PollInterval:           time.Millisecond,
		AfterEmptyBatchDrained: true,
		AfterBatchDrained: func(context.Context, bool) error {
			hookCalls++
			if hookCalls <= 2 {
				return sentinel
			}
			return nil
		},
		Logger: logger,
	}

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil (sentinel drain error skips the pass)", err)
	}
	if hookCalls < 3 {
		t.Fatalf("AfterBatchDrained() calls = %d, want >= 3 (polling continued after the sentinel skips)", hookCalls)
	}
	if !strings.Contains(logBuf.String(), "consecutive_skips=1") {
		t.Fatalf("degraded drain line missing consecutive_skips=1\noutput:\n%s", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "consecutive_skips=2") {
		t.Fatalf("degraded drain line missing consecutive_skips=2 (successive skips must count up)\noutput:\n%s", logBuf.String())
	}
}

// TestServiceRunKeepsNonSentinelDrainErrorFatal is seeded-violation A for the
// drain boundary: a generic hook error must still tear down the collector.
func TestServiceRunKeepsNonSentinelDrainErrorFatal(t *testing.T) {
	t.Parallel()

	scopeValue, generationValue, envelopes := testCollectedGeneration()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boom := errors.New("connection refused")
	service := Service{
		Source: &stubSource{
			collected: []CollectedGeneration{
				FactsFromSlice(scopeValue, generationValue, envelopes),
			},
			empty: func() {
				cancel()
			},
		},
		Committer:    &stubCommitter{},
		PollInterval: time.Millisecond,
		AfterBatchDrained: func(context.Context, bool) error {
			return boom
		},
	}

	err := service.Run(ctx)
	if err == nil {
		t.Fatal("Run() error = nil, want the fatal drain error")
	}
	if !strings.Contains(err.Error(), "after collector batch drained") {
		t.Fatalf("Run() error = %v, want the fatal drain wrapper", err)
	}
	if errors.Is(err, backfill.ErrRelationshipFactLoadBudgetExceeded) {
		t.Fatalf("Run() error = %v, must not map a generic failure to the budget sentinel", err)
	}
}
