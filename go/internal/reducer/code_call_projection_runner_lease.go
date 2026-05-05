package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
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

func (r *CodeCallProjectionRunner) startLeaseHeartbeat(ctx context.Context) (context.Context, codeCallLeaseHeartbeatStop) {
	heartbeatCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)

	go func() {
		ticker := time.NewTicker(r.leaseHeartbeatInterval())
		defer ticker.Stop()

		var heartbeatErr error
		for {
			select {
			case <-heartbeatCtx.Done():
				done <- heartbeatErr
				return
			case <-ticker.C:
				claimed, err := r.LeaseManager.ClaimPartitionLease(
					heartbeatCtx,
					DomainCodeCalls,
					0,
					1,
					r.Config.leaseOwner(),
					r.Config.leaseTTL(),
				)
				if err != nil {
					heartbeatErr = fmt.Errorf("heartbeat code call lease: %w", err)
					r.logLeaseHeartbeatFailure(heartbeatCtx, heartbeatErr)
					cancel()
					continue
				}
				if !claimed {
					heartbeatErr = errCodeCallLeaseHeartbeatRejected
					r.logLeaseHeartbeatFailure(heartbeatCtx, heartbeatErr)
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

func (r *CodeCallProjectionRunner) logLeaseHeartbeatFailure(ctx context.Context, heartbeatErr error) {
	if r.Logger == nil {
		return
	}

	logAttrs := make([]any, 0, 6)
	for _, attr := range telemetry.DomainAttrs(string(DomainCodeCalls), "") {
		logAttrs = append(logAttrs, attr)
	}
	logAttrs = append(logAttrs,
		slog.String("queue", "code_calls"),
		slog.Duration("heartbeat_interval", r.leaseHeartbeatInterval()),
		telemetry.PhaseAttr(telemetry.PhaseReduction),
		telemetry.FailureClassAttr("lease_heartbeat_failure"),
		slog.String("error", heartbeatErr.Error()),
	)
	r.Logger.ErrorContext(ctx, "code call projection lease heartbeat failed", logAttrs...)
}
