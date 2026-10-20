// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kinesis

import (
	"context"
	"time"
)

// Client is the metadata-only Kinesis read surface consumed by Scanner. It
// spans Kinesis Data Streams, Kinesis Data Firehose, and Kinesis Video
// Streams. Runtime adapters translate AWS SDK responses into these
// scanner-owned types. The interface intentionally exposes no record-plane,
// media-plane, or mutation methods so those AWS APIs are unreachable from the
// scanner.
type Client interface {
	ListDataStreams(context.Context) ([]DataStream, error)
	ListFirehoseDeliveryStreams(context.Context) ([]FirehoseDeliveryStream, error)
	ListVideoStreams(context.Context) ([]VideoStream, error)
}

// DataStream is the scanner-owned representation of one Kinesis Data Streams
// stream. It carries inventory metadata only; stream records are never read.
type DataStream struct {
	ARN               string
	Name              string
	Status            string
	StreamMode        string
	OpenShardCount    int32
	RetentionHours    int32
	EncryptionType    string
	KMSKeyID          string
	CreationTimestamp time.Time
	Tags              map[string]string
}

// FirehoseDeliveryStream is the scanner-owned representation of one Kinesis
// Data Firehose delivery stream. The scanner records source, destination
// type, IAM role, transform Lambda, and encryption status. The processing
// configuration Lambda body, destination secret material (HTTP endpoint access
// key, Splunk HEC token, Redshift password), and secrets-manager configuration
// are intentionally outside this contract.
type FirehoseDeliveryStream struct {
	ARN                 string
	Name                string
	Status              string
	StreamType          string
	SourceKinesisStream string
	EncryptionStatus    string
	EncryptionKeyType   string
	EncryptionKMSKeyARN string
	CreationTimestamp   time.Time
	Destinations        []FirehoseDestination
	Tags                map[string]string
}

// FirehoseDestination is the scanner-owned representation of one Firehose
// delivery destination. Only the destination kind, the target identity needed
// for correlation, the service IAM role, and any transform Lambda ARN are
// recorded. Secret-bearing fields are never mapped into this type.
type FirehoseDestination struct {
	DestinationID       string
	Kind                string
	RoleARN             string
	TransformLambdaARNs []string
	S3BucketARN         string
	RedshiftClusterID   string
	OpenSearchDomainARN string
	SplunkEndpoint      string
	HTTPEndpointURL     string
	HTTPEndpointName    string
}

// VideoStream is the scanner-owned representation of one Kinesis Video Streams
// stream. It carries inventory metadata only; media fragments are never read.
type VideoStream struct {
	ARN               string
	Name              string
	Status            string
	KMSKeyID          string
	MediaType         string
	RetentionHours    int32
	CreationTimestamp time.Time
	Tags              map[string]string
}

// Firehose destination kind labels recorded on FirehoseDestination.Kind. They
// describe the AWS destination class without persisting destination payloads.
const (
	FirehoseDestinationKindS3           = "s3"
	FirehoseDestinationKindRedshift     = "redshift"
	FirehoseDestinationKindOpenSearch   = "opensearch"
	FirehoseDestinationKindSplunk       = "splunk"
	FirehoseDestinationKindHTTPEndpoint = "http_endpoint"
)
