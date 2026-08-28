// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package prometheusmimir plans workflow rows for configured Prometheus and
// Grafana Mimir metric-metadata targets.
//
// WorkPlanner validates one enabled, claim-capable collector instance and
// returns deterministic workflow rows without resolving credentials or
// contacting a provider. The parent coordinator owns scheduling, tenant and
// egress filtering, durable admission, retries, and telemetry.
package prometheusmimir
