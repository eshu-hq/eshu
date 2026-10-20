// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Config client into the
// metadata-only AWS Config scanner interface.
//
// The adapter owns Config pagination, the configuration recorder and delivery
// channel describes, the config rule describe, the conformance pack describe
// (joined with deployment status and member-rule names from aggregate
// compliance), the configuration aggregator describe, the retention
// configuration describe, throttle classification, and per-call AWS API
// telemetry. It intentionally excludes recorded configuration-item reads
// (GetResourceConfigHistory, BatchGetResourceConfig, GetDiscoveredResourceCounts),
// per-resource compliance-detail reads, custom-rule policy-body reads, stored
// query reads, and every mutation API. A reflection test over the internal
// apiClient interface enforces that exclusion.
package awssdk
