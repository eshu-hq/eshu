// Package awssdk adapts AWS SDK for Go v2 S3 bucket metadata responses to the
// scanner-owned S3 client contract.
//
// The adapter owns S3 ListBuckets pagination, bucket control-plane point reads,
// optional-not-configured error handling, throttle classification, and AWS API
// telemetry. Object inventory calls, bucket policy JSON reads, ACL grant reads,
// notification reads, replication reads, lifecycle reads, and mutation APIs stay
// outside this package contract.
package awssdk
