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
// scope and never writes the graph. Three rails keep a bad cycle from writing
// a false not_listed: a truncated listing writes nothing, a mass miss (more
// newly unlisted scopes than max(10, 10% of known), or an empty listing with
// known scopes) trips the guard and writes nothing, and a not_listed scope is
// only Confirmed after two consecutive unlisted cycles spanning at least the
// evaluation interval.
//
// This package is a leaf below the git collector: git imports it, it must not
// import git.
package membership
