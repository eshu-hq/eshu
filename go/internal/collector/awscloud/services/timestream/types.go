// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package timestream

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only Amazon Timestream database and table
// observations for one AWS claim. Implementations read control-plane metadata
// through the timestream-write management APIs and never read time-series
// records, measures, or query results.
type Client interface {
	// Snapshot returns every Timestream database visible to the configured AWS
	// credentials, each carrying the tables that live under it.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures Timestream database metadata plus non-fatal scan warnings.
type Snapshot struct {
	// Databases is the metadata-only set of Timestream databases, each carrying
	// its tables.
	Databases []Database
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Database is the scanner-owned Timestream database model. It carries
// control-plane metadata only and intentionally excludes any time-series
// record, measure value, or query result.
type Database struct {
	// ARN is the Amazon Resource Name that uniquely identifies the database.
	ARN string
	// Name is the Timestream database name.
	Name string
	// KMSKeyID is the identifier of the KMS key used to encrypt database data.
	// AWS reports a key ARN here for Timestream databases.
	KMSKeyID string
	// TableCount is the total number of tables AWS reports within the database.
	TableCount int64
	// CreationTime is when the database was created.
	CreationTime time.Time
	// LastUpdatedTime is when the database was last updated.
	LastUpdatedTime time.Time
	// Tags carries the database resource tags.
	Tags map[string]string
	// Tables are the metadata-only tables that live under this database.
	Tables []Table
}

// Table is the scanner-owned Timestream table model. It carries control-plane
// metadata only and intentionally excludes time-series records, measure
// values, schema measure definitions, and query results.
type Table struct {
	// ARN is the Amazon Resource Name that uniquely identifies the table.
	ARN string
	// Name is the Timestream table name.
	Name string
	// DatabaseName is the name of the parent Timestream database.
	DatabaseName string
	// State is the current table lifecycle state (for example ACTIVE).
	State string
	// MemoryStoreRetentionPeriodInHours is the memory-store retention duration.
	MemoryStoreRetentionPeriodInHours int64
	// MagneticStoreRetentionPeriodInDays is the magnetic-store retention
	// duration.
	MagneticStoreRetentionPeriodInDays int64
	// MagneticStoreWritesEnabled reports whether magnetic-store writes are
	// enabled on the table.
	MagneticStoreWritesEnabled bool
	// RejectedDataS3Bucket is the customer S3 bucket name where magnetic-store
	// rejected-data reports are written, when configured. It is a bucket name,
	// not an ARN.
	RejectedDataS3Bucket string
	// RejectedDataS3Prefix is the optional object-key prefix for the
	// rejected-data report location.
	RejectedDataS3Prefix string
	// RejectedDataS3EncryptionOption is the encryption option AWS reports for
	// the rejected-data S3 location (for example SSE_S3 or SSE_KMS).
	RejectedDataS3EncryptionOption string
	// PartitionKeyNames are the composite partition-key attribute names from
	// the table schema. Measure definitions are intentionally excluded.
	PartitionKeyNames []string
	// CreationTime is when the table was created.
	CreationTime time.Time
	// LastUpdatedTime is when the table was last updated.
	LastUpdatedTime time.Time
	// Tags carries the table resource tags.
	Tags map[string]string
}
