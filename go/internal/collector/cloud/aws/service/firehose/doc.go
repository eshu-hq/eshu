// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package firehose maps Amazon Data Firehose delivery stream metadata into AWS
// cloud collector facts.
//
// The scanner emits one reported-confidence resource per delivery stream
// (name, ARN, status, stream type, source type, encryption mode, and creation
// time) plus relationship evidence for the stream's S3 bucket, Amazon Redshift
// cluster, and Amazon OpenSearch Service domain destinations, its Kinesis data
// stream source, its delivery IAM role, its server-side encryption KMS key, its
// CloudWatch Logs error-logging log group, and its data-transformation Lambda
// functions. Delivery records, destination access keys, Splunk HEC tokens,
// Redshift passwords, processing-configuration Lambda bodies, and mutation APIs
// stay outside this package contract.
package firehose
