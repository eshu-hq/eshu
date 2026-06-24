// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

const (
	// SpanGrafanaObserve wraps one claimed Grafana observation from workflow
	// claim through observability source fact envelope production.
	SpanGrafanaObserve = "grafana.observe"
	// SpanGrafanaFetch wraps one bounded Grafana REST metadata fetch.
	SpanGrafanaFetch = "grafana.fetch"
)
