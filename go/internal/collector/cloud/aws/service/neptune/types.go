// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package neptune

import "context"

// Client lists metadata-only Amazon Neptune observations for one AWS claim.
//
// Every method is a control-plane read. The interface deliberately excludes
// every mutation API (Create/Delete/Modify/Restore/Failover/Reboot for
// clusters, instances, parameter groups, subnet groups, snapshots, and global
// clusters; CreateGraph/DeleteGraph/ResetGraph/UpdateGraph/RestoreGraph and
// CreateGraphSnapshot for Neptune Analytics) and every data-plane access
// (ExecuteQuery, CancelQuery, GetQuery, ListQueries, import/export tasks). The
// scanner reaches the Neptune and Neptune Analytics APIs only through this
// interface, so the interface shape is a load-bearing proof that mutation and
// graph data-plane calls are unreachable from this code path. See
// contract_test.go.
type Client interface {
	ListDBClusters(ctx context.Context) ([]DBCluster, error)
	ListClusterInstances(ctx context.Context) ([]ClusterInstance, error)
	ListClusterParameterGroups(ctx context.Context) ([]ClusterParameterGroup, error)
	ListClusterSnapshots(ctx context.Context) ([]ClusterSnapshot, error)
	ListSubnetGroups(ctx context.Context) ([]SubnetGroup, error)
	ListGlobalClusters(ctx context.Context) ([]GlobalCluster, error)
	ListGraphs(ctx context.Context) ([]Graph, error)
	ListGraphSnapshots(ctx context.Context) ([]GraphSnapshot, error)
}

// DBCluster is the scanner-owned Neptune (provisioned) DB cluster model. It
// contains control-plane metadata only and intentionally excludes master user
// passwords, master user secrets, and graph vertex or edge contents.
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

// ClusterInstance is the scanner-owned Neptune cluster instance model. It
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

// ClusterParameterGroup is the scanner-owned Neptune cluster parameter group
// model. It carries the group name and parameter-group family only. Parameter
// values are never read or persisted.
type ClusterParameterGroup struct {
	ARN         string
	Name        string
	Family      string
	Description string
	Tags        map[string]string
}

// ClusterSnapshot is the scanner-owned Neptune cluster snapshot metadata model.
// Snapshot contents are never read; only identity and reported metadata are
// persisted.
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

// SubnetGroup is the scanner-owned Neptune DB subnet group model.
type SubnetGroup struct {
	ARN         string
	Name        string
	Description string
	Status      string
	VPCID       string
	SubnetIDs   []string
	Tags        map[string]string
}

// GlobalCluster is the scanner-owned Neptune global cluster model.
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

// Graph is the scanner-owned Neptune Analytics graph model. It carries
// control-plane metadata only: name, status, the vector-search embedding
// dimension, and provisioning shape. It never carries graph vertex or edge
// contents, query results, or import/export payloads.
type Graph struct {
	ARN                   string
	ID                    string
	Name                  string
	Status                string
	KMSKeyID              string
	VectorSearchDimension *int32
	ProvisionedMemory     int32
	ReplicaCount          int32
	PublicConnectivity    bool
	DeletionProtection    bool
	EndpointAddress       string
	Tags                  map[string]string
}

// GraphSnapshot is the scanner-owned Neptune Analytics graph snapshot metadata
// model. Snapshot contents are never read; only identity and reported metadata
// are persisted.
type GraphSnapshot struct {
	ARN           string
	ID            string
	Name          string
	Status        string
	KMSKeyID      string
	SourceGraphID string
	Tags          map[string]string
}

// ClusterMember is reported Neptune DB cluster membership metadata.
type ClusterMember struct {
	DBInstanceIdentifier string
	IsWriter             bool
}

// GlobalClusterMember is reported Neptune global cluster membership metadata.
// DBClusterARN names a regional DB cluster joined into the global cluster.
type GlobalClusterMember struct {
	DBClusterARN string
	IsWriter     bool
}
