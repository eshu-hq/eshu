// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// Next returns the next collected repository generation from the current
// stream. When the stream is exhausted, it resets the source so the next call
// triggers a fresh discovery cycle.
func (s *GitSource) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	if !s.started {
		if s.Selector == nil {
			return collector.CollectedGeneration{}, false, fmt.Errorf("git repository selector is required")
		}
		if err := s.startStream(ctx); err != nil {
			return collector.CollectedGeneration{}, false, err
		}
		s.started = true
	}

	select {
	case gen, ok := <-s.stream:
		if !ok {
			// Channel closed: stream exhausted. Reset for next discovery cycle.
			s.started = false
			if s.streamErr != nil {
				err := s.streamErr
				s.streamErr = nil
				return collector.CollectedGeneration{}, false, err
			}
			return collector.CollectedGeneration{}, false, nil
		}
		return gen, true, nil
	case <-ctx.Done():
		return collector.CollectedGeneration{}, false, ctx.Err()
	}
}

// logDiscoveryFailure marks the active scope.assign span as failed and logs a
// failed selection cycle (#8084): WARN when it is retryable (the collector
// loop skips the cycle), ERROR otherwise. A shutdown cancel is not a failure.
func (s *GitSource) logDiscoveryFailure(ctx context.Context, err error) {
	if err == nil || ctx.Err() != nil {
		return
	}
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	if s.Logger == nil {
		return
	}
	retryable := collector.IsRetryable(err)
	logFn := s.Logger.ErrorContext
	if retryable {
		logFn = s.Logger.WarnContext
	}
	logFn(
		ctx, "collector discovery failed",
		log.CollectorKind("git"),
		slog.Bool("retryable", retryable),
		log.Err(err),
	)
}
