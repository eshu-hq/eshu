// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fence

import (
	"context"
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
	// AttemptTimeout bounds each re-pin call; zero or less leaves it bounded
	// only by the keeper's own context, which Stop cancels.
	AttemptTimeout time.Duration
}

// Keeper re-pins one claim for the life of one commit transaction. It is
// safe for concurrent use: the commit goroutine reads LatestDeadline and
// Stale while the loop goroutine writes them.
type Keeper struct {
	latest atomic.Int64
	stale  atomic.Pointer[Rejection]
	// halt cancels the loop's own context. Stop calls it before waiting, so
	// a re-pin blocked on a stalled plane returns at once instead of holding
	// the already-decided commit until the call gives up.
	halt context.CancelFunc
	done chan struct{}
}

// StartKeeper starts the re-pin loop. When the plane refuses a re-pin as
// stale or unknown, the loop records the Rejection and cancels the commit
// context with it as the cause, so every in-flight statement fails and the
// transaction rolls back. A transport failure is reported and ignored: the
// pre-COMMIT deadline check decides. The deadline only ever grows.
//
// The loop runs on its own context derived from ctx, so Stop can end an
// in-flight re-pin without cancelling ctx itself.
func StartKeeper(ctx context.Context, cancel context.CancelCauseFunc, config KeeperConfig) *Keeper {
	loopCtx, halt := context.WithCancel(ctx)
	keeper := &Keeper{halt: halt, done: make(chan struct{})}
	keeper.latest.Store(config.FirstDeadline.UnixNano())
	go keeper.run(loopCtx, cancel, config)
	return keeper
}

func (k *Keeper) run(ctx context.Context, cancel context.CancelCauseFunc, config KeeperConfig) {
	defer close(k.done)
	request := config.Request
	for {
		select {
		case <-ctx.Done():
			return
		case <-config.Ticks:
		}
		deadline := config.Now().Add(request.Lease)
		_, err := pinOnce(ctx, config.AttemptTimeout, config.Pinner, request, deadline)
		if ctx.Err() != nil {
			// Stopped (or the commit ended) while the call was in flight:
			// the commit's outcome is already decided, so the answer is
			// neither recorded nor reported.
			return
		}
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

// Stop cancels the loop, including any re-pin in flight, and waits for it to
// exit. It is idempotent and must be called before the commit returns, at
// COMMIT or ROLLBACK. It never cancels the commit context.
func (k *Keeper) Stop() {
	k.halt()
	<-k.done
}
