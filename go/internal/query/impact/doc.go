// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package impact holds the impact-analysis handler family (Issue #6060,
// lane B): the Handler HTTP surface plus every file that declares one
// of its methods, the pre-change check types, and the exported seam the
// staying root package consumes through aliases. Non-method helpers that the
// family needs but do not touch handler state live in deployment.
//
// A failed backend read answers a fixed message per route step and records
// the error on the request span; the backend error text never reaches the
// response body (#7674). A client cancel answers 499 and a stale PostgreSQL
// reader answers a retryable 503.
package impact
