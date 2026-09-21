// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capture

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
	internalruntime "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// captureDirEnvVar is the second half of the capture opt-in: the directory
// recording files land in. Capture needs both this and
// ESHU_DIFFERENTIAL_CAPTURE set — two independent knobs, neither of which
// is set in production, so a stray flag alone can never record statement
// parameters outside a gate run.
const captureDirEnvVar = "ESHU_DIFFERENTIAL_CAPTURE_DIR"

// Session binds one process's differential capture: an in-memory recorder
// feeding the capture decorators plus the sink the records flush to. The
// zero value is invalid; use [Open].
type Session struct {
	recorder *backendconformance.DifferentialRecorder
	sink     *Sink
	backend  string
}

// Open starts a capture session for binary, or returns a nil session when
// capture is off. Off means the opt-in flag is unset: every decorator on a
// nil session is a passthrough, so the normal path pays one env read at
// startup and nothing per statement. A set flag without a directory fails
// closed instead of running a replay that records nothing.
func Open(getenv func(string) string, binary string) (*Session, error) {
	if !backendconformance.CaptureEnabled() {
		return nil, nil
	}
	dir := getenv(captureDirEnvVar)
	if dir == "" {
		return nil, fmt.Errorf("differential capture flag is set but %s is not", captureDirEnvVar)
	}
	backend, err := internalruntime.LoadGraphBackend(getenv)
	if err != nil {
		return nil, fmt.Errorf("differential capture backend: %w", err)
	}
	sink, err := OpenDir(dir, string(backend), binary)
	if err != nil {
		return nil, err
	}
	recorder := backendconformance.NewDifferentialRecorder()
	// Stream every record to disk as it executes: the replay binaries die
	// by SIGTERM, so waiting for Close would lose the whole recording.
	// Append errors stash inside the sink and surface from Close.
	recorder.OnRecord = func(record backendconformance.DifferentialRecord) {
		_ = sink.Append(record)
	}
	return &Session{recorder: recorder, sink: sink, backend: string(backend)}, nil
}

// Reader decorates the graph read seam with capture. On a nil session it
// returns inner unchanged.
func (s *Session) Reader(inner backendconformance.GraphQuery) backendconformance.GraphQuery {
	if s == nil {
		return inner
	}
	return backendconformance.WrapGraphQuery(inner, s.recorder, s.backend)
}

// Writer decorates the graph write seam with capture. On a nil session it
// returns inner unchanged.
func (s *Session) Writer(inner sourcecypher.Executor) sourcecypher.Executor {
	if s == nil {
		return inner
	}
	return backendconformance.WrapExecutor(inner, s.recorder, s.backend)
}

// Close flushes the sink and reports the first streaming error, if any. A
// nil session closes cleanly; a session that recorded nothing writes no
// file. Records already streamed during execution, so Close never
// re-appends them.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	return s.sink.Close()
}
