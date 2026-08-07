// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

var errCodeCallLeaseHeartbeatRejected = errors.New("code call partition lease heartbeat rejected")

type codeCallLeaseHeartbeatStop func() error

func (r *CodeCallProjectionRunner) leaseHeartbeatInterval() time.Duration {
	interval := r.Config.leaseTTL() / 2
	if interval <= 0 {
		return time.Second
	}
	return interval
}

func (r *CodeCallProjectionRunner) startLeaseHeartbeat(
	ctx context.Context,
	partitionID int,
	partitionCount int,
) (context.Context, codeCallLeaseHeartbeatStop) {
	heartbeatCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)

	go func() {
		ticker := time.NewTicker(r.leaseHeartbeatInterval())
		defer ticker.Stop()

		var heartbeatErr error
		recordFailure := func(err error) {
			if heartbeatErr != nil {
				return
			}
			heartbeatErr = err
			r.logLeaseHeartbeatFailure(heartbeatCtx, heartbeatErr)
			cancel()
		}
		for {
			select {
			case <-heartbeatCtx.Done():
				done <- heartbeatErr
				return
			case <-ticker.C:
				claimed, err := r.LeaseManager.ClaimPartitionLease(
					heartbeatCtx,
					DomainCodeCalls,
					partitionID,
					partitionCount,
					r.Config.leaseOwner(),
					r.Config.leaseTTL(),
				)
				if err != nil {
					if errors.Is(err, context.Canceled) {
						continue
					}
					recordFailure(fmt.Errorf("heartbeat code call lease: %w", err))
					continue
				}
				if !claimed {
					recordFailure(errCodeCallLeaseHeartbeatRejected)
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

func (r *CodeCallProjectionRunner) logLeaseHeartbeatFailure(ctx context.Context, heartbeatErr error) {
	if r.Logger == nil {
		return
	}

	logAttrs := make([]any, 0, 6)
	for _, attr := range telemetry.DomainAttrs(string(DomainCodeCalls), "") {
		logAttrs = append(logAttrs, attr)
	}
	logAttrs = append(
		logAttrs,
		log.Queue("code_calls"),
		slog.Duration("heartbeat_interval", r.leaseHeartbeatInterval()),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
		telemetry.FailureClassAttr("lease_heartbeat_failure"),
		log.Err(heartbeatErr),
	)
	r.Logger.ErrorContext(ctx, "code call projection lease heartbeat failed", logAttrs...)
}
