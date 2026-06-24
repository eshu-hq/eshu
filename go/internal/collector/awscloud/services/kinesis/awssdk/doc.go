// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 Kinesis calls into scanner-owned
// metadata for Kinesis Data Streams, Kinesis Data Firehose, and Kinesis Video
// Streams.
//
// The adapter exposes three narrow API interfaces (dataStreamsAPI, firehoseAPI,
// videoAPI) that admit only metadata-only reads: ListStreams,
// DescribeStreamSummary, ListTagsForStream for Data Streams;
// ListDeliveryStreams, DescribeDeliveryStream, ListTagsForDeliveryStream for
// Firehose; ListStreams and ListTagsForStream for Video Streams. It must never
// call any record-plane API (PutRecord, PutRecords, GetRecords,
// GetShardIterator), any media-plane API (GetMedia, PutMedia,
// GetMediaForFragmentList), or any mutation API. It must never persist the
// Firehose processing-configuration Lambda body, HTTP endpoint access key,
// Splunk HEC token, or Redshift password; only safe identity fields are
// mapped.
package awssdk
