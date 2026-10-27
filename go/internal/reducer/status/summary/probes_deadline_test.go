// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"testing"
	"time"
)

type delayedDeadlineContext struct {
	context.Context
	deadline time.Time
}

func (c delayedDeadlineContext) Deadline() (time.Time, bool) {
	return c.deadline, true
}

// shouldReportProbeError ignores a read-only probe's socket timeout at its
// deadline even when the context's cancellation has not become visible yet.
func shouldReportProbeError(ctx context.Context, err error, observedAt time.Time) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline || observedAt.Before(deadline) {
		return true
	}
	var timeout net.Error
	return !errors.As(err, &timeout) || !timeout.Timeout()
}

func TestShouldReportProbeError(t *testing.T) {
	observedAt := time.Date(2026, time.October, 27, 0, 0, 0, 0, time.UTC)
	socketTimeout := fmt.Errorf("dial: %w", &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded})
	otherError := errors.New("database unavailable")
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{"socket timeout after deadline before context observes cancellation", delayedDeadlineContext{context.Background(), observedAt.Add(-time.Nanosecond)}, socketTimeout, false},
		{"socket timeout exactly at deadline", delayedDeadlineContext{context.Background(), observedAt}, socketTimeout, false},
		{"socket timeout within active window", delayedDeadlineContext{context.Background(), observedAt.Add(time.Nanosecond)}, socketTimeout, true},
		{"other error after deadline while context is live", delayedDeadlineContext{context.Background(), observedAt.Add(-time.Nanosecond)}, otherError, true},
		{"canceled context", canceled, otherError, false},
		{"timeout without a deadline", context.Background(), socketTimeout, true},
		{"nil error", context.Background(), nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldReportProbeError(tt.ctx, tt.err, observedAt); got != tt.want {
				t.Errorf("shouldReportProbeError() = %t, want %t", got, tt.want)
			}
		})
	}
}
