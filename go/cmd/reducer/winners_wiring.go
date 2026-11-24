// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	pgaccess "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// supplyChainImpactWinnersAuditIntervalEnv sets how often the winners audit
// backstop recomputes the whole table (#9805).
const supplyChainImpactWinnersAuditIntervalEnv = "ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_INTERVAL"

// defaultSupplyChainImpactWinnersAuditInterval is the audit cadence: one full
// recompute read every ten minutes, outside the resweep lock.
const defaultSupplyChainImpactWinnersAuditInterval = 10 * time.Minute

// supplyChainImpactWinnersReadRoutingEnv is the reader routing mode the stale
// audit scan honors. stale-audits admits the split scan, staged-audits the
// staged scan; anything else keeps the writer-backed audit.
const supplyChainImpactWinnersReadRoutingEnv = "ESHU_POSTGRES_READ_ROUTING"

// winnersStagedAuditRouting reports whether routing selects the staged
// standby scan (#10451) instead of the split scan. The access gate already
// rejects bogus modes, so this only distinguishes the two admitted ones.
func winnersStagedAuditRouting(getenv func(string) string) bool {
	return strings.TrimSpace(getenv(supplyChainImpactWinnersReadRoutingEnv)) == string(pgaccess.ReadRoutingStagedAudits)
}

// openWinnersStaleAccessIfEnabled opens the Postgres access for the
// winners-audit stale scans when routing admits stale audits: the split scan
// (#10416 slice 1) or the staged scan (#10451). Default off (unset or "off")
// returns nil without touching the database, so the Auditor stays
// writer-backed exactly as before. Any other value fails startup: an unknown
// mode must never silently run as off. The access serves only the audit's
// candidate scan; every write and every other read keeps the reducer's own
// writer pool.
func openWinnersStaleAccessIfEnabled(
	ctx context.Context,
	getenv func(string) string,
	meter metric.Meter,
	tracer trace.Tracer,
	logger *slog.Logger,
) (access *pgaccess.Access, cleanup func(), err error) {
	noop := func() {}
	switch raw := strings.TrimSpace(getenv(supplyChainImpactWinnersReadRoutingEnv)); raw {
	case "", string(pgaccess.ReadRoutingOff):
		return nil, noop, nil
	case string(pgaccess.ReadRoutingStaleAudits), string(pgaccess.ReadRoutingStagedAudits):
	default:
		return nil, nil, fmt.Errorf("supply chain impact winners stale access: ESHU_POSTGRES_READ_ROUTING must be off, stale-audits, or staged-audits, got %q", raw)
	}
	poolCfg, err := pgaccess.LoadConfig(getenv)
	if err != nil {
		return nil, nil, fmt.Errorf("supply chain impact winners stale access: %w", err)
	}
	poolCfg.Logger = logger
	observer, err := pgaccess.NewObserver(meter, tracer)
	if err != nil {
		return nil, nil, fmt.Errorf("supply chain impact winners stale access: %w", err)
	}
	// The open retries a briefly unreachable reader with a bounded backoff
	// (#11306) instead of failing the startup fast: the wrapper already
	// carries the stale-access prefix on every error path, so its error
	// returns unwrapped to avoid doubling the prefix.
	access, err = openWinnersStaleAccessWithRetry(ctx, poolCfg, observer, pgaccess.Open, logger)
	if err != nil {
		return nil, nil, err
	}
	return access, func() {
		if err := access.Close(); err != nil && logger != nil {
			logger.Warn("winners stale access close failed", "error", err)
		}
	}, nil
}

// winnersStaleAccessOpenBackoff paces stale-reader open retries. It matches
// the shared Postgres ping retry (postgresPingBackoff in
// internal/runtime/data_stores.go): a single dial is fragile against a
// Postgres that passed pg_isready but is still finishing startup, and a retry
// a few hundred milliseconds later clears it.
const winnersStaleAccessOpenBackoff = 250 * time.Millisecond

// winnersStaleAccessOpenBudget bounds the deferred stale-reader open
// (#11306). A replica restart observed in the field needed about 40s before
// the reader was reachable again; the budget covers that with margin while
// still failing loudly when the reader stays unreachable past it.
const winnersStaleAccessOpenBudget = 5 * time.Minute

// winnersStaleAccessOpenFunc opens the Postgres reader access. pgaccess.Open
// is the production implementation; tests inject a stub that fails with the
// reader-unavailable sentinel before succeeding.
type winnersStaleAccessOpenFunc func(context.Context, pgaccess.Config, pgaccess.Observer) (*pgaccess.Access, error)

