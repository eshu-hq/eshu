// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestInfraReadModelReadyDecision(t *testing.T) {
	cases := []struct {
		name  string
		state infraReadModelState
		ready bool
		want  string
	}{
		{"not installed (main: graph path)", infraReadModelState{}, true, ""},
		{"installed, backfill not done", infraReadModelState{Installed: true}, false, "backfill marker"},
		{"installed, marked dirty", infraReadModelState{Installed: true, MarkerPresent: true, DirtyRepos: 3}, false, "3 repositories"},
		{"installed, marker and clean", infraReadModelState{Installed: true, MarkerPresent: true}, true, ""},
	}
	for _, tc := range cases {
		ready, reason := tc.state.ready()
		if ready != tc.ready {
			t.Errorf("%s: ready = %v, want %v (%s)", tc.name, ready, tc.ready, reason)
		}
		if tc.want != "" && !strings.Contains(reason, tc.want) {
			t.Errorf("%s: reason %q does not mention %q", tc.name, reason, tc.want)
		}
	}
}

func TestWaitForInfraReadModelPollsUntilTheBackfillClearsTheFence(t *testing.T) {
	states := []infraReadModelState{
		{Installed: true},
		{Installed: true, MarkerPresent: true, DirtyRepos: 2},
		{Installed: true, MarkerPresent: true},
	}
	calls := 0
	read := func(context.Context) (infraReadModelState, error) {
		s := states[calls]
		calls++
		return s, nil
	}

	var log bytes.Buffer
	if err := waitForInfraReadModel(context.Background(), read, time.Second, time.Millisecond, &log); err != nil {
		t.Fatalf("waitForInfraReadModel: %v", err)
	}
	if calls != 3 {
		t.Errorf("polled %d times, want 3", calls)
	}
	if !strings.Contains(log.String(), "dirty") {
		t.Errorf("log does not report the dirty repositories: %q", log.String())
	}
}

func TestWaitForInfraReadModelReturnsImmediatelyWhenNotInstalled(t *testing.T) {
	calls := 0
	read := func(context.Context) (infraReadModelState, error) { calls++; return infraReadModelState{}, nil }

	var log bytes.Buffer
	if err := waitForInfraReadModel(context.Background(), read, time.Second, time.Millisecond, &log); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !strings.Contains(log.String(), "not installed") {
		t.Errorf("calls = %d, log = %q, want one poll and a 'not installed' line", calls, log.String())
	}
}

// TestWaitForInfraReadModelFailsLoudlyWhenTheFenceNeverClears is the #6817
// finding path: if marks remain, readers stay on the graph and the sweep would
// measure the old path while looking green, so the run must fail and say why.
func TestWaitForInfraReadModelFailsLoudlyWhenTheFenceNeverClears(t *testing.T) {
	read := func(context.Context) (infraReadModelState, error) {
		return infraReadModelState{Installed: true, MarkerPresent: true, DirtyRepos: 5}, nil
	}

	err := waitForInfraReadModel(context.Background(), read, 20*time.Millisecond, time.Millisecond, &bytes.Buffer{})

	if err == nil || !strings.Contains(err.Error(), "5 repositories") {
		t.Fatalf("err = %v, want a timeout naming the 5 dirty repositories", err)
	}
}
