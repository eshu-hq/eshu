// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	pgaccess "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
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
// audit scan honors. Only stale-audits admits it; anything else keeps the
// writer-backed audit.
const supplyChainImpactWinnersReadRoutingEnv = "ESHU_POSTGRES_READ_ROUTING"

// openWinnersStaleAccessIfEnabled opens the Postgres access for the
// winners-audit split scan (#10416 slice 1) when routing admits stale audits.
// Default off (unset or "off") returns nil without touching the database, so
// the Auditor stays writer-backed exactly as before. Any other value fails
// startup: an unknown mode must never silently run as off. The access serves
// only the audit's candidate scan through StaleReader; every write and every
// other read keeps the reducer's own writer pool.
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
	case string(pgaccess.ReadRoutingStaleAudits):
	default:
		return nil, nil, fmt.Errorf("supply chain impact winners stale access: ESHU_POSTGRES_READ_ROUTING must be off or stale-audits, got %q", raw)
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
	access, err = pgaccess.Open(ctx, poolCfg, observer)
	if err != nil {
		return nil, nil, fmt.Errorf("supply chain impact winners stale access: %w", err)
	}
	return access, func() {
		if err := access.Close(); err != nil && logger != nil {
			logger.Warn("winners stale access close failed", "error", err)
		}
	}, nil
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