// openWinnersStaleAccessWithRetry opens the reader access, retrying while the
// failure reports a reader that is not yet reachable (errors.Is(err,
// db.ErrReaderUnavailable)) until ctx expires. Any other failure returns
// immediately: a dead writer, a bad config, or a wrong topology is not a
// transient reader outage and must keep failing fast. Past the bound the last
// error returns wrapped with the stale-access prefix, so the process still
// exits loudly on the same error instead of silently falling back to the
// writer. Every wait logs its attempt and reason, so the startup wait and
// its cause are visible while the status server reports not-ready.
func openWinnersStaleAccessWithRetry(
	ctx context.Context,
	cfg pgaccess.Config,
	observer pgaccess.Observer,
	open winnersStaleAccessOpenFunc,
	logger *slog.Logger,
) (*pgaccess.Access, error) {
	for attempt := 1; ; attempt++ {
		access, err := open(ctx, cfg, observer)
		if err == nil {
			return access, nil
		}
		if !errors.Is(err, db.ErrReaderUnavailable) {
			return nil, fmt.Errorf("supply chain impact winners stale access: %w", err)
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("supply chain impact winners stale access: %w", err)
		}
		if logger != nil {
			logger.Info("winners stale access waiting for reader",
				slog.Int("attempt", attempt),
				slog.Duration("retry_in", winnersStaleAccessOpenBackoff),
				slog.String("reason", pgaccess.OpenFailureSummary(err)),
			)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("supply chain impact winners stale access: %w", err)
		case <-time.After(winnersStaleAccessOpenBackoff):
		}
	}
}

// classifyWinnersStaleRouting maps the ESHU_POSTGRES_READ_ROUTING value to
// the stale-access path: off stays writer-backed, the two admitted modes
// wait for the reader, and anything else is bogus. An unknown mode must fail
// startup, never silently run as off.
func classifyWinnersStaleRouting(raw string) (admitted, known bool) {
	switch strings.TrimSpace(raw) {
	case "", string(pgaccess.ReadRoutingOff):
		return false, true
	case string(pgaccess.ReadRoutingStaleAudits), string(pgaccess.ReadRoutingStagedAudits):
		return true, true
	default:
		return false, false
	}
}

// winnersStaleAccessAdmitted reports whether routing admits the stale audit
// scans. Only the two admitted modes wait for the reader; anything else
// either stays writer-backed (off) or fails startup as bogus when opened.
func winnersStaleAccessAdmitted(getenv func(string) string) bool {
	admitted, _ := classifyWinnersStaleRouting(getenv(supplyChainImpactWinnersReadRoutingEnv))
	return admitted
}

// winnersStaleAccessRoutingError rejects a bogus routing mode at build time
// with the same error the open path reports: the deferred gate never sees
// the value, so an unknown mode cannot slip through as writer-backed.
func winnersStaleAccessRoutingError(getenv func(string) string) error {
	raw := strings.TrimSpace(getenv(supplyChainImpactWinnersReadRoutingEnv))
	if _, known := classifyWinnersStaleRouting(raw); !known {
		return fmt.Errorf("supply chain impact winners stale access: ESHU_POSTGRES_READ_ROUTING must be off, stale-audits, or staged-audits, got %q", raw)
	}
	return nil
}

// winnersRunner is the long-running behavior the stale-access gate wraps:
// the same Run contract the hosted application serves.
type winnersRunner interface {
	Run(context.Context) error
}

// winnersStaleAccessDeferred is the stale-reader open moved out of the
// buildObservedReducerService fail-fast path (#11306). The build returns
// quickly with the writer-backed Auditor it always built; the open, with its
// bounded retry, runs in the Runner below after the status server starts, so
// /readyz reports not-ready while the reducer waits for the reader instead
// of the process exiting before the status server listens. Past the bound
// the Runner returns the same wrapped error, so a persistently unreachable
// reader still exits loudly, never falling back to the primary.
type winnersStaleAccessDeferred struct {
	admitted bool
	ready    atomic.Bool
	open     func(ctx context.Context) (*pgaccess.Access, func(), error)
	attach   func(access *pgaccess.Access) error
}

// newWinnersStaleAccessDeferred builds the deferred stale-reader gate. When
// routing does not admit stale audits there is nothing to wait for, so the
// gate starts ready and the Runner passes straight through.
func newWinnersStaleAccessDeferred(
	admitted bool,
	open func(ctx context.Context) (*pgaccess.Access, func(), error),
	attach func(access *pgaccess.Access) error,
) *winnersStaleAccessDeferred {
	gate := &winnersStaleAccessDeferred{admitted: admitted, open: open, attach: attach}
	if !admitted {
		gate.ready.Store(true)
	}
	return gate
}

