// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestClassifyBoundedGraphReadError pins the #7353 classification contract:
// an expired deadline on the bounded ctx makes any reader error a graph-read
// deadline, while a live or canceled ctx leaves the error untouched.
func TestClassifyBoundedGraphReadError(t *testing.T) {
	t.Parallel()

	driverErr := errors.New("ConnectivityError: Timeout while reading from connection")

	expired, cancelExpired := WithBoundedGraphReadDeadlineFor(context.Background(), time.Nanosecond)
	defer cancelExpired()
	<-expired.Done()

	callerExpired, cancelCaller := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancelCaller()
	<-callerExpired.Done()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	live := context.Background()

	tests := []struct {
		name         string
		ctx          context.Context
		err          error
		wantDeadline bool
		wantSame     bool // err returned unchanged
	}{
		{name: "nil error", ctx: expired, err: nil, wantSame: true},
		{name: "policy budget expired, driver error", ctx: expired, err: driverErr, wantDeadline: true},
		{name: "caller deadline expired, driver error", ctx: callerExpired, err: driverErr, wantDeadline: true},
		{name: "live ctx, raw DeadlineExceeded", ctx: live, err: fmt.Errorf("read: %w", context.DeadlineExceeded), wantDeadline: true},
		{name: "live ctx, driver error", ctx: live, err: driverErr, wantSame: true},
		{name: "canceled ctx, driver error", ctx: canceled, err: driverErr, wantSame: true},
		{name: "already a deadline", ctx: expired, err: ErrGraphReadDeadline, wantDeadline: true, wantSame: true},
		{name: "live ctx, unavailable", ctx: live, err: ErrGraphUnavailable, wantSame: true},
		// A reader that already classified an outage keeps that verdict even
		// if the bounded ctx expires before the handler looks: the response
		// is 503, so the deadline sentinel must not also be attached (F3).
		{name: "expired ctx, already unavailable", ctx: expired, err: fmt.Errorf("read: %w", ErrGraphUnavailable), wantSame: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyBoundedGraphReadError(tc.ctx, tc.err)
			if tc.wantSame && (!errors.Is(got, tc.err) || fmt.Sprint(got) != fmt.Sprint(tc.err)) {
				t.Fatalf("got %v, want the input error unchanged (%v)", got, tc.err)
			}
			if gotDeadline := errors.Is(got, ErrGraphReadDeadline); gotDeadline != tc.wantDeadline {
				t.Fatalf("errors.Is(got, ErrGraphReadDeadline) = %t, want %t (got %v)", gotDeadline, tc.wantDeadline, got)
			}
			if tc.wantDeadline && tc.err != nil && !errors.Is(got, tc.err) {
				t.Fatalf("got %v, want it to keep the reader's cause %v for logs", got, tc.err)
			}
		})
	}
}
