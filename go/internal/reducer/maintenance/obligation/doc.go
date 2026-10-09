// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package obligation settles the exact-generation activation obligations
// ProjectorQueue.Ack writes (#7584).
//
// [Runner] workers claim one obligation at a time through [Store], call the
// [Maintainer] port only when the generation's own backward-evidence phase
// is missing, and finalize; one worker per process also runs a bounded
// catch-up page, a bounded prune and the census gauges each cycle. The
// production maintainer is postgres.ActivationMaintainer, the
// partition-scoped pass on the obligation's own (scope, generation), wired
// by cmd/reducer when ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED is true;
// whole-corpus maintenance is a test control arm only. A maintainer
// answers [ErrInapplicable] when no repository maps to the owed partition
// (the row retires inapplicable) or a [HoldError] built by [Hold]
// (catalog_changed, no_memo_baseline, closure_too_deep): held at lease
// cadence, counted under its reason, no fallback pass.
package obligation
