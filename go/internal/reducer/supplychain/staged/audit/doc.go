// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package stagedaudit assembles one staged stale-winners audit tick
// (eshu-hq/eshu#10451): per-scope standby batches compacted client-side into
// per-key winners, diffed against the stored table, resolved to verification
// identifiers, and handed to the unchanged writer-side verification. It
// implements the reducer core's auditor interface, so the maintainer needs no
// changes: wiring constructs this auditor instead of the writer-backed store
// when staged routing is enabled, and the tick still defers with no writer
// contact whenever the standby refuses or a batch fails.
package audit
