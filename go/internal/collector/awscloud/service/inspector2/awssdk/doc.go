// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Inspector v2 client into the
// metadata-only Inspector v2 scanner interface.
//
// The adapter owns Inspector v2 pagination, the account status read, member,
// filter-name, and CIS scan configuration list reads, throttle classification,
// and per-call AWS API telemetry. It intentionally excludes finding-body reads,
// finding aggregations, code-snippet reads, SBOM exports, CIS scan-result
// reads, filter-criteria reads, and every mutation API. A reflection test over
// the internal apiClient interface enforces that exclusion.
package awssdk
