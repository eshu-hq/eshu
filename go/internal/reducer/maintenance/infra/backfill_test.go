// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package infra

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

type stubBackfiller struct {
	calls int
	err   error
}

func (s *stubBackfiller) BackfillInfraInventory(context.Context) error {
	s.calls++
	return s.err
}

func TestBackfillRunnerRunsTheBackfillOnce(t *testing.T) {
	t.Parallel()
	backfiller := &stubBackfiller{}
	if err := (&BackfillRunner{Backfiller: backfiller}).Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if backfiller.calls != 1 {
		t.Fatalf("backfill calls = %d, want 1", backfiller.calls)
	}
}

func TestBackfillRunnerTreatsCancellationAsCleanShutdown(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	backfiller := &stubBackfiller{err: ctx.Err()}
	if err := (&BackfillRunner{Backfiller: backfiller}).Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil on a cancelled context", err)
	}
}

func TestBackfillRunnerReturnsANonCancellationFailure(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	err := (&BackfillRunner{Backfiller: &stubBackfiller{err: boom}}).Run(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("Run() error = %v, want it to wrap %v", err, boom)
	}
}

func TestBackfillRunnerRequiresABackfiller(t *testing.T) {
	t.Parallel()
	if err := (&BackfillRunner{}).Run(context.Background()); err == nil {
		t.Fatal("Run() error = nil, want a missing-backfiller error")
	}
}

func TestBackfillRunnerLogsStartAndFinish(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	runner := &BackfillRunner{Backfiller: &stubBackfiller{}, Logger: logger}
	if err := runner.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	out := buf.String()
	for _, want := range []string{
		"infra inventory backfill runner started",
		"infra inventory backfill runner finished",
		"infra_inventory.backfill.runner",
		"duration_seconds",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("log output missing %q:\n%s", want, out)
		}
	}
}
