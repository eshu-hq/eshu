// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestDecideDeltaBaseline pins the #7319 decision table row by row.
func TestDecideDeltaBaseline(t *testing.T) {
	t.Parallel()
	pending := scope.GenerationStatusPending
	for _, tc := range []struct {
		name  string
		state DeltaBaselineState
		want  DeltaBaselineOutcome
	}{
		{"target missing", DeltaBaselineState{}, DeltaBaselineTargetMissing},
		{"full generation", DeltaBaselineState{
			TargetFound: true, TargetStatus: pending,
			ActiveGenerationID: "g-a", ActiveCommitSHA: "A",
		}, DeltaBaselineFull},
		{"legacy delta without baseline", DeltaBaselineState{
			TargetFound: true, TargetStatus: pending, IsDelta: true,
			ActiveGenerationID: "g-a", ActiveCommitSHA: "A",
		}, DeltaBaselineUnfenced},
		{"target already active", DeltaBaselineState{
			TargetFound: true, TargetStatus: scope.GenerationStatusActive,
			IsDelta: true, BaselineCommitSHA: "A", ActiveGenerationID: "g-d", ActiveCommitSHA: "D",
		}, DeltaBaselineAlreadyActive},
		{"baseline matches active commit", DeltaBaselineState{
			TargetFound: true, TargetStatus: pending, IsDelta: true,
			BaselineCommitSHA: "A", ActiveGenerationID: "g-a", ActiveCommitSHA: "A",
		}, DeltaBaselineMatched},
		{"match compares trimmed strings", DeltaBaselineState{
			TargetFound: true, TargetStatus: pending, IsDelta: true,
			BaselineCommitSHA: " A\n", ActiveGenerationID: "g-a", ActiveCommitSHA: "A ",
		}, DeltaBaselineMatched},
		{"active commit differs", DeltaBaselineState{
			TargetFound: true, TargetStatus: pending, IsDelta: true,
			BaselineCommitSHA: "A", ActiveGenerationID: "g-b", ActiveCommitSHA: "B",
		}, DeltaBaselineRefusedActiveDiffers},
		{"active generation has no commit", DeltaBaselineState{
			TargetFound: true, TargetStatus: pending, IsDelta: true,
			BaselineCommitSHA: "A", ActiveGenerationID: "g-x",
		}, DeltaBaselineRefusedActiveDiffers},
		{"no active generation", DeltaBaselineState{
			TargetFound: true, TargetStatus: pending, IsDelta: true,
			BaselineCommitSHA: "A",
		}, DeltaBaselineRefusedNoActive},
		{"failed target replayed with no active", DeltaBaselineState{
			TargetFound:  true,
			TargetStatus: scope.GenerationStatusFailed, IsDelta: true, BaselineCommitSHA: "A",
		}, DeltaBaselineRefusedNoActive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := DecideDeltaBaseline(tc.state); got != tc.want {
				t.Fatalf("DecideDeltaBaseline(%+v) = %q, want %q", tc.state, got, tc.want)
			}
		})
	}
}

func TestDeltaBaselineRefusalFailureClassByPhase(t *testing.T) {
	t.Parallel()
	if got := (DeltaBaselineRefusal{Phase: DeltaBaselinePhasePreflight}).FailureClass(); got != DeltaBaselineMismatchClass {
		t.Fatalf("preflight class = %q", got)
	}
	if got := (DeltaBaselineRefusal{Phase: DeltaBaselinePhaseAck}).FailureClass(); got != DeltaBaselineMismatchAfterProjectionClass {
		t.Fatalf("ack class = %q", got)
	}
	if got := (DeltaBaselineRefusal{}).ActiveCommitForLog(); got != "none" {
		t.Fatalf("ActiveCommitForLog with no active = %q, want none", got)
	}
}

// fakeDeltaBaselineFence records fence calls. The zero value passes every
// generation as a full one.
type fakeDeltaBaselineFence struct {
	mu        sync.Mutex
	state     DeltaBaselineState
	readErr   error
	refuseErr error
	reads     int
	refusals  []DeltaBaselineRefusal
}

func (f *fakeDeltaBaselineFence) ReadDeltaBaseline(context.Context, ScopeGenerationWork) (DeltaBaselineState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.readErr != nil {
		return DeltaBaselineState{}, f.readErr
	}
	state := f.state
	if !state.TargetFound && f.state == (DeltaBaselineState{}) {
		state.TargetFound = true
	}
	return state, nil
}

func (f *fakeDeltaBaselineFence) RefuseDeltaBaseline(_ context.Context, _ ScopeGenerationWork, r DeltaBaselineRefusal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refusals = append(f.refusals, r)
	if f.refuseErr != nil {
		return f.refuseErr
	}
	return fmt.Errorf("refused (failure_class=%s): %w", r.FailureClass(), failure.ErrWorkSuperseded)
}

func refusingFence() *fakeDeltaBaselineFence {
	return &fakeDeltaBaselineFence{state: DeltaBaselineState{
		TargetFound: true, TargetStatus: scope.GenerationStatusPending, IsDelta: true,
		BaselineCommitSHA: "A", ActiveGenerationID: "g-b", ActiveCommitSHA: "B",
	}}
}

