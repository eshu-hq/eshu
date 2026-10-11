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
	// DefaultBudget is the work budget when EnvBudgetMS is unset. It keeps the
	// common case under the one-second end-to-end bar; a cancelled statement
	// can still overrun its own timeout by one candidate's recheck, and the
	// result reports that overrun.
	DefaultBudget = 800 * time.Millisecond
	// MinBudget is the smallest accepted budget. Below it the tail floor and
	// the phase rooms shrink under the cost of one round trip.
	MinBudget = 100 * time.Millisecond
	// MaxBudget is the largest accepted budget. The read replica cancels a
	// transaction that conflicts with recovery after about 30 s, so a search
	// must stay far under it.
	MaxBudget = 10 * time.Second

	// probeRows is the size of the first key-ordered window.
	probeRows = 200
	// stepRows is the size of each continuation window.
	stepRows = 500
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

// plan holds the phase limits derived from one budget. The ruling's figures
// are stated at the 800 ms default; each scales linearly with the budget.
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
	phaseOneCap time.Duration // continuation before the tail: 0.55 x budget
	tailCap     time.Duration // largest tail timeout: 0.5 x budget
	tailFloor   time.Duration // smallest useful tail timeout: 150 ms at 800 ms
}

func newPlan(budget time.Duration) plan {
	scale := func(atDefault time.Duration) time.Duration {
		return time.Duration(int64(atDefault) * int64(budget) / int64(DefaultBudget))
	}
	return plan{
		budget: budget,
		// 0.55 x budget (#7730 continuation shim of 2026-10-11,
		// 7730-continuation-shim-20261011.md, on the QA environment read of
		// 2026-10-11): one key-ordered window to the 11th match of the common
		// tokens that ended partial at 0.15 costs 289-409 ms, so 440 ms reaches
		// them, and the 360 ms left for the tail still covers the measured
		// 268 ms zero-match and 300 ms selective tails. Caps 0.50 to 0.60 pass
		// the model's accuracy rules; 0.65 turns the selective class partial.
		phaseOneCap: scale(440 * time.Millisecond),
		tailCap:     scale(400 * time.Millisecond),
		tailFloor:   scale(150 * time.Millisecond),
	}
}
