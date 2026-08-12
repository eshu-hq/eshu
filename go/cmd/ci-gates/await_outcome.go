// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "errors"

// Why this file exists (#6075).
//
// `required-gates-complete` is branch protection's summary of every other
// gate, and it was publishing `failure` for any non-success await outcome. So
// "a required gate went red", "the gates are still running", and "the
// aggregation itself broke" all landed on the head SHA as the same red
// status. Three observed consequences:
//
//   - A red carried no information. The correct reaction became "wait and look
//     again", which is how you treat a flake -- so a genuine aggregation
//     failure gets waved through.
//   - Automation broke. Any lander treating a red check as terminal abandons a
//     PR whose real gates are all green.
//   - Reds landed on already-merged PRs (one posted 21 minutes after merge),
//     so the merge record shows a red gate on a PR that merged clean.
//
// #6064 closed one instance (cancelled runs publishing failure). The
// default-to-failure branch stayed reachable for every other non-success
// outcome, which is what this classifies.
//
// The exit code is the interface: the workflow publisher branches on it, so
// these values are a contract with .github/workflows/required-gates.yml and
// cannot be renumbered without moving that step in the same commit.
const (
	// awaitExitGateFailed means a selected blocking gate concluded failure.
	// This is the ONLY outcome that may publish `failure`.
	awaitExitGateFailed = 1
	// awaitExitStillRunning means the wait ended while selected gates were
	// still pending -- a timeout, or a superseded run. Not a gate result.
	awaitExitStillRunning = 3
	// awaitExitBroken means aggregation could not reach a verdict at all
	// (API error, bad token, unreadable registry). A publisher problem.
	awaitExitBroken = 4
)

// awaitOutcome is what the await loop concluded, as opposed to how it exited.
type awaitOutcome int

const (
	awaitOutcomePassed awaitOutcome = iota
	awaitOutcomeGateFailed
	awaitOutcomeStillRunning
	awaitOutcomeBroken
)

func (o awaitOutcome) String() string {
	switch o {
	case awaitOutcomePassed:
		return "passed"
	case awaitOutcomeGateFailed:
		return "gate_failed"
	case awaitOutcomeStillRunning:
		return "still_running"
	case awaitOutcomeBroken:
		return "broken"
	}
	return "unknown"
}

// exitCode maps an outcome to the process exit code the workflow reads.
func (o awaitOutcome) exitCode() int {
	switch o {
	case awaitOutcomePassed:
		return 0
	case awaitOutcomeGateFailed:
		return awaitExitGateFailed
	case awaitOutcomeStillRunning:
		return awaitExitStillRunning
	case awaitOutcomeBroken:
		return awaitExitBroken
	}
	return awaitExitBroken
}

// errGateFailed and errStillRunning are wrapped by the await loop so the
// classification is structural rather than a match on message text. Text
// matching would reclassify silently the first time someone reworded an
// error, and the direction it would fail is the dangerous one: a genuine gate
// failure quietly demoted to "still running" stops blocking merges.
var (
	errGateFailed   = errors.New("selected blocking gate failed")
	errStillRunning = errors.New("selected blocking gates still running")
)

// classifyAwaitOutcome decides which of the three non-success meanings an
// await error carries. Anything unrecognized is `broken`, not `gate_failed`:
// an unclassified error means the publisher does not know what happened, and
// reporting "a gate failed" on that basis is the overclaim this fixes.
// Publishing `error` instead keeps it visible without asserting a gate result.
func classifyAwaitOutcome(err error) awaitOutcome {
	switch {
	case err == nil:
		return awaitOutcomePassed
	case errors.Is(err, errGateFailed):
		return awaitOutcomeGateFailed
	case errors.Is(err, errStillRunning):
		return awaitOutcomeStillRunning
	default:
		return awaitOutcomeBroken
	}
}
