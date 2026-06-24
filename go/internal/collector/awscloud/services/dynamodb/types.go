// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dynamodb

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only DynamoDB table observations for one AWS claim.
type Client interface {
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures DynamoDB table metadata plus non-fatal scan warnings.
type Snapshot struct {
	Tables   []Table
	Warnings []awscloud.WarningObservation
}

// Table is the scanner-owned DynamoDB table model. It contains control-plane
// metadata only and intentionally excludes item values, item reads, stream
// records, exports, backup payloads, policies, PartiQL output, and mutations.
type Table struct {
	ARN                       string
	Name                      string
	ID                        string
	Status                    string
	CreationTime              time.Time
	BillingMode               string
	TableClass                string
	ItemCount                 int64
	TableSizeBytes            int64
	DeletionProtectionEnabled bool
	KeySchema                 []KeySchemaElement
	AttributeDefinitions      []AttributeDefinition
	ProvisionedThroughput     Throughput
	OnDemandThroughput        OnDemandThroughput
	SSE                       SSE
	TTL                       TTL
	ContinuousBackups         ContinuousBackups
	Stream                    Stream
	GlobalSecondaryIndexes    []SecondaryIndex
	LocalSecondaryIndexes     []SecondaryIndex
	Replicas                  []Replica
	Tags                      map[string]string
}

// KeySchemaElement is DynamoDB key-schema metadata for a table or index.
type KeySchemaElement struct {
	AttributeName string
	KeyType       string
}

// AttributeDefinition describes a DynamoDB key attribute name and scalar type.
type AttributeDefinition struct {
	AttributeName string
	AttributeType string
}

// Throughput describes provisioned DynamoDB read/write capacity metadata.
type Throughput struct {
	ReadCapacityUnits      int64
	WriteCapacityUnits     int64
	NumberOfDecreasesToday int64
}

// OnDemandThroughput describes optional on-demand throughput caps.
type OnDemandThroughput struct {
	MaxReadRequestUnits  int64
	MaxWriteRequestUnits int64
}

// SSE describes DynamoDB server-side encryption metadata.
type SSE struct {
	Status          string
	Type            string
	KMSMasterKeyARN string
}

// TTL describes DynamoDB Time to Live metadata.
type TTL struct {
	Status        string
	AttributeName string
}

// ContinuousBackups describes backup and point-in-time recovery metadata.
type ContinuousBackups struct {
	Status                    string
	PointInTimeRecoveryStatus string
	RecoveryPeriodInDays      int32
}

// Stream describes DynamoDB Streams metadata without reading stream records.
type Stream struct {
	Enabled         bool
	ViewType        string
	LatestStreamARN string
	LatestLabel     string
}

// SecondaryIndex describes local or global secondary index metadata.
type SecondaryIndex struct {
	Name                  string
	ARN                   string
	Status                string
	ItemCount             int64
	SizeBytes             int64
	Backfilling           bool
	KeySchema             []KeySchemaElement
	ProjectionType        string
	NonKeyAttributes      []string
	ProvisionedThroughput Throughput
	OnDemandThroughput    OnDemandThroughput
}

// Replica describes global-table replica metadata reported on the table.
type Replica struct {
	RegionName     string
	Status         string
	KMSMasterKeyID string
	TableClass     string
}
