// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"sync"
	"time"

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
	// threshold alone; at or under the routing gate it still runs the shipped
	// single holder statement, which passes isolated on direct-only ids.
	// Above the gate it reads as key ranges (#10972 H2: member-carrying
	// singletons time out as shipped singletons and concurrent direct-only
	// singletons breach the 2 s p95 bar, while the key-range read passes
	// every set).
	fanOutHolderHotMassRows = 10000
	// fanOutHolderReadParallelism bounds how many holder-read statements of
	// one pass run concurrently (#10643). Holder reads are I/O-read dominated
	// (72% of exec in the production EXPLAIN behind
	// (#10564)), so overlapping a
	// few statements hides disk waits; one 5-id chunk splits into at most 5
	// statements, so 4 drains a fully split chunk in about two waves while
	// one pass holds at most 4 read transactions, far under the pool even
	// with parallel reducer workers. This is a bound, not a target: cold
	// chunks still run one statement with no goroutines.
	fanOutHolderReadParallelism = 4
)

// HolderMassProbeCap bounds the per-id mass the holder-mass probe reports
// (#10681). It sits one row over fanOutHolderHotMassRows, so a capped report
// decides exactly like the true count for the cheap arms and the total: at
// or under the threshold the probe is exact and cold ids pack the same way,
// over the threshold the probe reports the cap and the id still runs alone.
// The member arm is exact at or under HolderMassProbeFillToCapRows and
// saturates above it (split-safe). Operators read the probe mass saturated:
// a probe-hot singleton's max_id_mass_rows tops out at the cap unless the
// routing-mass count resolved its routing mass for the key-range gate
// (#10972 H2 A2, Lane-1b), which then replaces the saturated value. The
// production probe builds its SQL LEAST, CASE saturation value, and
// already-hot short-circuit from this constant while the member-arm LIMIT
// comes from HolderMassProbeFillToCapRows, and the probe-cap guard test
// pins the derivation, so the number lives here alone.
const HolderMassProbeCap = fanOutHolderHotMassRows + 1

// HolderMassProbeFillToCapRows bounds the group-members arm of the holder-mass
// probe (#10681 design B). The arm counts through a per-id LATERAL with
// LIMIT HolderMassProbeFillToCapRows+1 and reports HolderMassProbeCap above
// that many rows, exact at or under it. It sits far under
// fanOutHolderHotMassRows, so cold ids (exact, <= 100 member rows) pack
// exactly as before while the per-id heap fetch stays bounded at cold cost;
// the residual overestimate (a 101st..threshold member row reports the cap)
// only splits more, never less. The postgres probe builds its arm-3 LIMIT
// and CASE from this constant, and the probe-cap guard test pins the value,
// so the number lives here alone beside HolderMassProbeCap.
const HolderMassProbeFillToCapRows = 100

// HolderMassEstimator is the optional mass probe behind adaptive holder-read
// splitting (#10643). The production probe reports, per requested lookup
// id, how many live finding rows name that id, capped at HolderMassProbeCap
// with one entry for every requested id (zero for no match). Member-arm
// counts above HolderMassProbeFillToCapRows saturate to the cap, so
// mid-band ids intentionally read hot. A port without
// it keeps the legacy shape: one statement per fanOutHolderReadChunk-sized
// chunk. The probe runs under the same 10 s guard as the holder read and
// fails the same way, so a blind pass never proceeds on unmeasured ids.
type HolderMassEstimator interface {
	// EstimateHolderMasses returns the live finding-row count per lookup id.
	EstimateHolderMasses(ctx context.Context, lookupIDs []string) (map[string]int64, error)
}

