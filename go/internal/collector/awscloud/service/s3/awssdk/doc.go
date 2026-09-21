// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 S3 bucket metadata responses to the
// scanner-owned S3 client contract.
//
// The adapter owns S3 ListBuckets pagination, bucket control-plane point reads,
// optional-not-configured error handling, throttle classification, and AWS API
// telemetry. Bucket policy and replication configuration reads are transient:
// policy JSON is reduced to posture booleans plus bounded external-principal
// metadata, and replication configuration is reduced to presence. Object
// inventory calls, ACL grant reads, notification reads, replication rule detail,
// lifecycle reads, and mutation APIs stay outside this package contract.
package awssdk
