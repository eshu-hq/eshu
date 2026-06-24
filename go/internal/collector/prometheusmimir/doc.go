// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package prometheusmimir collects bounded live Prometheus and Mimir metadata
// as observability source facts.
//
// The package reads configured Prometheus-compatible API targets and emits
// metadata-only observability.source_instance, observability.observed_target,
// observability.observed_rule, and observability.coverage_warning facts. It
// never persists metric samples, exemplars, raw PromQL, scrape target URLs,
// target label values, tenant IDs, tenant secrets, or alert payload bodies.
// Provider failures expose bounded workflow failure classes while omitting
// provider response bodies and credential-bearing request details. Reducers and
// query surfaces own declared/applied/observed comparison and user-facing
// observability truth.
package prometheusmimir
