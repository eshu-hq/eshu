// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"time"
)

// supplyChainImpactWinnersAuditIntervalEnv sets how often the winners audit
// backstop recomputes the whole table (#9805).
const supplyChainImpactWinnersAuditIntervalEnv = "ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_AUDIT_INTERVAL"

// defaultSupplyChainImpactWinnersAuditInterval is the audit cadence: one full
// recompute read every ten minutes, outside the resweep lock.
const defaultSupplyChainImpactWinnersAuditInterval = 10 * time.Minute

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
