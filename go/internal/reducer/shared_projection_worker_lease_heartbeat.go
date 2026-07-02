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

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// errSharedProjectionLeaseHeartbeatRejected signals that a partition lease
// heartbeat renewal was rejected (another worker won the lease), matching
// the sibling code-calls lane's rejection error (#4449).
var errSharedProjectionLeaseHeartbeatRejected = errors.New("shared projection partition lease heartbeat rejected")

type sharedProjectionLeaseHeartbeatStop func() error

// sharedProjectionLeaseHeartbeatInterval derives the renewal interval from
// the lease TTL, matching the sibling code-calls and repo-dependency
// projection lanes (TTL/2). A non-positive TTL falls back to one second so a
// misconfigured TTL cannot spin the renewal ticker.
func sharedProjectionLeaseHeartbeatInterval(leaseTTL time.Duration) time.Duration {
	interval := leaseTTL / 2
	if interval <= 0 {
		return time.Second
	}
	return interval
}

// startSharedProjectionLeaseHeartbeat renews cfg's partition lease at
// TTL/2 for the lifetime of one ProcessPartitionOnce cycle. Without this,
// ProcessPartitionOnce claims the lease once and holds it passively through
// selection/retract/edge-write/mark-completed; a slow backend or large
// partition whose processing exceeds the lease TTL lets the lease be
// reclaimed by another worker while the original holder is still writing,
// causing a double-write (#4449). The returned context is cancelled if a
// renewal fails or is rejected, so downstream work in the same cycle
// observes the loss of the lease instead of continuing to write under it.
func startSharedProjectionLeaseHeartbeat(
	ctx context.Context,
	cfg PartitionProcessorConfig,
	leaseManager PartitionLeaseManager,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (context.Context, sharedProjectionLeaseHeartbeatStop) {
	heartbeatCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)

	go func() {
		ticker := time.NewTicker(sharedProjectionLeaseHeartbeatInterval(cfg.LeaseTTL))
		defer ticker.Stop()

		var heartbeatErr error
		for {
			select {
			case <-heartbeatCtx.Done():
				done <- heartbeatErr
				return
			case <-ticker.C:
				claimed, err := leaseManager.ClaimPartitionLease(
					heartbeatCtx, cfg.Domain, cfg.PartitionID, cfg.PartitionCount,
					cfg.LeaseOwner, cfg.LeaseTTL,
				)
				if err != nil {
					heartbeatErr = fmt.Errorf("heartbeat shared projection partition lease: %w", err)
					recordSharedProjectionLeaseHeartbeatMissed(heartbeatCtx, cfg, instruments, logger, heartbeatErr)
					cancel()
					continue
				}
				if !claimed {
					heartbeatErr = errSharedProjectionLeaseHeartbeatRejected
					recordSharedProjectionLeaseHeartbeatMissed(heartbeatCtx, cfg, instruments, logger, heartbeatErr)
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

func recordSharedProjectionLeaseHeartbeatMissed(
	ctx context.Context,
	cfg PartitionProcessorConfig,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
	heartbeatErr error,
) {
	if instruments != nil {
		instruments.SharedProjectionPartitionHeartbeatMissed.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrDomain(cfg.Domain),
		))
	}
	if logger == nil {
		return
	}

	logAttrs := make([]any, 0, 7)
	for _, attr := range telemetry.DomainAttrs(cfg.Domain, "") {
		logAttrs = append(logAttrs, attr)
	}
	logAttrs = append(
		logAttrs,
		log.Queue("shared_projection"),
		slog.Int("partition_id", cfg.PartitionID),
		slog.Int("partition_count", cfg.PartitionCount),
		slog.Duration("heartbeat_interval", sharedProjectionLeaseHeartbeatInterval(cfg.LeaseTTL)),
		telemetry.PhaseAttr(telemetry.PhaseShared),
		telemetry.FailureClassAttr("lease_heartbeat_failure"),
		log.Err(heartbeatErr),
	)
	logger.ErrorContext(ctx, "shared projection partition lease heartbeat failed", logAttrs...)
}
