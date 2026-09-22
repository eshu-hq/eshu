// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package generation holds the scope-generation lifecycle family of the
// status report: recent lifecycle transitions and the bounded drilldown page
// behind the freshness generation query surface. The root internal/status
// package aggregates this package into RawSnapshot and Report, so the root
// imports generation; this package must never import the root or a sibling
// leaf.
//
// TransitionSnapshot captures one recent scope-generation lifecycle row
// straight from the status store: scope, generation, status, trigger kind,
// freshness hint, and the observed/activated/superseded timestamps.
// CloneTransitions returns a defensive copy; TransitionsText and
// TransitionsJSON render the slice as operator text or wire JSON.
//
// LifecycleFilter bounds a generation lifecycle drilldown to a scope,
// repository, collector, source system, generation, or status; Normalize
// trims selectors and clamps Limit into [1, MaxLifecycleLimit], defaulting
// to DefaultLifecycleLimit. HasScopeSelector reports whether the filter
// names a specific scope, repository, or generation, which the drilldown
// handler uses to distinguish an explicit not-found from a confident empty
// broad scan.
//
// LifecycleRecord is one scope-generation lifecycle row joined with its
// scope identity, current-active-generation pointer, and the per-generation
// QueueStatus and LatestFailure. LifecyclePage is one bounded, ordered
// drilldown page; Truncated is true when more rows matched the filter than
// the requested Limit. LifecycleTimestamp formats a database timestamp as
// RFC3339 UTC, or empty for a zero value, and is the shared timestamp shape
// this contract and the changedsince family's own timestamp helper promise
// to keep in lockstep.
package generation
