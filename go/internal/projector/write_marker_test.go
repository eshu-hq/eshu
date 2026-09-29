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

// fakeWriteMarker records write-marker calls. The zero value marks every
// generation; errs are returned in order, one per call, before that.
type fakeWriteMarker struct {
	mu    sync.Mutex
	calls int
	errs  []error
	// onMark runs on every call before the result is returned.
	onMark func(context.Context)
}

func (f *fakeWriteMarker) MarkProjectionWriteStarted(ctx context.Context, _ ScopeGenerationWork) error {
	f.mu.Lock()
	f.calls++
	var err error
	if len(f.errs) > 0 {
		err = f.errs[0]
		f.errs = f.errs[1:]
	}
	hook := f.onMark
	f.mu.Unlock()
	if hook != nil {
		hook(ctx)
	}
	return err
}

func (f *fakeWriteMarker) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func writeMarkerTestWork() ScopeGenerationWork {
	return ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: "scope-7389", ScopeKind: scope.KindRepository},
		Generation:   scope.ScopeGeneration{ScopeID: "scope-7389", GenerationID: "generation-b"},
		AttemptCount: 1,
	}
}

func writeMarkerTestService(marker ProjectionWriteMarker, factStore *stubFactStore, runner *stubProjectionRunner, sink *stubProjectorWorkSink) Service {
	return Service{
		PollInterval:       10 * time.Millisecond,
		WorkSource:         &stubProjectorWorkSource{workItems: []ScopeGenerationWork{writeMarkerTestWork()}},
		FactStore:          factStore,
		Runner:             runner,
		WorkSink:           sink,
		DeltaBaselineFence: &fakeDeltaBaselineFence{},
		WriteMarker:        marker,
		Wait:               func(context.Context, time.Duration) error { return context.Canceled },
	}
}

// TestServiceRequiresWriteMarker keeps a nil marker from meaning "write
// without recording it" (#7389).
func TestServiceRequiresWriteMarker(t *testing.T) {
	t.Parallel()
	service := writeMarkerTestService(nil, &stubFactStore{}, &stubProjectionRunner{}, &stubProjectorWorkSink{})
	if err := service.Run(context.Background()); !errors.Is(err, errWriteMarkerMissing) {
		t.Fatalf("Run() without a write marker = %v, want errWriteMarkerMissing", err)
	}
}

// TestServiceMarksWriteStartAfterLoadFactsBeforeProject pins the marker's
// place: after LoadFacts, before the first graph write.
func TestServiceMarksWriteStartAfterLoadFactsBeforeProject(t *testing.T) {
	t.Parallel()
	factStore := &stubFactStore{}
	runner := &stubProjectionRunner{}
	sink := &stubProjectorWorkSink{}
	var loadsAtMark, projectsAtMark int
	marker := &fakeWriteMarker{onMark: func(context.Context) {
		factStore.mu.Lock()
		loadsAtMark = factStore.loadCalls
		factStore.mu.Unlock()
		runner.mu.Lock()
		projectsAtMark = runner.runCalls
		runner.mu.Unlock()
	}}
	if err := writeMarkerTestService(marker, factStore, runner, sink).Run(context.Background()); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if marker.callCount() != 1 || loadsAtMark != 1 || projectsAtMark != 0 {
		t.Fatalf("marker calls = %d, loads at mark = %d, projects at mark = %d; want 1, 1, 0",
			marker.callCount(), loadsAtMark, projectsAtMark)
	}
	if runner.runCalls != 1 || sink.ackCalls != 1 {
		t.Fatalf("project calls = %d, acks = %d, want 1 and 1", runner.runCalls, sink.ackCalls)
	}
}

