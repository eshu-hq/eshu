// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Firehose client into the
// metadata-only Firehose scanner interface.
//
// The adapter uses ListDeliveryStreams and DescribeDeliveryStream to read
// delivery stream identity, status, source, encryption, and destination
// metadata. It intentionally excludes PutRecord, PutRecordBatch,
// CreateDeliveryStream, DeleteDeliveryStream, UpdateDestination,
// StartDeliveryStreamEncryption, StopDeliveryStreamEncryption,
// TagDeliveryStream, UntagDeliveryStream, and every other mutation or
// record-payload API, so they are unreachable through this adapter by
// construction. Destination access keys, Splunk HEC tokens, Redshift passwords,
// and processing-configuration Lambda bodies are never mapped.
package awssdk
