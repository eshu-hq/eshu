// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	// fanOutHolderHotMassRows is the per-id row-mass threshold that isolates
	// one lookup id into its own holder-read statement (#10643). A 5-id
	// statement over 67k candidate rows ran 9.8 s cold against the 10 s guard,
	// so 10k rows sits about 7x under the observed timeout mass at the same
	// query shape; a linear read of the single-hot-id shim worst case
	// (~0.78 s) puts a 10k-row statement near 0.5 s, 4x under the 2 s
	// per-statement bar. The same budget caps a multi-id statement's combined
	// mass, so no statement of two or more ids can rebuild the timed-out
	// shape out of sub-threshold ids. A singleton hot id may exceed the
	// threshold alone; that residual needs keyset paging, not more splitting.
	fanOutHolderHotMassRows = 10000
	// fanOutHolderReadParallelism bounds how many holder-read statements of
	// one pass run concurrently (#10643). Holder reads are I/O-read dominated
	// (72% of exec in the production EXPLAIN behind
	// docs/internal/evidence/10564-chunk5-production.md), so overlapping a
	// few statements hides disk waits; one 5-id chunk splits into at most 5
	// statements, so 4 drains a fully split chunk in about two waves while
	// one pass holds at most 4 read transactions, far under the pool even
	// with parallel reducer workers. This is a bound, not a target: cold
	// chunks still run one statement with no goroutines.
	fanOutHolderReadParallelism = 4
)

// holderMassEstimator is the optional mass probe behind adaptive holder-read
// splitting (#10643). A port that implements it reports, per requested lookup
// id, how many live finding rows name that id, with one entry for every
// requested id (zero for no match). A port without it keeps the legacy shape:
// one statement per fanOutHolderReadChunk-sized chunk. The probe runs under
// the same 10 s guard as the holder read and fails the same way, so a blind
// pass never proceeds on unmeasured ids.
type holderMassEstimator interface {
	// EstimateHolderMasses returns the live finding-row count per lookup id.
	EstimateHolderMasses(ctx context.Context, lookupIDs []string) (map[string]int64, error)
}

// fanOutSplitStats counts what adaptive splitting did for one pass: how many
// holder-read statements ran, how many chunks split, how many ids ran alone
// as hot, and the largest measured per-id mass. Operators read these on the
// fan-out span, the summary log line, and the result sub-signals.
type fanOutSplitStats struct {
	statements  int
	splitChunks int
	hotIDs      int
	maxMass     int64
}

// add folds one chunk's stats into the pass total.
func (s *fanOutSplitStats) add(other fanOutSplitStats) {
	s.statements += other.statements
	s.splitChunks += other.splitChunks
	s.hotIDs += other.hotIDs
	if other.maxMass > s.maxMass {
		s.maxMass = other.maxMass
	}
}

// planHolderReadStatements partitions one fanOutHolderReadChunk-sized chunk of
// lookup ids into the statements the holder read runs, by measured per-id
// row mass. An id over fanOutHolderHotMassRows, or with no measured mass,
// runs alone; cold ids pack in chunk order up to fanOutHolderReadChunk ids
// and a combined fanOutHolderHotMassRows budget. Every input id lands in
// exactly one statement, so the admitted set never changes.
func planHolderReadStatements(chunk []string, masses map[string]int64) [][]string {
	var out [][]string
	var cold []string
	var coldMass int64
	flush := func() {
		if len(cold) > 0 {
			out = append(out, cold)
			cold = nil
			coldMass = 0
		}
	}
	for _, id := range chunk {
		mass, ok := masses[id]
		if !ok || mass > fanOutHolderHotMassRows {
			flush()
			out = append(out, []string{id})
			continue
		}
		if len(cold) >= fanOutHolderReadChunk || coldMass+mass > fanOutHolderHotMassRows {
			flush()
		}
		cold = append(cold, id)
		coldMass += mass
	}
	flush()
	return out
}

// readHolderScopesSplit runs one chunk's holder read as the planned
// statements: one direct call when the plan is a single statement (the cold
// common case, byte-identical to the legacy call), otherwise the statements
// with bounded parallelism under the same per-statement guard. It returns the
// union of the scopes every statement found. A failed probe or statement
// fails closed with the existing read-error classification and counter, so
// the pass retries instead of proceeding without its holders.
func (h SupplyChainImpactHandler) readHolderScopesSplit(
	ctx context.Context,
	cause string,
	ownScopeID string,
	chunk []string,
) (map[string]struct{}, fanOutSplitStats, error) {
	var stats fanOutSplitStats
	statements := [][]string{chunk}
	if est, ok := h.FanOut.(holderMassEstimator); ok {
		masses, err := est.EstimateHolderMasses(ctx, chunk)
		if err != nil {
			h.countFanOutRead(ctx, cause, err, telemetry.FanOutReadSiteHolders)
			return nil, stats, fanOutError{fmt.Errorf("estimate supply chain impact holder mass: %w", err)}
		}
		statements = planHolderReadStatements(chunk, masses)
		for _, id := range chunk {
			if mass, ok := masses[id]; ok && mass > stats.maxMass {
				stats.maxMass = mass
			}
		}
	}
	stats.statements = len(statements)
	if len(statements) > 1 {
		stats.splitChunks = 1
		for _, stmt := range statements {
			if len(stmt) == 1 {
				stats.hotIDs++
			}
		}
	}
	if len(statements) == 1 {
		found, err := h.FanOut.FindHolderScopes(ctx, ownScopeID, statements[0])
		if err != nil {
			h.countFanOutRead(ctx, cause, err, telemetry.FanOutReadSiteHolders)
			return nil, stats, fanOutError{fmt.Errorf("find supply chain impact holders: %w", err)}
		}
		set := make(map[string]struct{}, len(found))
		for _, id := range found {
			set[id] = struct{}{}
		}
		return set, stats, nil
	}
	set := make(map[string]struct{})
	var mu sync.Mutex
	var firstErr error
	sem := make(chan struct{}, fanOutHolderReadParallelism)
	var wg sync.WaitGroup
	for _, stmt := range statements {
		wg.Add(1)
		sem <- struct{}{}
		go func(ids []string) {
			defer wg.Done()
			defer func() { <-sem }()
			found, err := h.FanOut.FindHolderScopes(ctx, ownScopeID, ids)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			for _, id := range found {
				set[id] = struct{}{}
			}
		}(stmt)
	}
	wg.Wait()
	if firstErr != nil {
		h.countFanOutRead(ctx, cause, firstErr, telemetry.FanOutReadSiteHolders)
		return nil, stats, fanOutError{fmt.Errorf("find supply chain impact holders: %w", firstErr)}
	}
	return set, stats, nil
}
