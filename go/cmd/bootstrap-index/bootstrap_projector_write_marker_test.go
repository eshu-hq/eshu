// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// scriptedBootstrapWriteMarker returns err from every call and counts them.
type scriptedBootstrapWriteMarker struct {
	err   error
	calls atomic.Int64
}

func (m *scriptedBootstrapWriteMarker) MarkProjectionWriteStarted(context.Context, projector.ScopeGenerationWork) error {
	m.calls.Add(1)
	return m.err
}

// TestDrainProjectorRequiresWriteMarker proves the #7389 marker is required.
func TestDrainProjectorRequiresWriteMarker(t *testing.T) {
	t.Parallel()
	source := &concurrentWorkSource{items: bootstrapDeltaItems(1)}
	err := drainProjector(context.Background(), source, &fakeFactStore{}, &fakeProjectionRunner{},
		&concurrentWorkSink{}, passBootstrapBaselineFence{}, nil, nil, 0, 1, nil, nil, nil)
	if err == nil {
		t.Fatal("drainProjector(nil write marker) = nil, want error")
	}
	if source.index != 0 {
		t.Fatalf("claimed %d items without a write marker, want 0", source.index)
	}
}

// TestDrainProjectorWriteMarkerRefusalWritesNothing proves a marker refusal
// stops the item before Project, sequential and concurrent: superseded and a
// lost claim are dropped, any other failure goes to Fail.
func TestDrainProjectorWriteMarkerRefusalWritesNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		err       error
		wantFails int64
		wantErr   bool
	}{
		{name: "superseded", err: fmt.Errorf("retired: %w", failure.ErrWorkSuperseded)},
		{name: "claim_lost", err: fmt.Errorf("stale: %w", failure.ErrWorkClaimLost)},
		{name: "database_error", err: errors.New("connection reset"), wantFails: 2, wantErr: true},
	} {
		for _, workers := range []int{1, 4} {
			t.Run(fmt.Sprintf("%s/workers=%d", tc.name, workers), func(t *testing.T) {
				t.Parallel()
				marker := &scriptedBootstrapWriteMarker{err: tc.err}
				runner := &countingBootstrapRunner{}
				sink := &countingBootstrapSink{}
				err := drainProjector(context.Background(), &concurrentWorkSource{items: bootstrapDeltaItems(2)},
					&fakeFactStore{}, runner, sink, passBootstrapBaselineFence{}, marker, nil, 0, workers, nil, nil, nil)
				if (err != nil) != tc.wantErr {
					t.Fatalf("drainProjector() = %v, want error %t", err, tc.wantErr)
				}
				if marker.calls.Load() != 2 || runner.calls.Load() != 0 || sink.acked.Load() != 0 || sink.failed.Load() != tc.wantFails {
					t.Fatalf("marks=%d project=%d ack=%d fail=%d, want 2 0 0 %d", marker.calls.Load(),
						runner.calls.Load(), sink.acked.Load(), sink.failed.Load(), tc.wantFails)
				}
			})
		}
	}
}

// TestBuildBootstrapProjectorWiresWriteMarker fails when bootstrap-index builds
// its projector without the #7389 write marker.
func TestBuildBootstrapProjectorWiresWriteMarker(t *testing.T) {
	t.Parallel()
	deps, err := buildBootstrapProjector(context.Background(), &fakeBootstrapSQLDB{}, &noopCanonicalWriter{},
		func(string) string { return "" }, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildBootstrapProjector() = %v", err)
	}
	if _, ok := deps.writeMarker.(postgres.ProjectorQueue); !ok {
		t.Fatalf("writeMarker type = %T, want postgres.ProjectorQueue", deps.writeMarker)
	}
}

// failingBootstrapHeartbeater fails every heartbeat with err.
type failingBootstrapHeartbeater struct{ err error }

func (h failingBootstrapHeartbeater) Heartbeat(context.Context, projector.ScopeGenerationWork) error {
	return h.err
}

// TestBootstrapHeartbeatSupersedeIsNotAnError keeps a routine supersede off
// the bootstrap ERROR log (#7389); a real heartbeat failure still logs it.
func TestBootstrapHeartbeatSupersedeIsNotAnError(t *testing.T) {
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
			var logs lockedBuffer
			ctx, stop := startBootstrapProjectorHeartbeat(context.Background(), bootstrapDeltaItems(1)[0],
				failingBootstrapHeartbeater{err: tc.err}, time.Millisecond, 0, slog.New(slog.NewJSONHandler(&logs, nil)))
			<-ctx.Done()
			_ = stop()
			gotError := strings.Contains(logs.String(), `"level":"ERROR","msg":"bootstrap projector lease heartbeat failed"`)
			if gotError != tc.wantError {
				t.Fatalf("ERROR heartbeat log = %t, want %t; logs:\n%s", gotError, tc.wantError, logs.String())
			}
		})
	}
}

// lockedBuffer is a bytes.Buffer safe for the heartbeat goroutine's writes.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
