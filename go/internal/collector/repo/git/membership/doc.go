// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package membership decides, once per githubOrg collector cycle, which known
// repository scopes are still members of the org listing the collector
// selects from, and records that decision as repository selection
// observations (#7625). In explicit mode it records only selected rows, one
// per configured repository that already has a scope.
//
// Evaluate is pure: it compares one complete listing against the org's known
// repository scopes and the selector's prior observations, and returns the
// rows to upsert plus the projected per-scope state. Observer wraps Evaluate
// with the Store reads and the single upsert, the outcome counter, the state
// gauge, and the structured logs.
//
// The package only records evidence. It never deletes, hides, or retires a
// scope and never writes the graph. At most once per cycle, on the request marked
// SweepExpired and after any outcome but a store error, Observer asks
// the Store to delete observation rows that stayed expired for
// ExpiredObservationGrace past their own liveness window (#7774). The Store
// never deletes not_listed rows, because the mass-miss guard reads them.
//
// Three rails keep a bad cycle from writing a false not_listed: a truncated
// listing writes nothing, a mass miss (more newly unlisted scopes than
// max(10, 10% of known), or an empty listing with known scopes) trips the
// guard and writes nothing, and a non-selected state
// only counts as Confirmed after at least two cycles in that state spanning
// selection.ConfirmationMinSpan.
//
// This package is a leaf below the git collector: git imports it, it must not
// import git.
package membership
