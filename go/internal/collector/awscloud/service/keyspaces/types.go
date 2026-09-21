// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package keyspaces

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only Amazon Keyspaces observations for one AWS
// claim. Implementations read keyspace and table control-plane metadata only and
// never execute CQL statements, run Select/ExecuteStatement/BatchStatement, read
// table rows or cells, or mutate keyspaces or tables.
type Client interface {
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures Amazon Keyspaces keyspace and table metadata plus non-fatal
// scan warnings for partial optional-metadata coverage.
type Snapshot struct {
	Keyspaces []Keyspace
	Tables    []Table
	Warnings  []awscloud.WarningObservation
}

// Keyspace is the scanner-owned Amazon Keyspaces keyspace model. It contains
// control-plane metadata only: identity, ARN, replication strategy, and the list
// of replication Regions for multi-Region keyspaces. It never carries table row
// or cell data.
type Keyspace struct {
	Name                string
	ARN                 string
	ReplicationStrategy string
	ReplicationRegions  []string
}

// Table is the scanner-owned Amazon Keyspaces table model. It contains
// control-plane metadata only and intentionally excludes table row data, cell
// values, CQL query results, and mutations. Schema column names and types are
// structural metadata, not row data, and are safe to report.
type Table struct {
	ARN                  string
	Name                 string
	KeyspaceName         string
	KeyspaceARN          string
	Status               string
	CreationTime         time.Time
	DefaultTimeToLive    int32
	CapacityMode         string
	ReadCapacityUnits    int64
	WriteCapacityUnits   int64
	Encryption           Encryption
	PointInTimeRecovery  PointInTimeRecovery
	ClientSideTimestamps string
	TimeToLiveStatus     string
	CDCStatus            string
	Comment              string
	LatestStreamARN      string
	Schema               Schema
	Tags                 map[string]string
}

// Encryption describes Amazon Keyspaces table server-side encryption metadata.
// KMSKeyIdentifier is populated only for customer-managed KMS keys and is the
// KMS key ARN reported by the API.
type Encryption struct {
	Type             string
	KMSKeyIdentifier string
}

// PointInTimeRecovery describes Amazon Keyspaces point-in-time recovery (PITR)
// status for a table.
type PointInTimeRecovery struct {
	Status string
}

// Schema describes the structural CQL schema of an Amazon Keyspaces table. It
// carries column names and data types and the names of partition, clustering,
// and static columns. These are structural metadata definitions, not table row
// or cell data.
type Schema struct {
	Columns        []Column
	PartitionKeys  []string
	ClusteringKeys []ClusteringKey
	StaticColumns  []string
}

// Column is one Amazon Keyspaces table column definition: a column name and its
// CQL data type. It is structural schema metadata only and carries no row data.
type Column struct {
	Name string
	Type string
}

// ClusteringKey is one Amazon Keyspaces clustering-key column definition: the
// column name and its sort order. It is structural schema metadata only.
type ClusteringKey struct {
	Name    string
	OrderBy string
}
