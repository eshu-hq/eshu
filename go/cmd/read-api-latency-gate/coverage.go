// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "fmt"

// ExercisedCoverageFloor is the minimum number of routes this gate must
// exercise (see RouteLatency.Exercised) for a run to be considered valid
// coverage of the read surface. It is a ratchet: raise it when
// RouteQueryArgs or the auth setup lets a previously not-exercised route run
// for real, but never lower it to make a coverage regression pass quietly.
// Which routes are exercised is deterministic (it depends on RouteQueryArgs
// and each route's own auth/selector requirements, not on measured
// latency), so this is pinned to the exact measured count — 65/113 across
// every live Compose run in this issue's drive (2026-09-18) — rather than
// a value with slack: slack here would let one
// named route silently drop out of coverage without moving the floor.
// RequireNamedRoutesExercised is the complementary, stricter check for any
// route this gate explicitly budgets.
const ExercisedCoverageFloor = 65

// ExercisedCoverage returns how many of results were exercised (see
// RouteLatency.Exercised) versus the total route count.
func ExercisedCoverage(results []RouteLatency) (exercised, total int) {
	for _, r := range results {
		if r.Exercised {
			exercised++
		}
	}
	return exercised, len(results)
}

// CheckCoverageFloor fails when results' exercised count drops below floor,
// so a future change (a route that starts requiring a new selector, an auth
// regression that starts rejecting the gate's key) cannot silently shrink
// how much of the read surface this gate actually measures.
func CheckCoverageFloor(results []RouteLatency, floor int) error {
	exercised, total := ExercisedCoverage(results)
	if exercised < floor {
		return fmt.Errorf("exercised %d/%d routes, below the committed coverage floor of %d — see ExercisedCoverageFloor", exercised, total, floor)
	}
	return nil
}
