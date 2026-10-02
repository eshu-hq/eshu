// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// CheckpointSource captures the primary's committed visibility boundary without
// exposing a database handle to HTTP middleware or trusted status readers.
type CheckpointSource interface {
	ContextWithCheckpoint(context.Context) (context.Context, error)
}

// WithCheckpoint captures a fresh checkpoint immediately before selected
// business dispatch. Mount it inside authentication. A nil selection requires a
// checkpoint for every request; failure never dispatches or exposes its cause.
func WithCheckpoint(next http.Handler, source CheckpointSource, selected func(*http.Request) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if selected != nil && !selected(r) {
			next.ServeHTTP(w, r)
			return
		}
		if source == nil {
			writeReadUnavailable(w)
			return
		}
		ctx, err := source.ContextWithCheckpoint(r.Context())
		if err != nil {
			writeReadUnavailable(w)
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// writeReadUnavailable answers a failed checkpoint with a retryable 503. The
// body is a fixed string and Retry-After carries the shared reader retry hint,
// matching the query layer's backend_unavailable contract (#7523).
func writeReadUnavailable(w http.ResponseWriter) {
	w.Header().Set("Retry-After", strconv.Itoa(db.ReaderRetryAfterSeconds))
	http.Error(w, "database read unavailable", http.StatusServiceUnavailable)
}

// NewTrustedStatusReader gives the public runtime admin surface its own bounded
// checkpoint boundary. Application handlers must use the original guarded
// reader and obtain their checkpoint only after authorization.
func NewTrustedStatusReader(reader status.Reader, source CheckpointSource) status.Reader {
	return trustedStatusReader{reader: reader, source: source}
}

type trustedStatusReader struct {
	reader status.Reader
	source CheckpointSource
}

func (r trustedStatusReader) context(ctx context.Context) (context.Context, context.CancelFunc, error) {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	if r.source == nil || r.reader == nil {
		cancel()
		return nil, nil, errors.New("trusted status reader is not configured")
	}
	checkpoint, err := r.source.ContextWithCheckpoint(bounded)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return checkpoint, cancel, nil
}

func (r trustedStatusReader) ReadStatusSnapshot(ctx context.Context, asOf time.Time) (status.RawSnapshot, error) {
	checkpoint, cancel, err := r.context(ctx)
	if err != nil {
		return status.RawSnapshot{}, err
	}
	defer cancel()
	return r.reader.ReadStatusSnapshot(checkpoint, asOf)
}

func (r trustedStatusReader) ReadStatusSnapshotFiltered(ctx context.Context, asOf time.Time, selection status.SnapshotSelection) (status.RawSnapshot, error) {
	checkpoint, cancel, err := r.context(ctx)
	if err != nil {
		return status.RawSnapshot{}, err
	}
	defer cancel()
	return r.reader.ReadStatusSnapshotFiltered(checkpoint, asOf, selection)
}

func (r trustedStatusReader) CheckStatusReadiness(ctx context.Context) error {
	checkpoint, cancel, err := r.context(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	checker, ok := r.reader.(status.ReadinessChecker)
	if !ok {
		return errors.New("status reader does not support readiness checks")
	}
	return checker.CheckStatusReadiness(checkpoint)
}
