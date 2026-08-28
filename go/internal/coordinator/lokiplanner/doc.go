// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package lokiplanner plans workflow rows for configured Grafana Loki targets.
//
// WorkPlanner validates one enabled, claim-capable collector instance and
// returns deterministic workflow rows without resolving credentials or
// contacting Loki. The parent coordinator owns scheduling, tenant and egress
// filtering, durable admission, retries, and telemetry.
package lokiplanner
