// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package kinesisanalyticsv2 maps Amazon Managed Service for Apache Flink
// (Kinesis Data Analytics v2) application metadata into AWS cloud collector
// facts.
//
// The scanner emits reported-confidence resources for Managed Flink
// applications plus relationships for the application's SQL input/output Kinesis
// data streams and Firehose delivery streams, its S3 code bucket, its VPC
// subnets and security groups, its service execution IAM role, and its
// CloudWatch logging log groups. Application code bodies, SQL text, environment
// property values, run-configuration content, record payloads, and any mutation
// API stay outside this package contract: the scanner is metadata-only.
package kinesisanalyticsv2
