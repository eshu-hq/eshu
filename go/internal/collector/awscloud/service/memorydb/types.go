// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package memorydb

import "context"

// Client lists MemoryDB metadata for one claimed account and region. It is the
// scanner-facing surface that adapter packages implement; the contract is
// intentionally narrow so the scanner cannot reach for user passwords, AUTH
// tokens, ACL access strings, snapshot data, or mutation APIs.
type Client interface {
	ListClusters(ctx context.Context) ([]Cluster, error)
	ListSubnetGroups(ctx context.Context) ([]SubnetGroup, error)
	ListParameterGroups(ctx context.Context) ([]ParameterGroup, error)
	ListUsers(ctx context.Context) ([]User, error)
	ListACLs(ctx context.Context) ([]ACL, error)
	ListSnapshots(ctx context.Context) ([]SnapshotMetadata, error)
}

// Cluster is the scanner-owned MemoryDB cluster model. It carries control-plane
// metadata only and intentionally excludes AUTH token values, cache keys, cache
// values, snapshot contents, and any payload data.
type Cluster struct {
	ARN                      string
	Name                     string
	Description              string
	Status                   string
	Engine                   string
	EngineVersion            string
	NodeType                 string
	NumberOfShards           int32
	NumberOfReplicasPerShard int32
	ACLName                  string
	ParameterGroupName       string
	SubnetGroupName          string
	SecurityGroupIDs         []string
	KMSKeyID                 string
	SNSTopicARN              string
	TLSEnabled               bool
	DataTiering              string
	AutoMinorVersionUpgrade  bool
	SnapshotRetentionLimit   int32
	SnapshotWindow           string
	MaintenanceWindow        string
	AvailabilityMode         string
	NetworkType              string
	IPDiscovery              string
	Tags                     map[string]string
}

// SubnetGroup is the scanner-owned MemoryDB subnet group model.
type SubnetGroup struct {
	ARN         string
	Name        string
	Description string
	VPCID       string
	SubnetIDs   []string
	Tags        map[string]string
}

// ParameterGroup is the scanner-owned MemoryDB parameter group model. It
// persists the name, family, description, and tags as non-secret control-plane
// metadata. Individual parameter values can reveal operational posture and stay
// outside this type by design.
type ParameterGroup struct {
	ARN         string
	Name        string
	Family      string
	Description string
	Tags        map[string]string
}

// User is the scanner-owned MemoryDB user model. MemoryDB's User resource
// exposes Passwords (via the authentication mode) and an AccessString grant
// string that the scanner must never persist. AccessStringPresent is a
// non-secret summary signal that records whether AWS reported an access string
// without persisting the grant string itself. PasswordCount is included as a
// non-secret cardinality signal only.
type User struct {
	ARN                  string
	Name                 string
	Status               string
	AuthenticationType   string
	PasswordCount        int32
	AccessStringPresent  bool
	MinimumEngineVersion string
	ACLNames             []string
	Tags                 map[string]string
}

// ACL is the scanner-owned MemoryDB Access Control List model. It records the
// ACL identity, status, and the member user names so the scanner can emit
// acl-to-user evidence. Grant strings and password material stay with the User
// resource's adapter boundary, which never persists them.
type ACL struct {
	ARN                  string
	Name                 string
	Status               string
	MinimumEngineVersion string
	UserNames            []string
	ClusterNames         []string
	Tags                 map[string]string
}

// SnapshotMetadata is the scanner-owned MemoryDB snapshot model. The scanner
// persists name, source cluster identity, snapshot source, and status only;
// cluster configuration, shard sizes, engine version, KMS keys, and any backup
// payload detail stay outside the type by design.
type SnapshotMetadata struct {
	ARN               string
	Name              string
	Status            string
	Source            string
	SourceClusterName string
	Tags              map[string]string
}
