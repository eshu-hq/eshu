// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestPackageManifestBackfillWaitsForCancellationAndDrain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	wait := startPackageManifestConsumptionKeyBackfill(ctx, func(runCtx context.Context) error {
		close(started)
		<-runCtx.Done()
		<-release
		close(finished)
		return runCtx.Err()
	}, logger)

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("backfill did not start")
	}
	cancel()
	waited := make(chan struct{})
	go func() {
		wait()
		close(waited)
	}()
	select {
	case <-waited:
		t.Fatal("cleanup returned before in-flight backfill stopped")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not wait for backfill to stop")
	}
	select {
	case <-finished:
	default:
		t.Fatal("backfill still running after cleanup")
	}
}

func TestPackageManifestBackfillPollInterval(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ran   bool
		ready bool
		err   error
		want  time.Duration
	}{
		{name: "initial backfill", ran: true, ready: false, want: time.Second},
		{name: "ready repair", ran: true, ready: true, want: 30 * time.Second},
		{name: "contended replica", ran: false, want: 30 * time.Second},
		{name: "failed pass", ran: true, err: context.DeadlineExceeded, want: 30 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := packageManifestBackfillPollInterval(tc.ran, tc.ready, tc.err); got != tc.want {
				t.Fatalf("poll interval = %v, want %v", got, tc.want)
			}
		})
	}
}
