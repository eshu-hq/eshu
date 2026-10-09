// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package evidence keeps the #3466 collector-readiness evidence summary
// read model reconciled with the active fact set.
//
// [Maintainer] runs the atomic, idempotent full resweep
// ([Rebuilder]) on startup and on a fixed cadence under a single-owner
// partition lease, so concurrent resweeps never contend on the table; the
// durable [FreshnessLookup] guard caps cluster-wide resweeps at ~one per
// cadence regardless of replica count. The lease is always released before
// RunOnce returns, so a crashed instance never blocks takeover beyond the
// lease TTL. See
// docs/internal/design/collector-readiness-evidence-summary-materialization-design.md.
package evidence
