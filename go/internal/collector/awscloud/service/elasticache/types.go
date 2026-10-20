// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elasticache

import "context"

// Client lists ElastiCache metadata for one claimed account and region. It is
// the scanner-facing surface that adapter packages implement; the contract is
// intentionally narrow so the scanner cannot reach for cache contents, AUTH
// tokens, or mutation APIs.
type Client interface {
	ListCacheClusters(ctx context.Context) ([]CacheCluster, error)
	ListReplicationGroups(ctx context.Context) ([]ReplicationGroup, error)
	ListCacheSubnetGroups(ctx context.Context) ([]SubnetGroup, error)
	ListCacheParameterGroups(ctx context.Context) ([]ParameterGroup, error)
	ListUsers(ctx context.Context) ([]User, error)
	ListUserGroups(ctx context.Context) ([]UserGroup, error)
	ListSnapshots(ctx context.Context) ([]SnapshotMetadata, error)
}

// CacheCluster is the scanner-owned ElastiCache cache cluster model. It carries
// control-plane metadata only and intentionally excludes AUTH token values,
// cache keys, cache values, snapshot contents, and any payload data.
type CacheCluster struct {
	ARN                       string
	ID                        string
	Engine                    string
	EngineVersion             string
	Status                    string
	NodeType                  string
	NumCacheNodes             int32
	PreferredAvailabilityZone string
	SubnetGroupName           string
	VPCID                     string
	SubnetIDs                 []string
	SecurityGroupIDs          []string
	ParameterGroupName        string
	ReplicationGroupID        string
	KMSKeyID                  string
	TransitEncryptionEnabled  bool
	AtRestEncryptionEnabled   bool
	AuthTokenEnabled          bool
	SnapshotRetentionLimit    int32
	SnapshotWindow            string
	AutoMinorVersionUpgrade   bool
	NotificationTopicARN      string
	NetworkType               string
	IPDiscovery               string
	Tags                      map[string]string
}

// ReplicationGroup is the scanner-owned ElastiCache replication group model.
// AuthToken values, cache contents, and snapshot data are intentionally
// excluded.
type ReplicationGroup struct {
	ARN                      string
	ID                       string
	Description              string
	Status                   string
	MemberClusters           []string
	AutomaticFailover        string
	MultiAZ                  string
	ClusterEnabled           bool
	NodeType                 string
	KMSKeyID                 string
	TransitEncryptionEnabled bool
	AtRestEncryptionEnabled  bool
	AuthTokenEnabled         bool
	SnapshotRetentionLimit   int32
	SnapshotWindow           string
	DataTiering              string
	NetworkType              string
	IPDiscovery              string
	Tags                     map[string]string
}

// SubnetGroup is the scanner-owned ElastiCache cache subnet group model.
type SubnetGroup struct {
	ARN         string
	Name        string
	Description string
	VPCID       string
	SubnetIDs   []string
	Tags        map[string]string
}

// ParameterGroup is the scanner-owned ElastiCache cache parameter group model.
// Parameter values are not persisted because they can include engine knobs that
// reveal operational posture.
type ParameterGroup struct {
	ARN         string
	Name        string
	Family      string
	Description string
	IsGlobal    bool
	Tags        map[string]string
}

// User is the scanner-owned ElastiCache user model. ElastiCache's User
// resource exposes Passwords and AccessString fields that the scanner must
// never persist. PasswordCount is included as a non-secret cardinality signal
// only.
type User struct {
	ARN                  string
	ID                   string
	Name                 string
	Engine               string
	Status               string
	AuthenticationType   string
	PasswordCount        int32
	MinimumEngineVersion string
	UserGroupIDs         []string
	Tags                 map[string]string
}

// UserGroup is the scanner-owned ElastiCache user group model.
type UserGroup struct {
	ARN     string
	ID      string
	Engine  string
	Status  string
	UserIDs []string
	Tags    map[string]string
}

// SnapshotMetadata is the scanner-owned ElastiCache snapshot model. Per issue
// #713 the scanner must persist name, source, and status only; node snapshot
// detail, engine version, KMS keys, and AUTH token state stay outside the
// type by design.
type SnapshotMetadata struct {
	ARN                  string
	Name                 string
	Status               string
	SnapshotSource       string
	SourceCacheClusterID string
	SourceReplicationGrp string
	Tags                 map[string]string
}
