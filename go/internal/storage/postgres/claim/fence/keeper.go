// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fence

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Re-pin outcomes, the outcome label of eshu_dp_ingestion_commit_repins_total.
const (
	// RepinOK means the plane extended the pin.
	RepinOK = "ok"
	// RepinStale means the plane refused the re-pin; the commit is cancelled.
	RepinStale = "stale"
	// RepinError means the re-pin did not answer; the deadline check decides.
	RepinError = "error"
)

// KeeperConfig configures one commit's re-pin loop.
type KeeperConfig struct {
	Pinner  Pinner
	Code    CodeFunc
	Request Request
	// FirstDeadline is the deadline the pre-transaction pin obtained.
	FirstDeadline time.Time
	// Now is the store's Go clock. Each re-pin asks for Now()+Request.Lease.
	Now func() time.Time
	// Ticks drives re-pins. Production passes a time.Ticker channel at
	// Request.Lease/3; tests pass a channel they control.
	Ticks <-chan time.Time
	// OnRepin, when set, receives one outcome per re-pin attempt.
	OnRepin func(outcome string)
}

// Keeper re-pins one claim for the life of one commit transaction. It is
// safe for concurrent use: the commit goroutine reads LatestDeadline and
// Stale while the loop goroutine writes them.
type Keeper struct {
	latest   atomic.Int64
	stale    atomic.Pointer[Rejection]
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

// StartKeeper starts the re-pin loop. When the plane refuses a re-pin as
// stale or unknown, the loop records the Rejection and cancels the commit
// context with it as the cause, so every in-flight statement fails and the
// transaction rolls back. A transport failure is reported and ignored: the
// pre-COMMIT deadline check decides. The deadline only ever grows.
func StartKeeper(ctx context.Context, cancel context.CancelCauseFunc, config KeeperConfig) *Keeper {
	keeper := &Keeper{stop: make(chan struct{}), done: make(chan struct{})}
	keeper.latest.Store(config.FirstDeadline.UnixNano())
	go keeper.run(ctx, cancel, config)
	return keeper
}

func (k *Keeper) run(ctx context.Context, cancel context.CancelCauseFunc, config KeeperConfig) {
	defer close(k.done)
	request := config.Request
	for {
		select {
		case <-k.stop:
			return
		case <-ctx.Done():
			return
		case <-config.Ticks:
		}
		deadline := config.Now().Add(request.Lease)
		_, err := config.Pinner.PinClaimForCommit(ctx, request.ClaimID, request.FencingToken,
			request.ScopeID, request.SourceRunID, request.GenerationID, deadline)
		if err == nil {
			k.extend(deadline)
			report(config.OnRepin, RepinOK)
			continue
		}
		if reason, refused := refusalReason(config.Code(err)); refused {
			rejection := &Rejection{Reason: reason, RequestedToken: request.FencingToken, Err: err}
			k.stale.Store(rejection)
			report(config.OnRepin, RepinStale)
			cancel(rejection)
			return
		}
		report(config.OnRepin, RepinError)
	}
}

func (k *Keeper) extend(deadline time.Time) {
	next := deadline.UnixNano()
	for {
		current := k.latest.Load()
		if next <= current || k.latest.CompareAndSwap(current, next) {
			return
		}
	}
}

func report(onRepin func(string), outcome string) {
	if onRepin != nil {
		onRepin(outcome)
	}
}

// LatestDeadline returns the latest deadline the plane accepted.
func (k *Keeper) LatestDeadline() time.Time {
	return time.Unix(0, k.latest.Load())
}

// Stale returns the re-pin refusal, or nil while the claim is still pinned.
func (k *Keeper) Stale() error {
	if rejection := k.stale.Load(); rejection != nil {
		return rejection
	}
	return nil
}

// Stop ends the loop and waits for it. It is idempotent and must be called
// before the commit returns, at COMMIT or ROLLBACK.
func (k *Keeper) Stop() {
	k.stopOnce.Do(func() { close(k.stop) })
	<-k.done
}