func TestPreflightDeltaBaseline(t *testing.T) {
	t.Parallel()
	work := ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: "scope-1"},
		Generation: scope.ScopeGeneration{GenerationID: "gen-d"},
	}

	t.Run("pass proceeds without marking", func(t *testing.T) {
		t.Parallel()
		fence := &fakeDeltaBaselineFence{}
		if err := PreflightDeltaBaseline(context.Background(), fence, work, nil, nil); err != nil {
			t.Fatalf("PreflightDeltaBaseline() = %v, want nil", err)
		}
		if fence.reads != 1 || len(fence.refusals) != 0 {
			t.Fatalf("reads=%d refusals=%d, want 1 and 0", fence.reads, len(fence.refusals))
		}
	})

	t.Run("refusal marks with the preflight class and logs a warning", func(t *testing.T) {
		t.Parallel()
		fence := refusingFence()
		var logs bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logs, nil))
		err := PreflightDeltaBaseline(context.Background(), fence, work, nil, logger)
		if !errors.Is(err, failure.ErrWorkSuperseded) {
			t.Fatalf("PreflightDeltaBaseline() = %v, want ErrWorkSuperseded", err)
		}
		if len(fence.refusals) != 1 {
			t.Fatalf("refusals = %d, want 1", len(fence.refusals))
		}
		got := fence.refusals[0]
		if got.Phase != DeltaBaselinePhasePreflight || got.Outcome != DeltaBaselineRefusedActiveDiffers ||
			got.FailureClass() != DeltaBaselineMismatchClass {
			t.Fatalf("refusal = %+v", got)
		}
		line := logs.String()
		for _, want := range []string{
			`"level":"WARN"`, `"delta_baseline_commit_sha":"A"`,
			`"active_commit_sha":"B"`, `"failure_class":"projector_delta_baseline_mismatch"`, `"fence_phase":"preflight"`,
		} {
			if !strings.Contains(line, want) {
				t.Fatalf("log %s missing %s", line, want)
			}
		}
		if strings.Contains(line, "superseded by newer generation") {
			t.Fatalf("log reuses the newer-generation message: %s", line)
		}
	})

	t.Run("read error fails closed and is retryable", func(t *testing.T) {
		t.Parallel()
		fence := &fakeDeltaBaselineFence{readErr: errors.New("connection reset")}
		err := PreflightDeltaBaseline(context.Background(), fence, work, nil, nil)
		if err == nil || !failure.IsRetryable(err) {
			t.Fatalf("PreflightDeltaBaseline() = %v, want a retryable error", err)
		}
		if errors.Is(err, failure.ErrWorkSuperseded) || len(fence.refusals) != 0 {
			t.Fatalf("read error must not refuse: err=%v refusals=%d", err, len(fence.refusals))
		}
	})

	t.Run("missing target fails closed and is retryable", func(t *testing.T) {
		t.Parallel()
		fence := &fakeDeltaBaselineFence{state: DeltaBaselineState{IsDelta: true}}
		err := PreflightDeltaBaseline(context.Background(), fence, work, nil, nil)
		if err == nil || !failure.IsRetryable(err) {
			t.Fatalf("PreflightDeltaBaseline() = %v, want a retryable error", err)
		}
	})

	t.Run("lost claim during the mark is returned as claim lost", func(t *testing.T) {
		t.Parallel()
		fence := refusingFence()
		fence.refuseErr = fmt.Errorf("mark: %w", failure.ErrWorkClaimLost)
		err := PreflightDeltaBaseline(context.Background(), fence, work, nil, nil)
		if !errors.Is(err, failure.ErrWorkClaimLost) {
			t.Fatalf("PreflightDeltaBaseline() = %v, want ErrWorkClaimLost", err)
		}
	})

	t.Run("mark failure fails closed and is retryable", func(t *testing.T) {
		t.Parallel()
		fence := refusingFence()
		fence.refuseErr = errors.New("mark statement failed")
		err := PreflightDeltaBaseline(context.Background(), fence, work, nil, nil)
		if err == nil || !failure.IsRetryable(err) || errors.Is(err, failure.ErrWorkSuperseded) {
			t.Fatalf("PreflightDeltaBaseline() = %v, want a retryable non-superseded error", err)
		}
	})

	t.Run("nil fence is refused", func(t *testing.T) {
		t.Parallel()
		if err := PreflightDeltaBaseline(context.Background(), nil, work, nil, nil); err == nil {
			t.Fatal("PreflightDeltaBaseline(nil fence) = nil, want error")
		}
	})
}

type classedRefusal struct{ class string }

func (e classedRefusal) Error() string        { return "refused (" + e.class + ")" }
func (e classedRefusal) Unwrap() error        { return failure.ErrWorkSuperseded }
func (e classedRefusal) FailureClass() string { return e.class }

func TestIsDeltaBaselineRefusal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{fmt.Errorf("ack: %w", classedRefusal{DeltaBaselineMismatchClass}), true},
		{classedRefusal{DeltaBaselineMismatchAfterProjectionClass}, true},
		{classedRefusal{"projector_ack_generation_superseded"}, false},
		{failure.ErrWorkSuperseded, false},
		{errors.New("other"), false},
		{nil, false},
	} {
		if got := IsDeltaBaselineRefusal(tc.err); got != tc.want {
			t.Fatalf("IsDeltaBaselineRefusal(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

// TestRecordSupersededWorkNamesDeltaBaselineRefusal keeps the Service from
// logging an Ack-phase baseline refusal as "superseded by newer generation".
func TestRecordSupersededWorkNamesDeltaBaselineRefusal(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	service := Service{Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	refusal := classedRefusal{DeltaBaselineMismatchAfterProjectionClass}
	if !service.recordSupersededWork(context.Background(), ScopeGenerationWork{}, time.Now(), 0, refusal, 0) {
		t.Fatal("recordSupersededWork() = false, want true")
	}
	if line := logs.String(); strings.Contains(line, "newer generation") || !strings.Contains(line, "delta baseline") {
		t.Fatalf("log = %s, want the delta-baseline message", line)
	}
}
