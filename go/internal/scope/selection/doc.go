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
// whether a row is still inside the liveness window its writer stored
// (ESHU_REPO_SELECTION_LIVENESS_WINDOW); rows past it belong to a selector
// that stopped evaluating and decide nothing. Confirmed is the uniform
// confirmation rule: any state other than selected, held for at least two
// evaluations spanning at least ConfirmationMinSpan.
//
// Summarize aggregates a scope's live rows into a Summary: unknown with no
// live row, selected when any live row is selected, pending_confirmation
// while a live row is unconfirmed, and otherwise not_selected unless a
// generation of the scope was observed after the newest exclusion began, in
// which case excluded_still_ingested. Only not_selected changes a freshness
// verdict.
//
// The package is evidence only. Nothing here deletes, hides, or retires a
// scope. It depends only on the standard library so the collector, storage,
// and status packages can all import it without a cycle.
package selection
