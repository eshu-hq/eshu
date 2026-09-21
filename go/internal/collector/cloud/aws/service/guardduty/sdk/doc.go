// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 GuardDuty client into the
// metadata-only GuardDuty scanner interface.
//
// The adapter owns GuardDuty pagination, point metadata reads, aggregate
// finding statistics, throttle classification, and per-call AWS API telemetry.
// It intentionally excludes finding-body reads, filter detail reads, mutation
// APIs, and any S3 reads that would resolve threat intel set or IP set
// locations into list contents.
package awssdk
