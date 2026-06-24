// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 CloudWatch Synthetics client into
// the metadata-only Synthetics scanner interface.
//
// The adapter uses DescribeCanaries to read canary control-plane metadata and
// the inline resource tags it returns. It intentionally excludes GetCanaryRuns,
// DescribeCanariesLastRun, the GetCanary code read, and every Create/Update/
// Delete/Start/Stop mutation and run-control API, so the adapter cannot read run
// artifacts, run results, or canary script source code, and cannot mutate
// Synthetics state.
package awssdk
