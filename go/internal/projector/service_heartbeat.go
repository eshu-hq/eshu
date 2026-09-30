// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector //nolint:dirgate // Root owns the projector Service heartbeat loop; service/ owns only service-catalog reducer-intent routing.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// projectorHeartbeatStop stops a work item's heartbeat and returns the error
// that ended it, or nil.
type projectorHeartbeatStop func() error

func (s Service) startHeartbeat(ctx context.Context, work ScopeGenerationWork, workerID int) (context.Context, projectorHeartbeatStop) {
	if s.Heartbeater == nil || s.HeartbeatInterval <= 0 {
		return ctx, func() error { return nil }
	}

	heartbeatCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(s.HeartbeatInterval)
		defer ticker.Stop()

		var heartbeatErr error
		for {
			select {
			case <-heartbeatCtx.Done():
				done <- heartbeatErr
				return
			case <-ticker.C:
				if err := s.Heartbeater.Heartbeat(heartbeatCtx, work); err != nil {
					if heartbeatCtx.Err() != nil && errors.Is(err, heartbeatCtx.Err()) {
						done <- nil
						return
					}
					heartbeatErr = fmt.Errorf("heartbeat projector work: %w", err)
					// A lost claim is expected under attempt fencing and processWork
					// logs it at WARN; a superseded generation is routine and
					// processWork logs it at INFO (#7389). Neither may page as a
					// heartbeat failure.
					if s.Logger != nil && !errors.Is(err, failure.ErrWorkClaimLost) &&
						!errors.Is(err, failure.ErrWorkSuperseded) {
						scopeAttrs := telemetry.ScopeAttrs(work.Scope.ScopeID, work.Generation.GenerationID, work.Scope.SourceSystem)
						logAttrs := make([]any, 0, len(scopeAttrs)+4)
						for _, a := range scopeAttrs {
							logAttrs = append(logAttrs, a)
						}
						logAttrs = append(logAttrs, log.WorkerID(fmt.Sprintf("%d", workerID)))
						logAttrs = append(logAttrs, slog.Duration("heartbeat_interval", s.HeartbeatInterval))
						logAttrs = append(logAttrs, telemetry.PhaseAttr(telemetry.PhaseProjection))
						logAttrs = append(logAttrs, telemetry.FailureClassAttr("lease_heartbeat_failure"))
						logAttrs = append(logAttrs, log.Err(heartbeatErr))
						s.Logger.ErrorContext(heartbeatCtx, "projector lease heartbeat failed", logAttrs...)
					}
					cancel()
				}
			}
		}
	}()

	var once sync.Once
	return heartbeatCtx, func() error {
		var heartbeatErr error
		once.Do(func() {
			cancel()
			heartbeatErr = <-done
		})
		return heartbeatErr
	}
}