// readinessProbe reports the stale-reader gate to /readyz: not-ready while
// the deferred open is still waiting for the reader, ready once the access
// is attached (or when routing never admitted the stale scans).
func (gate *winnersStaleAccessDeferred) readinessProbe() runtimecfg.ReadinessProbe {
	return runtimecfg.ReadinessProbe{
		Name: "winners_stale_access",
		Check: func(context.Context) error {
			if !gate.admitted || gate.ready.Load() {
				return nil
			}
			return errors.New("winners stale reader access not yet established")
		},
	}
}

// wrapRunner returns a Runner that establishes the stale access before the
// inner service runs. Routing off passes straight through with no I/O; a
// nil access (only possible when off) skips the attach; any open or attach
// failure returns the loud wrapped error so the process exits instead of
// serving winners it cannot prove current.
func (gate *winnersStaleAccessDeferred) wrapRunner(inner winnersRunner, logger *slog.Logger) winnersRunner {
	return &winnersStaleAccessRunner{gate: gate, inner: inner, logger: logger}
}

// winnersStaleAccessRunner establishes the deferred stale access, then runs
// the inner service. The status server is already listening while it waits,
// so the wait surfaces as not-ready; the access closes when the inner run
// returns, matching the previous shutdown order.
type winnersStaleAccessRunner struct {
	gate   *winnersStaleAccessDeferred
	inner  winnersRunner
	logger *slog.Logger
}

// Run establishes the stale reader access with a bounded retry and then runs
// the inner service.
func (r *winnersStaleAccessRunner) Run(ctx context.Context) error {
	if !r.gate.admitted {
		return r.inner.Run(ctx)
	}
	openCtx, cancel := context.WithTimeout(ctx, winnersStaleAccessOpenBudget)
	defer cancel()
	access, cleanup, err := r.gate.open(openCtx)
	if err != nil {
		if ctx.Err() != nil {
			if r.logger != nil {
				r.logger.Info("winners stale access wait canceled by shutdown",
					slog.String("reason", pgaccess.OpenFailureSummary(err)),
				)
			}
			return nil
		}
		return err
	}
	defer cleanup()
	if access != nil {
		if err := r.gate.attach(access); err != nil {
			return err
		}
		if r.logger != nil {
			r.logger.Info("winners stale access established")
		}
	}
	r.gate.ready.Store(true)
	return r.inner.Run(ctx)
}

// loadSupplyChainImpactWinnersAuditInterval reads the audit cadence. An unset or
// unparsable value gives the default; "0" (or "0s") turns the audit off, which
// leaves the keyed resweep without its backstop, so it is an operator's explicit
// choice and never a default.
func loadSupplyChainImpactWinnersAuditInterval(getenv func(string) string) time.Duration {
	raw := strings.TrimSpace(getenv(supplyChainImpactWinnersAuditIntervalEnv))
	if raw == "" {
		return defaultSupplyChainImpactWinnersAuditInterval
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < 0 {
		return defaultSupplyChainImpactWinnersAuditInterval
	}
	return value
}

// supplyChainImpactWinnersAuditRangeBudgetEnv sets the fact-row budget of one
// staged audit sweep range (#11131).
const supplyChainImpactWinnersAuditRangeBudgetEnv = "ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_RANGE_BUDGET"

// supplyChainImpactWinnersAuditTickBudgetEnv bounds the wall time one audit
// tick spends running staged sweep ranges back to back (#11131).
const supplyChainImpactWinnersAuditTickBudgetEnv = "ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_TICK_BUDGET"

// loadSupplyChainImpactWinnersAuditRangeBudget reads the range budget. An
// unset, unparsable or non-positive value gives 0, which selects the default
// (postgres.DefaultWinnersAuditRangeBudget, 10,000 rows).
func loadSupplyChainImpactWinnersAuditRangeBudget(getenv func(string) string) int {
	value, err := strconv.Atoi(strings.TrimSpace(getenv(supplyChainImpactWinnersAuditRangeBudgetEnv)))
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

// loadSupplyChainImpactWinnersAuditTickBudget reads the audit tick budget. An
// unset, unparsable or non-positive value gives 0, which selects the
// maintainer default (5 minutes).
func loadSupplyChainImpactWinnersAuditTickBudget(getenv func(string) string) time.Duration {
	value, err := time.ParseDuration(strings.TrimSpace(getenv(supplyChainImpactWinnersAuditTickBudgetEnv)))
	if err != nil || value <= 0 {
		return 0
	}
	return value
}
