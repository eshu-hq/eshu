// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package sharedintent owns the shape of one durable shared-domain projection
// intent, its deterministic identity, and the freshness key read off it.
//
// [Row] is the intent as it is stored and drained. [Build] constructs one from
// [Input], deriving [Row.IntentID] through [StableIntentID] as a SHA256 over the
// identity fields — acceptance unit, generation, partition key, projection
// domain, repository, scope, and source run — serialized as compact JSON with
// sorted keys. That derivation is what makes an intent idempotent under retry:
// the same logical work always names the same row, so a redelivery updates
// rather than duplicates. It matches the original Python implementation exactly
// and must not be changed without auditing every in-flight intent already
// persisted under the old identity.
//
// [Row.AcceptanceKey] returns the bounded-unit freshness slice a row belongs to,
// falling back to the payload's scope_id and acceptance_unit_id when the columns
// are empty, and to the repository ID when no acceptance unit is named. It
// reports false when the row cannot name a slice at all, which callers treat as
// "not eligible for acceptance" rather than as an error.
//
// # Why this is a leaf
//
// Domain families build intents. Before this package existed, the shapes lived
// in the reducer root beside the worker, runner, lease, and batch-selection
// machinery, so a family that only wanted to construct a row had to import the
// root — and the root imports the families. That is an import cycle, and it is
// the single most common reason a family in issue #6061 cannot become a
// subpackage: three symbols from one file blocked 47 non-test files across
// roughly 23 domains.
//
// This package therefore holds only plain data and pure functions. It imports
// `payloadcore` for one string coercion and otherwise nothing but the standard
// library, and it must never import the reducer root. The worker, runner,
// readiness, lease-heartbeat and batch-selection machinery deliberately stay in
// the root: they are the reducer's concurrency core, not a shape a family needs.
//
// The root keeps aliases under the original names — SharedProjectionIntentRow,
// SharedProjectionIntentInput, SharedProjectionAcceptanceKey — and a forwarder
// for BuildSharedProjectionIntent, so no caller changed when this moved.
package sharedintent