// TestServiceWriteMarkerOutcomes covers every marker result: superseded and a
// lost claim drop the work without writing, a deferral is re-run, and any other
// failure (including exhausted deferrals) goes to Fail as retryable.
func TestServiceWriteMarkerOutcomes(t *testing.T) {
	t.Parallel()
	superseded := fmt.Errorf("generation retired: %w", failure.ErrWorkSuperseded)
	claimLost := fmt.Errorf("stale attempt: %w", failure.ErrWorkClaimLost)
	deferred := fmt.Errorf("lock timeout: %w", failure.ErrWorkWriteMarkerDeferred)
	exhausted := make([]error, DefaultWriteMarkerMaxAttempts)
	for i := range exhausted {
		exhausted[i] = deferred
	}
	for _, tc := range []struct {
		name          string
		errs          []error
		wantMarks     int
		wantProjects  int
		wantAcks      int
		wantFails     int
		wantRetryable bool
	}{
		{name: "superseded_drops", errs: []error{superseded}, wantMarks: 1},
		{name: "claim_lost_drops", errs: []error{claimLost}, wantMarks: 1},
		{name: "deferred_reruns", errs: []error{deferred, deferred}, wantMarks: 3, wantProjects: 1, wantAcks: 1},
		{name: "other_error_fails_retryable", errs: []error{errors.New("connection reset")}, wantMarks: 1, wantFails: 1, wantRetryable: true},
		{name: "exhausted_deferrals_fail_retryable", errs: exhausted, wantMarks: DefaultWriteMarkerMaxAttempts, wantFails: 1, wantRetryable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			marker := &fakeWriteMarker{errs: append([]error(nil), tc.errs...)}
			runner := &stubProjectionRunner{}
			sink := &stubProjectorWorkSink{}
			if err := writeMarkerTestService(marker, &stubFactStore{}, runner, sink).Run(context.Background()); err != nil {
				t.Fatalf("Run() = %v", err)
			}
			if got := marker.callCount(); got != tc.wantMarks {
				t.Fatalf("marker calls = %d, want %d", got, tc.wantMarks)
			}
			if runner.runCalls != tc.wantProjects || sink.ackCalls != tc.wantAcks || sink.failCalls != tc.wantFails {
				t.Fatalf("projects/acks/fails = %d/%d/%d, want %d/%d/%d", runner.runCalls, sink.ackCalls,
					sink.failCalls, tc.wantProjects, tc.wantAcks, tc.wantFails)
			}
			if tc.wantFails > 0 && failure.IsRetryable(sink.failedWith) != tc.wantRetryable {
				t.Fatalf("Fail cause %v retryable = %t, want %t", sink.failedWith, !tc.wantRetryable, tc.wantRetryable)
			}
		})
	}
}

// TestServiceHeartbeatSupersedeIsNotAnError keeps a routine heartbeat
// supersede off the ERROR log (#7389); a real heartbeat failure still logs it.
func TestServiceHeartbeatSupersedeIsNotAnError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		err       error
		wantError bool
	}{
		{name: "superseded", err: fmt.Errorf("heartbeat: %w", failure.ErrWorkSuperseded)},
		{name: "claim_lost", err: fmt.Errorf("heartbeat: %w", failure.ErrWorkClaimLost)},
		{name: "database_error", err: errors.New("connection refused"), wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var logs syncBuffer
			service := writeMarkerTestService(&fakeWriteMarker{}, &stubFactStore{},
				&stubProjectionRunner{waitForContextCancellation: true}, &stubProjectorWorkSink{})
			service.Heartbeater = &stubProjectorWorkHeartbeater{failAfter: 1, err: tc.err}
			service.HeartbeatInterval = 5 * time.Millisecond
			service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
			_ = service.Run(context.Background())
			gotError := strings.Contains(logs.String(), `"level":"ERROR","msg":"projector lease heartbeat failed"`)
			if gotError != tc.wantError {
				t.Fatalf("ERROR heartbeat log = %t, want %t; logs:\n%s", gotError, tc.wantError, logs.String())
			}
		})
	}
}

// syncBuffer is a bytes.Buffer safe for the heartbeat goroutine's writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestServiceWriteMarkerDeferralIsLogged pins the #7389 F6 WARN: the first
// marker deferral and every 30th after, with scope, generation, attempt and
// retry count, and nothing in between.
func TestServiceWriteMarkerDeferralIsLogged(t *testing.T) {
	t.Parallel()
	deferred := fmt.Errorf("lock timeout: %w", failure.ErrWorkWriteMarkerDeferred)
	errs := make([]error, 31)
	for i := range errs {
		errs[i] = deferred
	}
	var logs syncBuffer
	service := writeMarkerTestService(&fakeWriteMarker{errs: errs}, &stubFactStore{}, &stubProjectionRunner{}, &stubProjectorWorkSink{})
	service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	var warned []string
	for _, line := range lines {
		if strings.Contains(line, `"msg":"projector write marker waiting for busy generation row"`) {
			warned = append(warned, line)
		}
	}
	if len(warned) != 2 {
		t.Fatalf("deferral WARN lines = %d, want 2 (retry 1 and 30):\n%s", len(warned), logs.String())
	}
	for i, retry := range []string{`"marker_retry":1,`, `"marker_retry":30,`} {
		for _, want := range []string{`"level":"WARN"`, retry, `"scope_id":"scope-7389"`, `"generation_id":"generation-b"`, `"attempt_count":1`} {
			if !strings.Contains(warned[i], want) {
				t.Errorf("WARN line %d lacks %s: %s", i, want, warned[i])
			}
		}
	}
}
