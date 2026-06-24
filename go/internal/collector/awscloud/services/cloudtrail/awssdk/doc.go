// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK CloudTrail responses into the
// scanner-owned metadata records consumed by
// internal/collector/awscloud/services/cloudtrail.
//
// The adapter is the only place that talks to the AWS SDK. It is metadata
// only by construction: the apiClient interface does not expose
// `LookupEvents`, Lake query APIs, or any mutation API. Selector bodies and
// dashboard widget query bodies are reduced to bounded count summaries
// before they reach the scanner contract.
package awssdk
