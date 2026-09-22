// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package queue holds the work-queue coordination family of the status
// report: conflict-domain blockage diagnostics and the newest queued-work
// failure. The root internal/status package aggregates this package into
// RawSnapshot and Report, so the root imports queue; this package must
// never import the root or a sibling leaf.
//
// Blockage captures eligible work that could not be claimed because a
// durable coordination gate is protecting the same conflict domain: stage,
// domain, conflict domain/key, how many rows are blocked, and the oldest
// blocked age. CloneBlockages normalizes and orders rows biggest-and-oldest
// first, the same priority order the operator report renders, and drops any
// row with a blank Stage. RenderBlockageLines formats them for the
// plain-text status surface without promoting the high-cardinality
// ConflictKey into a metric label.
//
// FailureSnapshot captures the newest queued-work failure metadata shown on
// operator status surfaces: stage, domain, status, the failing work item and
// scope, failure class, and a bounded failure message/details pair.
// CloneFailure returns a defensive copy; FailureText renders it as one
// bounded operator line, truncating FailureMessage and FailureDetails past
// 240 characters each so a single oversized failure cannot dominate the
// status output. FailureSnapshot values are rendered only in status
// payloads and must never be promoted to metric labels.
package queue
