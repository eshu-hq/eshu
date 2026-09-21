// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package quicksight

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only Amazon QuickSight observations for one AWS
// claim. Implementations read account-scoped control-plane metadata only and
// never read data-source credentials, connection passwords, secret connection
// parameters, SQL query bodies, or visual definitions.
type Client interface {
	// Snapshot returns every QuickSight data source, dataset, dashboard, and
	// analysis visible to the configured AWS credentials for the boundary
	// account. A QuickSight subscription is account-scoped: when the account is
	// not signed up for QuickSight the implementation returns an empty snapshot
	// (optionally with a warning) instead of failing the scan.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures QuickSight resource metadata plus non-fatal scan warnings.
type Snapshot struct {
	// DataSources is the metadata-only set of QuickSight data sources.
	DataSources []DataSource
	// DataSets is the metadata-only set of QuickSight datasets.
	DataSets []DataSet
	// Dashboards is the metadata-only set of QuickSight dashboards.
	Dashboards []Dashboard
	// Analyses is the metadata-only set of QuickSight analyses.
	Analyses []Analysis
	// VPCConnections maps a bare QuickSight VPC connection id to the security
	// groups and subnets it spans, resolved once per account. Data sources that
	// use a VPC connection key into this map to emit security-group and subnet
	// edges.
	VPCConnections map[string]VPCConnection
	// Warnings carries non-fatal partial-scan observations such as a
	// not-subscribed account or sustained throttling that omitted a component.
	Warnings []awscloud.WarningObservation
}

// VPCConnection is the scanner-owned resolved view of a QuickSight VPC
// connection's network membership. It carries only the bare EC2 security group
// and subnet ids needed to key data-source-to-network edges; it intentionally
// excludes DNS resolvers and IAM role identity.
type VPCConnection struct {
	// SecurityGroupIDs are the bare EC2 security group ids (sg-...) attached to
	// the VPC connection.
	SecurityGroupIDs []string
	// SubnetIDs are the bare EC2 subnet ids (subnet-...) the connection's network
	// interfaces reside in.
	SubnetIDs []string
}

// DataSource is the scanner-owned QuickSight data source model. It carries
// control-plane metadata only and intentionally excludes credentials,
// connection passwords, secret connection parameters, and the Secrets Manager
// secret value.
type DataSource struct {
	// ARN is the Amazon Resource Name that uniquely identifies the data source.
	ARN string
	// ID is the QuickSight data source id, unique per Region per account.
	ID string
	// Name is the data source display name.
	Name string
	// Type is the connector type (for example REDSHIFT, RDS, ATHENA, S3).
	Type string
	// Status is the data source resource status (for example CREATION_SUCCESSFUL).
	Status string
	// SecretConfigured reports whether the data source references a Secrets
	// Manager secret, without recording the secret ARN or value.
	SecretConfigured bool
	// VPCConnectionARN is the ARN of the VPC connection the data source uses, when
	// configured. It drives security-group and subnet edges via the resolved VPC
	// connection summary.
	VPCConnectionARN string
	// Backing identifies the resolvable backing-store reference (cluster id,
	// instance id, workgroup name, or S3 manifest bucket) for the connector type,
	// when QuickSight reports one that resolves to a scanned resource.
	Backing BackingStore
	// CreatedTime is when the data source was created.
	CreatedTime time.Time
	// LastUpdatedTime is when the data source was last updated.
	LastUpdatedTime time.Time
	// Tags carries the data source resource tags.
	Tags map[string]string
}

// BackingStoreKind classifies the resolvable backing-store reference a
// QuickSight data source connects to.
type BackingStoreKind string

const (
	// BackingStoreNone marks a data source whose connector has no
	// repo-resolvable backing-store reference (an unscanned connector type, or a
	// connection identified only by host/port).
	BackingStoreNone BackingStoreKind = ""
	// BackingStoreRedshiftCluster marks a Redshift data source identified by a
	// provisioned cluster id.
	BackingStoreRedshiftCluster BackingStoreKind = "redshift_cluster"
	// BackingStoreRDSInstance marks an RDS data source identified by a DB
	// instance id.
	BackingStoreRDSInstance BackingStoreKind = "rds_instance"
	// BackingStoreAthenaWorkGroup marks an Athena data source identified by a
	// workgroup name.
	BackingStoreAthenaWorkGroup BackingStoreKind = "athena_workgroup"
	// BackingStoreS3Bucket marks an S3 data source identified by an S3 manifest
	// bucket name.
	BackingStoreS3Bucket BackingStoreKind = "s3_bucket"
)

// BackingStore is the resolvable backing-store reference a QuickSight data
// source connects to. Identifier is the bare id/name QuickSight reports; it is
// never a credential, host string with a password, or SQL body.
type BackingStore struct {
	// Kind classifies the backing store the data source connects to.
	Kind BackingStoreKind
	// Identifier is the bare resolvable id or name (cluster id, instance id,
	// workgroup name, or S3 bucket name) for the backing store.
	Identifier string
}

// DataSet is the scanner-owned QuickSight dataset model. It carries
// control-plane metadata only and intentionally excludes column data, row-level
// security values, and SQL query bodies.
type DataSet struct {
	// ARN is the Amazon Resource Name that uniquely identifies the dataset.
	ARN string
	// ID is the QuickSight dataset id.
	ID string
	// Name is the dataset display name.
	Name string
	// ImportMode reports whether the dataset is SPICE or DIRECT_QUERY.
	ImportMode string
	// DataSourceARNs are the data-source ARNs the dataset's physical tables read.
	// They are derived from the relational, custom-SQL, and S3 physical tables;
	// the SQL bodies of custom-SQL tables are never read.
	DataSourceARNs []string
	// CreatedTime is when the dataset was created.
	CreatedTime time.Time
	// LastUpdatedTime is when the dataset was last updated.
	LastUpdatedTime time.Time
	// Tags carries the dataset resource tags.
	Tags map[string]string
}

// Dashboard is the scanner-owned QuickSight dashboard model. It carries
// control-plane metadata only and intentionally excludes the visual definition.
type Dashboard struct {
	// ARN is the Amazon Resource Name that uniquely identifies the dashboard.
	ARN string
	// ID is the QuickSight dashboard id.
	ID string
	// Name is the dashboard display name.
	Name string
	// PublishedVersionNumber is the published dashboard version number, when set.
	PublishedVersionNumber int64
	// DataSetARNs are the dataset ARNs the published dashboard version reads.
	DataSetARNs []string
	// CreatedTime is when the dashboard was created.
	CreatedTime time.Time
	// LastUpdatedTime is when the dashboard was last updated.
	LastUpdatedTime time.Time
	// Tags carries the dashboard resource tags.
	Tags map[string]string
}

// Analysis is the scanner-owned QuickSight analysis model. It carries
// control-plane metadata only and intentionally excludes the visual definition.
type Analysis struct {
	// ARN is the Amazon Resource Name that uniquely identifies the analysis.
	ARN string
	// ID is the QuickSight analysis id.
	ID string
	// Name is the analysis display name.
	Name string
	// Status is the analysis resource status.
	Status string
	// DataSetARNs are the dataset ARNs the analysis reads.
	DataSetARNs []string
	// CreatedTime is when the analysis was created.
	CreatedTime time.Time
	// LastUpdatedTime is when the analysis was last updated.
	LastUpdatedTime time.Time
	// Tags carries the analysis resource tags.
	Tags map[string]string
}
