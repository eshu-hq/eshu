// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package observability holds the hosted-observability-source span name
// declarations for Eshu's Go data-plane telemetry contract: Grafana, Loki,
// Prometheus/Mimir, and Tempo source-collector spans. These used to live
// directly in root package telemetry as contract_grafana.go,
// contract_loki.go, contract_prometheus_mimir.go, and contract_tempo.go
// (issue #6777).
//
// Like its sibling contract package, this package holds only declarations:
// no func init(), no registration, and no import of root package telemetry
// or any other go/internal/* package. Root registration.go registers every
// span here into the ordered spanNames slice; root compat_observability.go
// re-exports every name as a root telemetry.* compat alias.
package observability
