// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

const (
	// SpanTempoObserve wraps one claimed Tempo metadata observation from
	// workflow claim through source fact envelope production.
	SpanTempoObserve = "tempo.observe"
	// SpanTempoFetch wraps one bounded Tempo metadata API fetch.
	SpanTempoFetch = "tempo.fetch"
)
