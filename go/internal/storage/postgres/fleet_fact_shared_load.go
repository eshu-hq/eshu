// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Shared fleet-load keys. One key per fleet-wide fact set; concurrent intents
// racing the same key share the leader's single pass.
const (
	sharedFleetLoadKeyContainerImageSLSA = "container-image-slsa"
	sharedFleetLoadKeyRepositoryCatalog  = "repository-catalog"
)

// fleetLoadResult is one finished fleet-wide pass: the rows every waiter is
// served, plus the payload bytes the repository catalog's load histograms
// record per served caller.
type fleetLoadResult struct {
	rows         []facts.Envelope
	payloadBytes int64
}

// sharedFleetLoadGroup coalesces concurrent duplicate fleet-wide fact loads
// within one process (#9697): 16 reducer workers storming the same
// cross-scope set pay for one paginated pass instead of 16. It retains
// nothing: the flight is deleted when the pass finishes, so the next call
// loads fresh from Postgres and a stale cache can never serve wrong graph
// truth. Sharing one pass is consistency-neutral versus today — every caller
// currently gets whatever its own multi-statement paginated pass read under
// the same READ COMMITTED tearing window; waiters now share one such pass
// instead of each reading their own.
//
// mu guards flights and is never held across the DB load.
type sharedFleetLoadGroup struct {
	mu      sync.Mutex
	flights map[string]*sharedFleetFlight
}

// sharedFleetFlight is one in-flight fleet-wide load. rows, err are written
// by the leader before it closes done and read by waiters only after done
// closes; the channel close is the happens-before edge.
type sharedFleetFlight struct {
	done chan struct{}
	res  fleetLoadResult
	err  error
}

func newSharedFleetLoadGroup() *sharedFleetLoadGroup {
	return &sharedFleetLoadGroup{flights: make(map[string]*sharedFleetFlight)}
}

// doResult runs load unless an equal-keyed load is already in flight, in
// which case the caller waits for that flight and shares its result. Every
// served caller gets a defensive copy, so no caller can mutate the rows
// another waiter shares. A load error is shared with its joinable waiters
// (standard singleflight sharing); each failed intent retries on the queue.
// A nil group loads directly, keeping zero-value FactStore test wiring on
// the unshared path.
func (g *sharedFleetLoadGroup) doResult(
	ctx context.Context,
	key string,
	load func(context.Context) (fleetLoadResult, error),
) (fleetLoadResult, error) {
	if g == nil {
		res, err := load(ctx)
		if err != nil {
			return fleetLoadResult{}, err
		}
		return fleetLoadResult{rows: defensiveCopyEnvelopes(res.rows), payloadBytes: res.payloadBytes}, nil
	}
	g.mu.Lock()
	if flight, ok := g.flights[key]; ok {
		g.mu.Unlock()
		select {
		case <-flight.done:
			if flight.err != nil {
				return fleetLoadResult{}, flight.err
			}
			return fleetLoadResult{
				rows:         defensiveCopyEnvelopes(flight.res.rows),
				payloadBytes: flight.res.payloadBytes,
			}, nil
		case <-ctx.Done():
			return fleetLoadResult{}, ctx.Err()
		}
	}
	flight := &sharedFleetFlight{done: make(chan struct{})}
	g.flights[key] = flight
	g.mu.Unlock()

	finished := false
	defer func() {
		// A panic in the load must not strand the waiters on a flight that
		// never closes; fail them with an error and let the panic continue.
		if !finished {
			flight.err = errors.New("fleet fact load aborted before it finished")
			g.mu.Lock()
			delete(g.flights, key)
			g.mu.Unlock()
			close(flight.done)
		}
	}()
	res, err := load(ctx)
	flight.res, flight.err = res, err
	g.mu.Lock()
	delete(g.flights, key)
	g.mu.Unlock()
	close(flight.done)
	finished = true
	if err != nil {
		return fleetLoadResult{}, err
	}
	return fleetLoadResult{rows: defensiveCopyEnvelopes(res.rows), payloadBytes: res.payloadBytes}, nil
}