// fanOutSplitStats counts what adaptive splitting did for one pass: how many
// holder-read statements ran, how many chunks split, how many singleton
// statements ran (hot by mass, or isolated by packing/unknown mass —
// hotIDs overstates "hot", read it as singletons), and the largest measured
// per-id mass. Operators read these on the fan-out span, the summary log
// line, and the result sub-signals.
type fanOutSplitStats struct {
	statements  int
	splitChunks int
	hotIDs      int
	maxMass     int64
	// keyRangedIDs counts hot singleton ids read as key ranges (#10643,
	// gated by HolderKeyRangeMassGate since #10972 H2 A2) and
	// keyRangeStatements the key-range statements they ran. Each key-range
	// statement carries the member arm as its foreign-member token arm
	// (#10643 PR 2), so there is no separate member statement to count.
	keyRangedIDs       int
	keyRangeStatements int
}

// add folds one chunk's stats into the pass total.
func (s *fanOutSplitStats) add(other fanOutSplitStats) {
	s.statements += other.statements
	s.splitChunks += other.splitChunks
	s.hotIDs += other.hotIDs
	s.keyRangedIDs += other.keyRangedIDs
	s.keyRangeStatements += other.keyRangeStatements
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

// holderStatement is one planned holder-read statement: its lookup ids and
// whether it runs as a key-range read (#10972 H2 A2, Lane-1b: a probe-hot
// singleton whose routing mass sits above HolderKeyRangeMassGate, on a port
// with both the routing-mass and key-range seams). Every other statement runs the
// already-shipped single holder statement (FindSupplyChainImpactHoldersQuery)
// once.
type holderStatement struct {
	ids    []string
	ranged bool
}

// readHolderScopesSplit runs one chunk's holder read as the planned
// statements: one direct call when the plan is a single unranged statement
// (the common case, byte-identical to the legacy call), otherwise the
// statements with bounded parallelism under the same per-statement guard. A
// probe-hot singleton whose routing mass sits above HolderKeyRangeMassGate
// runs as a key-range read when the port has both the routing-mass and
// key-range seams (#10972 H2 A2, Lane-1b). It returns the union of the
// scopes every statement found. A failed probe, routing-mass count, or
// statement fails closed with the
// existing read-error classification and counter, so the pass retries instead
// of proceeding without its holders.
func (h SupplyChainImpactHandler) readHolderScopesSplit(
	ctx context.Context,
	cause string,
	ownScopeID string,
	chunk []string,
) (map[string]struct{}, fanOutSplitStats, error) {
	var stats fanOutSplitStats
	planned := [][]string{chunk}
	var masses map[string]int64
	if est, ok := h.FanOut.(HolderMassEstimator); ok {
		start := time.Now()
		var err error
		masses, err = est.EstimateHolderMasses(ctx, chunk)
		h.observeFanOutRead(ctx, telemetry.FanOutReadKindMassProbe, time.Since(start), err)
		if err != nil {
			h.countFanOutRead(ctx, cause, err, telemetry.FanOutReadSiteHolders)
			return nil, stats, fanOutReadFailure(err, "estimate supply chain impact holder mass")
		}
		planned = planHolderReadStatements(chunk, masses)
		for _, id := range chunk {
			if mass, ok := masses[id]; ok && mass > stats.maxMass {
				stats.maxMass = mass
			}
		}
	}
	// #10972 H2 A2 (routing-mass semantics since Lane-1b): the probe
	// saturates every hot id at HolderMassProbeCap, so a probe-hot
	// singleton's routing needs its routing mass. One batched count
	// resolves every probe-hot singleton of the chunk, but only when the
	// port can range: without the key-range seam the count could not move
	// the routing, so it never runs and maxMass stays at the probe cap.
	// Each resolved id above HolderKeyRangeMassGate reads as key-range
	// statements, and everything else reads the shipped single holder
	// statement. Without the routing-mass seam the router cannot tell a
	// mid-band id from a truly hot one, so it keeps the shipped statement:
	// the safe direction is never to range blind. A lookup id over 1,024
	// bytes routes by mass like any other id; the key-range store read runs
	// such a read as its shipped-statement fallback on the same snapshot.
	var hot []string
	for _, ids := range planned {
		if len(ids) == 1 {
			if mass, ok := masses[ids[0]]; ok && mass > fanOutHolderHotMassRows {
				hot = append(hot, ids[0])
			}
		}
	}
	rangeReader, canRange := h.FanOut.(HolderKeyRangeReader)
	ranges := HolderKeyRanges()
	routingMasses := map[string]int64{}
	if canRange && len(hot) > 0 {
		if routingEst, ok := h.FanOut.(HolderRoutingMassEstimator); ok {
			start := time.Now()
			resolved, err := routingEst.EstimateHolderRoutingMasses(ctx, hot)
			h.observeFanOutRead(ctx, telemetry.FanOutReadKindMassProbe, time.Since(start), err)
			if err != nil {
				h.countFanOutRead(ctx, cause, err, telemetry.FanOutReadSiteHolders)
				return nil, stats, fanOutReadFailure(err, "estimate supply chain impact holder routing mass")
			}
			routingMasses = resolved
			for _, id := range hot {
				if mass, ok := resolved[id]; ok && mass > stats.maxMass {
					stats.maxMass = mass
				}
			}
		}
	}
	statements := make([]holderStatement, 0, len(planned))
	for _, ids := range planned {
		stmt := holderStatement{ids: ids}
		if canRange && len(ids) == 1 {
			if mass, ok := routingMasses[ids[0]]; ok && mass > HolderKeyRangeMassGate {
				stmt.ranged = true
				stats.keyRangedIDs++
			}
		}
		if !stmt.ranged {
			stats.statements++
		}
		statements = append(statements, stmt)
	}
	if len(planned) > 1 {
		stats.splitChunks = 1
		for _, ids := range planned {
			if len(ids) == 1 {
				stats.hotIDs++
			}
		}
	}
	if len(statements) == 1 && !statements[0].ranged {
		found, err := h.findHolderScopes(ctx, ownScopeID, statements[0].ids)
		if err != nil {
			h.countFanOutRead(ctx, cause, err, telemetry.FanOutReadSiteHolders)
			return nil, stats, fanOutReadFailure(err, "find supply chain impact holders")
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
		go func(stmt holderStatement) {
			defer wg.Done()
			defer func() { <-sem }()
			var found []string
			var err error
			if stmt.ranged {
				var read HolderKeyRangeRead
				start := time.Now()
				read, err = rangeReader.FindHolderScopesByKeyRange(ctx, ownScopeID, stmt.ids, ranges)
				// The statement counts come from the read that ran: the
				// long-id fallback runs one shipped statement, not one
				// per range, and a failed read still lists the statements
				// it ran.
				mu.Lock()
				stats.keyRangeStatements += len(read.Statements)
				stats.statements += len(read.Statements)
				mu.Unlock()
				h.observeFanOutRead(ctx, telemetry.FanOutReadKindHoldersKeyRangeRead, time.Since(start), err)
				nonCVE := lookupHoldsNonCVEID(stmt.ids)
				for _, done := range read.Statements {
					h.observeFanOutRead(ctx, done.Kind, done.Duration, done.Err)
					h.noteKeyRangeStatement(ctx, done, len(ranges), nonCVE, read.Fallback)
				}
				found = read.Scopes
			} else {
				found, err = h.findHolderScopes(ctx, ownScopeID, stmt.ids)
			}
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
		return nil, stats, fanOutReadFailure(firstErr, "find supply chain impact holders")
	}
	return set, stats, nil
}

// findHolderScopes runs one shipped holder statement and records its
// duration under the holders read kind.
func (h SupplyChainImpactHandler) findHolderScopes(ctx context.Context, ownScopeID string, ids []string) ([]string, error) {
	start := time.Now()
	found, err := h.FanOut.FindHolderScopes(ctx, ownScopeID, ids)
	h.observeFanOutRead(ctx, telemetry.FanOutReadKindHolders, time.Since(start), err)
	return found, err
}
