// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// fakeInfraBackfiller stands in for inventory.Backfiller so the seed step's
// decision logic is testable without Postgres. The real Backfiller is proven
// by the gate's live run, where awaitInfraReadModel and
// assertInfraServedFromReadModel re-check the same outcome from outside.
type fakeInfraBackfiller struct {
	result inventory.BackfillResult
	err    error
	calls  int
}

func (f *fakeInfraBackfiller) Run(context.Context) (inventory.BackfillResult, error) {
	f.calls++
	return f.result, f.err
}

func fixedInfraState(state infraReadModelState, err error) func(context.Context) (infraReadModelState, error) {
	return func(context.Context) (infraReadModelState, error) { return state, err }
}

func TestRunInfraBackfillStandInPassesOnMarkerAndCleanFence(t *testing.T) {
	backfiller := &fakeInfraBackfiller{result: inventory.BackfillResult{Repositories: 33, RowsInserted: 900000}}
	var log bytes.Buffer

	err := runInfraBackfillStandIn(context.Background(), backfiller,
		fixedInfraState(infraReadModelState{Installed: true, MarkerPresent: true}, nil), &log)
	if err != nil {
		t.Fatalf("runInfraBackfillStandIn error = %v, want nil", err)
	}
	if backfiller.calls != 1 {
		t.Fatalf("Backfiller.Run calls = %d, want 1", backfiller.calls)
	}
}

func TestRunInfraBackfillStandInNamesTheStandInInOneLogLine(t *testing.T) {
	var log bytes.Buffer
	err := runInfraBackfillStandIn(context.Background(), &fakeInfraBackfiller{},
		fixedInfraState(infraReadModelState{Installed: true, MarkerPresent: true}, nil), &log)
	if err != nil {
		t.Fatalf("runInfraBackfillStandIn error = %v", err)
	}
	standInLines := 0
	for _, line := range strings.Split(strings.TrimSpace(log.String()), "\n") {
		if strings.Contains(line, infraBackfillStandIn) {
			standInLines++
			if !strings.Contains(line, "no product runtime") {
				t.Fatalf("stand-in line %q does not say no product runtime runs the backfill", line)
			}
		}
	}
	if standInLines != 1 {
		t.Fatalf("log %q holds %d lines naming %q, want exactly 1", log.String(), standInLines, infraBackfillStandIn)
	}
}

func TestRunInfraBackfillStandInFailsWhenBackfillerFails(t *testing.T) {
	boom := errors.New("derive failed")
	err := runInfraBackfillStandIn(context.Background(), &fakeInfraBackfiller{err: boom},
		fixedInfraState(infraReadModelState{Installed: true, MarkerPresent: true}, nil), &bytes.Buffer{})
	if !errors.Is(err, boom) {
		t.Fatalf("runInfraBackfillStandIn error = %v, want it to wrap %v", err, boom)
	}
}

func TestRunInfraBackfillStandInFailsWhenStateReadFails(t *testing.T) {
	boom := errors.New("state read failed")
	err := runInfraBackfillStandIn(context.Background(), &fakeInfraBackfiller{},
		fixedInfraState(infraReadModelState{}, boom), &bytes.Buffer{})
	if !errors.Is(err, boom) {
		t.Fatalf("runInfraBackfillStandIn error = %v, want it to wrap %v", err, boom)
	}
}

func TestRunInfraBackfillStandInRefusesAnUnreadyReadModel(t *testing.T) {
	cases := []struct {
		name  string
		state infraReadModelState
		want  string
	}{
		{"read model not installed", infraReadModelState{}, "not installed"},
		{"marker absent after a clean Run", infraReadModelState{Installed: true}, "marker"},
		{"dirty repositories remain", infraReadModelState{Installed: true, MarkerPresent: true, DirtyRepos: 2}, "2 repositories"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runInfraBackfillStandIn(context.Background(), &fakeInfraBackfiller{},
				fixedInfraState(tc.state, nil), &bytes.Buffer{})
			if err == nil {
				t.Fatalf("runInfraBackfillStandIn error = nil, want a failure containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("runInfraBackfillStandIn error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}
