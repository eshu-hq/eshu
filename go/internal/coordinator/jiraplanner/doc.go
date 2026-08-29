// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package jiraplanner validates Jira planning requests and builds deterministic
// workflow rows for selected configured targets.
//
// The planner preserves configured work-item order, emits privacy-safe sorted
// requested-scope metadata, and accepts scheduled, bootstrap, and webhook
// triggers. HasConfiguredScope exposes validated target membership for the root
// coordinator's incident-freshness authorization policy. Membership is only
// one input to that root-owned decision. Callers retain responsibility for
// clocks, scheduling, egress and tenant gates, durable admission, retries,
// queue and lease behavior, and telemetry.
package jiraplanner
