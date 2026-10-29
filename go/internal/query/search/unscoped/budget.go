// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// EnvBudgetMS names the environment variable that sets the work budget of one
// unscoped file search, in milliseconds.
const EnvBudgetMS = "ESHU_CONTENT_SEARCH_BUDGET_MS"

const (
	// DefaultBudget is the work budget when EnvBudgetMS is unset. It is the
	// smallest measured budget where a selective unscoped query completes on
	// the QA corpus (#9306 ruling 2026-10-29: rare complete p95 1921 ms within
	// the 2500 ms gate; 800 ms left rare 21/21 partial at ~8.3% coverage). A
	// cancelled statement can still overrun its own timeout by one candidate's
	// recheck, and the result reports that overrun.
	DefaultBudget = 2000 * time.Millisecond
	// MinBudget is the smallest accepted budget. Below it the tail floor and
	// the phase rooms shrink under the cost of one round trip.
	MinBudget = 100 * time.Millisecond
	// MaxBudget is the largest accepted budget. The read replica cancels a
	// transaction that conflicts with recovery after about 30 s, so a search
	// must stay far under it.
	MaxBudget = 10 * time.Second
)

// BudgetFromEnv reads EnvBudgetMS through getenv. An unset or blank value
// yields DefaultBudget; a value that is not an integer number of milliseconds
// inside [MinBudget, MaxBudget] is an error so a typo fails startup instead
// of silently running unbounded or starved.
func BudgetFromEnv(getenv func(string) string) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(EnvBudgetMS))
	if raw == "" {
		return DefaultBudget, nil
	}
	millis, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s=%q: want an integer number of milliseconds: %w", EnvBudgetMS, raw, err)
	}
	budget := time.Duration(millis) * time.Millisecond
	if budget < MinBudget || budget > MaxBudget {
		return 0, fmt.Errorf("%s=%d: want %d..%d milliseconds", EnvBudgetMS, millis, MinBudget.Milliseconds(), MaxBudget.Milliseconds())
	}
	return budget, nil
}

// figures are one table shape's window sizes and phase limits, the phase
// limits stated at DefaultBudget. Each shape carries its own figures because
// a table's per-row cost and match density set them (#7009 ruling, 1.4); a
// figure needs a measurement, cited where the shape defines it.
type figures struct {
	probeRows   int           // size of the first key-ordered window
	stepRows    int           // size of each continuation window
	phaseOneCap time.Duration // continuation before the tail, at DefaultBudget
	tailCap     time.Duration // largest tail timeout, at DefaultBudget
	tailFloor   time.Duration // smallest useful tail timeout, at DefaultBudget
	// extensionRows caps the density-gated extension past phase one
	// (#8002): how many further ordered rows the walk may read when the
	// last window predicts the page fills inside them. Zero disables the
	// extension; the file route keeps it at zero.
	extensionRows int
}

// plan holds the phase limits derived from one budget and one shape's
// figures. Each limit scales linearly with the budget.
//
// The key-ordered windows have no fixed timeout of their own: the probe's is
// the remaining budget and a continuation step's is the room left in its
// phase. One document's recheck cannot be interrupted, so a fixed timeout
// below a window's cost cancels the window only after its work is done, and a
// resume chain then meets the same window again (#7730, decision 3.2). With
// the room as the timeout a window completes whenever the phase can pay for
// it; only a window that costs more than the whole budget still stalls.
type plan struct {
	budget      time.Duration
	probeRows   int
	stepRows    int
	phaseOneCap time.Duration // continuation before the tail
	tailCap     time.Duration // largest tail timeout
	tailFloor   time.Duration // smallest useful tail timeout
	// extensionRows is the figure's row allowance, unscaled like the
	// window sizes: the continuation stops at the remaining budget anyway.
	extensionRows int
}

func newPlan(budget time.Duration, f figures) plan {
	scale := func(atDefault time.Duration) time.Duration {
		return time.Duration(int64(atDefault) * int64(budget) / int64(DefaultBudget))
	}
	return plan{
		budget:        budget,
		probeRows:     f.probeRows,
		stepRows:      f.stepRows,
		phaseOneCap:   scale(f.phaseOneCap),
		tailCap:       scale(f.tailCap),
		tailFloor:     scale(f.tailFloor),
		extensionRows: f.extensionRows,
	}
}
