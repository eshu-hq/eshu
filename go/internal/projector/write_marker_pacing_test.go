// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
)

// TestMain paces nothing: the deferral backoff sleeps real time, so the
// package suite runs with an instant sleeper and only the tests below that
// assert pacing swap the real one back in.
func TestMain(m *testing.M) {
	instant := writeMarkerSleepFunc(func(context.Context, time.Duration) error { return nil })
	writeMarkerDeferralSleep.Store(&instant)
	m.Run()
}

// withRealWriteMarkerSleep swaps the real ctx-aware sleeper back in for one
// test. Callers must not be parallel: a parallel sibling would load the real
// sleeper mid-run.
func withRealWriteMarkerSleep(t *testing.T) {
	t.Helper()
	real := writeMarkerSleepFunc(func(ctx context.Context, d time.Duration) error {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	})
	writeMarkerDeferralSleep.Store(&real)
	t.Cleanup(func() {
		instant := writeMarkerSleepFunc(func(context.Context, time.Duration) error { return nil })
		writeMarkerDeferralSleep.Store(&instant)
	})
}

// TestWriteMarkerDeferralBackoffSchedule pins the pacing schedule: 10 ms,
// doubling per consecutive deferral, capped at 200 ms.
func TestWriteMarkerDeferralBackoffSchedule(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		deferral int
		want     time.Duration
	}{
		{deferral: 1, want: 10 * time.Millisecond},
		{deferral: 2, want: 20 * time.Millisecond},
		{deferral: 3, want: 40 * time.Millisecond},
		{deferral: 4, want: 80 * time.Millisecond},
		{deferral: 5, want: 160 * time.Millisecond},
		{deferral: 6, want: 200 * time.Millisecond},
		{deferral: 150, want: 200 * time.Millisecond},
	} {
		if got := writeMarkerDeferralBackoff(tc.deferral); got != tc.want {
			t.Errorf("writeMarkerDeferralBackoff(%d) = %v, want %v", tc.deferral, got, tc.want)
		}
	}
}

// TestMarkProjectionWriteStartedPacesDeferrals is the #7907 regression: N
// consecutive immediate deferrals must wait out the backoff instead of
// burning the retry bound in milliseconds.
func TestMarkProjectionWriteStartedPacesDeferrals(t *testing.T) {
	withRealWriteMarkerSleep(t)
	deferred := fmt.Errorf("lock timeout: %w", failure.ErrWorkWriteMarkerDeferred)
	marker := &fakeWriteMarker{errs: []error{deferred, deferred, deferred}}
	start := time.Now()
	if err := MarkProjectionWriteStarted(context.Background(), marker, writeMarkerTestWork(), nil, nil); err != nil {
		t.Fatalf("MarkProjectionWriteStarted() = %v, want nil", err)
	}
	if got := time.Since(start); got < 70*time.Millisecond {
		t.Fatalf("3 deferrals waited %v, want at least 10+20+40ms of pacing", got)
	}
	if got := marker.callCount(); got != 4 {
		t.Fatalf("marker calls = %d, want 4", got)
	}
}

// TestMarkProjectionWriteStartedPaceHonorsCancel keeps a canceled wait from
// sleeping out the backoff: the loop returns the shutdown outcome promptly.
func TestMarkProjectionWriteStartedPaceHonorsCancel(t *testing.T) {
	withRealWriteMarkerSleep(t)
	deferred := fmt.Errorf("lock timeout: %w", failure.ErrWorkWriteMarkerDeferred)
	errs := make([]error, DefaultWriteMarkerMaxAttempts)
	for i := range errs {
		errs[i] = deferred
	}
	marker := &fakeWriteMarker{errs: errs}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(25 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	err := MarkProjectionWriteStarted(ctx, marker, writeMarkerTestWork(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("MarkProjectionWriteStarted() = %v, want a shutdown error joining context.Canceled", err)
	}
	if got := time.Since(start); got > 5*time.Second {
		t.Fatalf("canceled wait took %v, want prompt shutdown, not the full bound", got)
	}
	if got := marker.callCount(); got >= DefaultWriteMarkerMaxAttempts {
		t.Fatalf("marker calls = %d, want fewer than the %d bound", got, DefaultWriteMarkerMaxAttempts)
	}
}

// TestServiceWriteMarkerDeferralCauseIsLogged pins the G1 enrichment: a
// fence-busy deferral and a generation-row deferral surface distinctly.
func TestServiceWriteMarkerDeferralCauseIsLogged(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		wrap    error
		wantMsg string
		want    string
	}{
		{
			name:    "fence_busy",
			wrap:    failure.ErrWorkWriteMarkerFenceBusy,
			wantMsg: `"msg":"projector write marker waiting for busy claim fence"`,
			want:    `"deferral_cause":"fence_busy"`,
		},
		{
			name:    "generation_row",
			wrap:    failure.ErrWorkWriteMarkerDeferred,
			wantMsg: `"msg":"projector write marker waiting for busy generation row"`,
			want:    `"deferral_cause":"generation_row"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			errs := []error{fmt.Errorf("deferred: %w", tc.wrap)}
			var logs syncBuffer
			service := writeMarkerTestService(&fakeWriteMarker{errs: errs}, &stubFactStore{}, &stubProjectionRunner{}, &stubProjectorWorkSink{})
			service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
			if err := service.Run(context.Background()); err != nil {
				t.Fatalf("Run() = %v", err)
			}
			if out := logs.String(); !strings.Contains(out, tc.wantMsg) || !strings.Contains(out, tc.want) {
				t.Fatalf("logs lack %s with %s:\n%s", tc.wantMsg, tc.want, out)
			}
		})
	}
}
