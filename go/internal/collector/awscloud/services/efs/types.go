// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package efs

import "context"

// Client is the EFS read surface consumed by Scanner. Runtime adapters
// translate AWS SDK responses into these scanner-owned metadata records. The
// surface exposes describe-only reads; it never carries a method that mutates
// EFS state or reads an NFS file system access policy body.
type Client interface {
	// ListFileSystems returns file system metadata, including each file
	// system's access points, mount targets, and lifecycle policy summary.
	ListFileSystems(context.Context) ([]FileSystem, error)
	// ListReplicationConfigurations returns replication configuration metadata
	// for the scanned account and region.
	ListReplicationConfigurations(context.Context) ([]ReplicationConfiguration, error)
}

// FileSystem is the scanner-owned representation of one EFS file system and its
// directly reported child resources. It contains inventory metadata only; the
// NFS file system policy body is intentionally outside this contract.
type FileSystem struct {
	ID                   string
	ARN                  string
	Name                 string
	OwnerID              string
	LifeCycleState       string
	PerformanceMode      string
	ThroughputMode       string
	Encrypted            bool
	KMSKeyID             string
	AvailabilityZoneID   string
	NumberOfMountTargets int32
	LifecyclePolicy      LifecyclePolicySummary
	Tags                 map[string]string
	AccessPoints         []AccessPoint
	MountTargets         []MountTarget
}

// LifecyclePolicySummary captures the EFS lifecycle transition rules as bounded
// metadata. It records which transition rules are configured, never file
// contents or policy authorization bodies.
type LifecyclePolicySummary struct {
	TransitionToIA                  string
	TransitionToArchive             string
	TransitionToPrimaryStorageClass string
}

// AccessPoint is the scanner-owned representation of one EFS access point. The
// root directory path and POSIX identity are infrastructure metadata, not file
// contents, so the scanner keeps them.
type AccessPoint struct {
	ID             string
	ARN            string
	Name           string
	FileSystemID   string
	LifeCycleState string
	RootDirectory  string
	PosixUID       *int64
	PosixGID       *int64
	Tags           map[string]string
}

// MountTarget is the scanner-owned representation of one EFS mount target. It
// carries network placement metadata for subnet and security group
// relationship evidence.
type MountTarget struct {
	ID                 string
	FileSystemID       string
	SubnetID           string
	VPCID              string
	AvailabilityZoneID string
	LifeCycleState     string
	IPAddress          string
	NetworkInterfaceID string
	SecurityGroupIDs   []string
}

// ReplicationConfiguration is the scanner-owned representation of one EFS
// replication configuration keyed by its source file system. Each destination
// records the target file system the source replicates to.
type ReplicationConfiguration struct {
	SourceFileSystemID  string
	SourceFileSystemARN string
	Destinations        []ReplicationDestination
}

// ReplicationDestination captures one EFS replication target file system and
// its reported sync status.
type ReplicationDestination struct {
	FileSystemID string
	Region       string
	Status       string
}
