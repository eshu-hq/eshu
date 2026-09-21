// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Security Lake client into the
// scanner-owned metadata model.
//
// The adapter reads only control-plane list operations (ListDataLakes,
// ListLogSources, ListSubscribers), pages every list to exhaustion, wraps each
// call in the shared AWS pagination span plus API-call/throttle counters, and
// maps SDK types into the metadata-only securitylake domain types. It never
// reads ingested security log records, object contents, subscriber credentials
// (external id, endpoint), and never calls a mutation API; the apiClient
// interface and the exclusion test enforce that contract by construction.
package awssdk
