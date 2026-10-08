// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package selection holds the one definition of what a stored repository
// selection observation means (#7625). The git collector's membership
// package writes the observations and the repository freshness reader turns
// them into the not_selected verdict; both use this package so the writer's
// gauge and the reader's verdict cannot disagree.
//
// State is the persisted per-selector state (selected, archived_excluded,
// rule_excluded, not_listed). Observation is one stored row. Live reports
// whether a row was evaluated within LiveIntervals of its own evaluation
// interval; rows that are not live belong to a selector that stopped
// evaluating and decide nothing. Confirmed is the two-cycle rule for
// not_listed: at least two unlisted cycles spanning at least the evaluation
// interval. Excluded is settled exclusion: archived or rule excluded, which
// apply immediately, or a confirmed not_listed.
//
// Summarize aggregates a scope's live rows into a Summary: selected when any
// live row is selected, not_selected when every live row is Excluded, and
// pending otherwise. It reports false when no live row exists, and callers
// must then leave their decision unchanged.
//
// The package is evidence only. Nothing here deletes, hides, or retires a
// scope. It depends only on the standard library so the collector, storage,
// and status packages can all import it without a cycle.
package selection
