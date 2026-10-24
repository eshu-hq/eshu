// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package producer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

// The store satisfies the reconciliation handler's port; a signature drift
// fails to compile.
var _ securityalert.ImpactProducerReadiness = ImpactStore{}

func TestImpactProducerStateMapsTheAggregateRow(t *testing.T) {
	t.Parallel()

	workableAt := time.Date(2026, time.October, 15, 11, 59, 0, 0, time.UTC)
	fake := &fakeQueryer{rows: [][]any{{1, 2, 0, 1, 2, 1, 7, workableAt}}}

	got, err := ImpactStore{DB: fake}.ImpactProducerState(context.Background(), "scope-a", "gen-1")
	if err != nil {
		t.Fatalf("ImpactProducerState() error = %v", err)
	}
	want := securityalert.ImpactProducerState{
		Pending: 1, Retrying: 2, Claimed: 0, Running: 1, Succeeded: 2, Failed: 1, Total: 7,
		NewestInFlightWorkableAt: workableAt,
	}
	if got != want {
		t.Fatalf("ImpactProducerState() = %+v, want %+v", got, want)
	}
	if got.InFlight() != 4 {
		t.Fatalf("InFlight() = %d, want 4", got.InFlight())
	}
	if len(fake.args) != 3 || fake.args[0] != "scope-a" || fake.args[1] != "gen-1" || fake.args[2] != "supply_chain_impact" {
		t.Fatalf("args = %v, want [scope-a gen-1 supply_chain_impact]", fake.args)
	}
	for _, want := range []string{
		"count(*) FILTER (WHERE work.status = 'succeeded')",
		"count(*) FILTER (WHERE work.status IN ('dead_letter', 'failed'))",
		"work.scope_id = $1",
		"work.generation_id = $2",
		"work.stage = 'reducer'",
		"work.domain = $3",
	} {
		if !strings.Contains(fake.query, want) {
			t.Fatalf("query missing %q:\n%s", want, fake.query)
		}
	}
	if strings.Contains(fake.query, "updated_at") {
		t.Fatalf("query must anchor on last-workable columns, never updated_at:\n%s", fake.query)
	}
}

func TestImpactProducerStateTreatsNoWorkAsTerminal(t *testing.T) {
	t.Parallel()

	fake := &fakeQueryer{rows: [][]any{{0, 0, 0, 0, 0, 0, 0, nil}}}
	got, err := ImpactStore{DB: fake}.ImpactProducerState(context.Background(), "scope-a", "gen-1")
	if err != nil {
		t.Fatalf("ImpactProducerState() error = %v", err)
	}
	if got.InFlight() != 0 || got.Total != 0 || !got.NewestInFlightWorkableAt.IsZero() {
		t.Fatalf("ImpactProducerState() = %+v, want the zero state", got)
	}
	if got.Settled() {
		t.Fatal("Settled() = true for no impact work, want false: no producer ran, so no-match is not proven (#8956)")
	}
}

// TestImpactProducerStateSettledNeedsEveryRowSucceeded pins the #8956 genuine
// no-match rule: the producer ran and every row of the generation succeeded.
func TestImpactProducerStateSettledNeedsEveryRowSucceeded(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		state securityalert.ImpactProducerState
		want  bool
	}{
		{"all succeeded", securityalert.ImpactProducerState{Succeeded: 2, Total: 2}, true},
		{"none enqueued", securityalert.ImpactProducerState{}, false},
		{"dead-lettered", securityalert.ImpactProducerState{Succeeded: 1, Failed: 1, Total: 2}, false},
		{"superseded or other terminal", securityalert.ImpactProducerState{Succeeded: 1, Total: 2}, false},
		{"still pending", securityalert.ImpactProducerState{Pending: 1, Succeeded: 1, Total: 2}, false},
	} {
		if got := tc.state.Settled(); got != tc.want {
			t.Fatalf("%s: Settled() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// An unanswerable question is an error, never an empty state: an empty state
// reads as "no impact work in flight" and would let the handler spend attempts
// or give up while the producer is still running.
func TestImpactProducerStateErrorsInsteadOfAnsweringEmpty(t *testing.T) {
	t.Parallel()

	if _, err := (ImpactStore{}).ImpactProducerState(context.Background(), "scope-a", "gen-1"); err == nil {
		t.Fatal("nil database: error = nil, want an error")
	}
	if _, err := (ImpactStore{DB: &fakeQueryer{err: context.DeadlineExceeded}}).
		ImpactProducerState(context.Background(), "scope-a", "gen-1"); err == nil {
		t.Fatal("query failure: error = nil, want an error")
	}
	if _, err := (ImpactStore{DB: &fakeQueryer{}}).
		ImpactProducerState(context.Background(), "scope-a", "gen-1"); err == nil {
		t.Fatal("no row: error = nil, want an error (an aggregate always returns one row)")
	}
	if _, err := (ImpactStore{DB: &fakeQueryer{}}).
		ImpactProducerState(context.Background(), " ", "gen-1"); err == nil {
		t.Fatal("blank scope: error = nil, want an error")
	}
}
