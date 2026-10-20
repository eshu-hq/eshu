// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Managed Service for Apache Flink
// (Kinesis Data Analytics v2) client into the metadata-only kinesisanalyticsv2
// scanner interface.
//
// The adapter uses ListApplications, DescribeApplication,
// ListApplicationSnapshots, and ListTagsForResource to read application
// control-plane metadata, snapshot names and status, and resource tags. It
// intentionally excludes every Create/Update/Delete/Start/Stop/Add/Rollback
// mutation API and never copies application code bodies, SQL text, environment
// property values, or run-configuration content, so the adapter cannot read
// record payloads or mutate application state.
package awssdk
