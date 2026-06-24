// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docdbelastic

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only Amazon DocumentDB Elastic Clusters cluster
// observations for one AWS claim. Implementations read control-plane metadata
// through the docdb-elastic management APIs and never read document contents,
// collections, indexes, query results, or the admin password.
type Client interface {
	// Snapshot returns every DocumentDB Elastic cluster visible to the
	// configured AWS credentials, each carrying its control-plane metadata.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures DocumentDB Elastic cluster metadata plus non-fatal scan
// warnings.
type Snapshot struct {
	// Clusters is the metadata-only set of DocumentDB Elastic clusters.
	Clusters []Cluster
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Cluster is the scanner-owned DocumentDB Elastic Clusters model. It carries
// control-plane metadata only and intentionally excludes document contents,
// collections, indexes, query results, the cluster endpoint connection string,
// and the admin password. The admin user name is intentionally omitted because,
// under SECRET_ARN auth, AWS reports the Secrets Manager secret ARN in that
// field; that ARN is captured only as the admin-secret reference, never as a
// persisted credential or username.
type Cluster struct {
	// ARN is the Amazon Resource Name that uniquely identifies the cluster.
	ARN string
	// Name is the DocumentDB Elastic cluster name.
	Name string
	// Status is the current cluster lifecycle status (for example ACTIVE).
	Status string
	// AuthType is the authentication type AWS reports for the cluster, one of
	// PLAIN_TEXT or SECRET_ARN. It records where the admin password is fetched
	// from, never the password itself.
	AuthType string
	// AdminSecretARN is the Secrets Manager secret ARN that holds the cluster's
	// admin credentials, populated only when AuthType is SECRET_ARN. The secret
	// value is never read; only the ARN reference is recorded.
	AdminSecretARN string
	// KMSKeyID is the identifier of the KMS key used to encrypt cluster data.
	// AWS reports a key id or key ARN here.
	KMSKeyID string
	// ShardCapacity is the number of vCPUs assigned to each cluster shard.
	ShardCapacity int32
	// ShardCount is the number of shards assigned to the cluster.
	ShardCount int32
	// ShardInstanceCount is the number of instances applied to every shard
	// (one writer plus replicas), when AWS reports it.
	ShardInstanceCount int32
	// BackupRetentionPeriod is the number of days automatic snapshots are
	// retained, when AWS reports it.
	BackupRetentionPeriod int32
	// PreferredBackupWindow is the daily UTC window during which automated
	// backups are created, when configured.
	PreferredBackupWindow string
	// PreferredMaintenanceWindow is the weekly UTC window during which system
	// maintenance can occur, when configured.
	PreferredMaintenanceWindow string
	// SubnetIDs are the bare EC2 subnet ids (subnet-...) the cluster is
	// deployed into.
	SubnetIDs []string
	// SecurityGroupIDs are the bare EC2 VPC security-group ids (sg-...)
	// associated with the cluster.
	SecurityGroupIDs []string
	// CreateTime is when the cluster was created.
	CreateTime time.Time
	// Tags carries the cluster resource tags.
	Tags map[string]string
}
