// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package tempoplanner plans workflow rows for configured Grafana Tempo
// trace-signal targets.
//
// WorkPlanner validates one enabled, claim-capable collector instance and
// returns deterministic workflow rows without resolving credentials or
// contacting Tempo. The parent coordinator owns scheduling, tenant and egress
// filtering, durable admission, retries, and telemetry.
package tempoplanner
