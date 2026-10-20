// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package kinesis maps Amazon Kinesis metadata into AWS cloud collector facts.
//
// The package covers three Kinesis sub-services under one service_kind:
// Kinesis Data Streams, Kinesis Data Firehose, and Kinesis Video Streams. It
// owns scanner-level normalization only. It never calls the AWS SDK directly,
// never reads stream records, never reads video media fragments, never mutates
// any resource, and never persists Firehose processing Lambda bodies or
// destination secret material (HTTP endpoint access keys, Splunk HEC tokens,
// Redshift passwords).
//
// SDK adapters provide DataStream, FirehoseDeliveryStream, and VideoStream
// values through the Client interface, and Scanner emits aws_resource facts
// plus relationship evidence for stream-to-KMS-key, Firehose-to-destination,
// Firehose-to-Lambda-transform, and Firehose-to-IAM-role dependencies.
package kinesis
