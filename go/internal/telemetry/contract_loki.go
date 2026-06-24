// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

const (
	// SpanLokiObserve wraps one claimed Loki observation from workflow claim
	// through observability source fact envelope production.
	SpanLokiObserve = "loki.observe"
	// SpanLokiFetch wraps one bounded Loki REST metadata fetch.
	SpanLokiFetch = "loki.fetch"
)
