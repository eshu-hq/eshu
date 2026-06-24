// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cloudtrail maps AWS CloudTrail configuration into AWS cloud
// collector facts.
//
// CloudTrail is the audit-config service: the scanner emits trail and Lake
// configuration only. Audit event payloads themselves are the protected data
// class for this service, so the scanner never calls event-extraction APIs
// (`LookupEvents`) and never persists event records. Lake query APIs
// (`StartQuery`, `GetQueryResults`) and all mutation APIs (CreateTrail,
// UpdateTrail, DeleteTrail, StartLogging, StopLogging, PutEventSelectors,
// PutInsightSelectors, Create/Update/Delete EventDataStore/Channel/Dashboard)
// are also outside the scanner contract; the Client interface in this package
// excludes them by construction so a maintainer adding them must also break a
// guard test.
//
// The package owns scanner-level fact selection for trails, Lake event data
// stores, channels, and Lake dashboard configurations, plus reported
// trail-to-S3-bucket, trail-to-CloudWatch-Logs-group, trail-to-KMS-key,
// trail-to-SNS-topic, and event-data-store-to-KMS-key relationships. Event
// selector and insight selector evidence is persisted as bounded summaries
// (selector counts and resource-type counts) rather than selector bodies.
package cloudtrail
