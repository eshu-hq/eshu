// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package grafana collects bounded live Grafana metadata as observability
// source facts.
//
// The package reads configured Grafana API targets and emits metadata-only
// observability.source_instance, observability.observed_dashboard,
// observability.observed_rule, and observability.coverage_warning facts. It
// never persists dashboard JSON, datasource URLs, query models, contact
// destinations, credentials, or notification routes. Reducers and query
// surfaces own declared/applied/observed comparison and any user-facing
// observability truth.
package grafana
