// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package routing builds the incident-routing materialization reducer
// intent from one immutable scope generation: when PagerDuty incident.record
// or a registered incident-routing source fact is present, it asks the reducer
// to compare declared, applied, and live routing and write
// IncidentRoutingEvidence graph truth. Root projector assembly owns lookup
// construction and lifetime, invocation order, queue writes, retries, and
// telemetry.
package routing
