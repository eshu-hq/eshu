// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package reduceradmission gates reducer intent enqueue behind reducer queue
// depth so a fast producer slows itself instead of piling recoverable work
// into an already-overloaded reducer queue or a timing-out graph backend.
//
// WrapIntentWriter wraps a projector.ReducerIntentWriter with two independent
// gates, checked in this order on every Enqueue call:
//
//  1. Graph-write-pressure gate: defers admission when the count of retrying
//     reducer rows whose failure_class is graph_write_timeout crosses
//     RetryingHighWaterMark, and releases only once that count falls below
//     RetryingLowWaterMark (hysteresis). This is the leading indicator that
//     the graph backend itself is timing out (issue #3560); it is scoped to
//     the graph_write_timeout failure class specifically so a backlog of
//     readiness-not-ready retrying rows (secrets_iam_endpoint_not_ready and
//     other *_n classes) never false-throttles unrelated admission.
//  2. Total-depth gate: defers admission once total outstanding reducer queue
//     depth (all statuses) reaches HighWaterMark. This is the trailing
//     safeguard for a queue that is simply deep, independent of failure
//     class.
//
// Both the ingester and bootstrap-index producers call WrapIntentWriter so
// admission backpressure behavior is identical across both binaries (parity
// requirement, issue #4515). The ingester additionally bypasses this gate
// entirely in its local-lightweight profile; that bypass is an
// ingester-local concern and lives in cmd/ingester, not here.
package reduceradmission
