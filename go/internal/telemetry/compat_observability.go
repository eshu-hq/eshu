// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "github.com/eshu-hq/eshu/go/internal/telemetry/contract/observability"

// Compat surface for go/internal/telemetry/contract/observability (issue
// #6777). The hosted observability-source span declarations that used to live
// directly in this package as contract_grafana.go, contract_loki.go,
// contract_prometheus_mimir.go, and contract_tempo.go now live in the
// observability subpackage; every name below re-exports one of them as a root
// telemetry.* identifier so every existing caller keeps compiling unchanged.
// See the referenced observability.* symbol for the canonical doc comment.

// Grafana source-collector spans (from contract/observability/grafana.go).
const (
	SpanGrafanaObserve = observability.SpanGrafanaObserve
	SpanGrafanaFetch   = observability.SpanGrafanaFetch
)

// Loki source-collector spans (from contract/observability/loki.go).
const (
	SpanLokiObserve = observability.SpanLokiObserve
	SpanLokiFetch   = observability.SpanLokiFetch
)

// Prometheus/Mimir source-collector spans (from
// contract/observability/prometheus_mimir.go).
const (
	SpanPrometheusMimirObserve = observability.SpanPrometheusMimirObserve
	SpanPrometheusMimirFetch   = observability.SpanPrometheusMimirFetch
)

// Tempo source-collector spans (from contract/observability/tempo.go).
const (
	SpanTempoObserve = observability.SpanTempoObserve
	SpanTempoFetch   = observability.SpanTempoFetch
)
