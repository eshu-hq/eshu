// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docdb

import "context"

// Client lists metadata-only Amazon DocumentDB observations for one AWS claim.
//
// Every method is a control-plane read. The interface deliberately excludes
// every mutation API (Create/Delete/Modify/Restore/Failover/Reboot for
// clusters, instances, parameter groups, subnet groups, snapshots, global
// clusters, and event subscriptions) and every data-plane access. The scanner
// reaches the DocumentDB API only through this interface, so the interface
// shape is a load-bearing proof that mutation and data-plane calls are
// unreachable from this code path. See contract_test.go.
type Client interface {
	ListDBClusters(ctx context.Context) ([]DBCluster, error)
	ListClusterInstances(ctx context.Context) ([]ClusterInstance, error)
	ListClusterParameterGroups(ctx context.Context) ([]ClusterParameterGroup, error)
	ListClusterSnapshots(ctx context.Context) ([]ClusterSnapshot, error)
	ListSubnetGroups(ctx context.Context) ([]SubnetGroup, error)
	ListGlobalClusters(ctx context.Context) ([]GlobalCluster, error)
	ListEventSubscriptions(ctx context.Context) ([]EventSubscription, error)
}

// DBCluster is the scanner-owned DocumentDB DB cluster model. It contains
// control-plane metadata only and intentionally excludes master user
// passwords, master user secrets, database document contents, schemas, and
// collection data.
type DBCluster struct {
	ARN                          string
	Identifier                   string
	ResourceID                   string
	Engine                       string
	EngineVersion                string
	Status                       string
	EndpointAddress              string
	ReaderEndpointAddress        string
	HostedZoneID                 string
	Port                         int32
	MultiAZ                      bool
	StorageEncrypted             bool
	KMSKeyID                     string
	DeletionProtection           bool
	BackupRetentionPeriod        int32
	DBSubnetGroupName            string
	VPCSecurityGroupIDs          []string
	Members                      []ClusterMember
	ParameterGroup               string
	EnabledCloudwatchLogsExports []string
	AssociatedRoleARNs           []string
	Tags                         map[string]string
}

// ClusterInstance is the scanner-owned DocumentDB cluster instance model. It
// contains control-plane metadata only.
type ClusterInstance struct {
	ARN               string
	Identifier        string
	ResourceID        string
	Class             string
	Engine            string
	EngineVersion     string
	Status            string
	EndpointAddress   string
	EndpointPort      int32
	HostedZoneID      string
	AvailabilityZone  string
	StorageEncrypted  bool
	KMSKeyID          string
	ClusterIdentifier string
	PromotionTier     int32
	Tags              map[string]string
}

// ClusterParameterGroup is the scanner-owned DocumentDB cluster parameter
// group model. It carries the group name, parameter-group family, and the
// count of parameters only. Parameter values are never read or persisted.
type ClusterParameterGroup struct {
	ARN            string
	Name           string
	Family         string
	Description    string
	ParameterCount int
	Tags           map[string]string
}

// ClusterSnapshot is the scanner-owned DocumentDB cluster snapshot metadata
// model. Snapshot contents are never read; only identity and reported
// metadata are persisted.
type ClusterSnapshot struct {
	ARN               string
	Identifier        string
	ClusterIdentifier string
	Engine            string
	EngineVersion     string
	Status            string
	SnapshotType      string
	StorageEncrypted  bool
	KMSKeyID          string
	VPCID             string
	Tags              map[string]string
}

// SubnetGroup is the scanner-owned DocumentDB DB subnet group model.
type SubnetGroup struct {
	ARN         string
	Name        string
	Description string
	Status      string
	VPCID       string
	SubnetIDs   []string
	Tags        map[string]string
}

// GlobalCluster is the scanner-owned DocumentDB global cluster model.
type GlobalCluster struct {
	ARN                string
	Identifier         string
	ResourceID         string
	Engine             string
	EngineVersion      string
	Status             string
	StorageEncrypted   bool
	DeletionProtection bool
	Members            []GlobalClusterMember
	Tags               map[string]string
}

// EventSubscription is the scanner-owned DocumentDB event subscription
// metadata model.
type EventSubscription struct {
	ARN             string
	Name            string
	CustomerAWSID   string
	Enabled         bool
	Status          string
	SourceType      string
	SNSTopicARN     string
	SourceIDs       []string
	EventCategories []string
	Tags            map[string]string
}

// ClusterMember is reported DocumentDB DB cluster membership metadata.
type ClusterMember struct {
	DBInstanceIdentifier string
	IsWriter             bool
}

// GlobalClusterMember is reported DocumentDB global cluster membership
// metadata. DBClusterARN names a regional DB cluster joined into the global
// cluster.
type GlobalClusterMember struct {
	DBClusterARN string
	IsWriter     bool
}
