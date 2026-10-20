// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fsx

import "context"

// Client is the FSx read surface consumed by Scanner. Runtime adapters
// translate AWS SDK describe responses into these scanner-owned metadata
// records. The surface exposes describe-only reads across every FSx flavor
// (Windows File Server, Lustre, NetApp ONTAP, OpenZFS); it never carries a
// method that mutates FSx state, restores a volume, copies a backup, or reads
// file contents.
type Client interface {
	// ListFileSystems returns file system metadata for every flavor in the
	// scanned account and region.
	ListFileSystems(context.Context) ([]FileSystem, error)
	// ListBackups returns backup metadata for the scanned account and region.
	ListBackups(context.Context) ([]Backup, error)
	// ListStorageVirtualMachines returns FSx for NetApp ONTAP storage virtual
	// machine metadata.
	ListStorageVirtualMachines(context.Context) ([]StorageVirtualMachine, error)
	// ListVolumes returns FSx for NetApp ONTAP and OpenZFS volume metadata.
	ListVolumes(context.Context) ([]Volume, error)
	// ListSnapshots returns FSx volume snapshot metadata.
	ListSnapshots(context.Context) ([]Snapshot, error)
}

// FileSystem is the scanner-owned representation of one FSx file system across
// all flavors. It carries control-plane metadata only. The Active Directory
// self-managed credential password and the ONTAP fsxadmin password are never
// mapped into this type. DNS server IPs, domain names, the service-account user
// name, and the domain-join secret ARN of a self-managed AD are also excluded;
// only the AWS Managed Microsoft AD directory ID is kept as a relationship
// join key.
type FileSystem struct {
	ID                 string
	ARN                string
	FileSystemType     string
	StorageType        string
	Lifecycle          string
	OwnerID            string
	VPCID              string
	SubnetIDs          []string
	PreferredSubnetID  string
	NetworkType        string
	StorageCapacityGiB int32
	KMSKeyID           string
	DNSName            string
	// ActiveDirectoryID is the AWS Managed Microsoft AD directory ID the file
	// system is joined to (Windows File Server, and ONTAP when joined at the
	// file-system level). Empty for self-managed AD and for flavors that do not
	// join a directory.
	ActiveDirectoryID string
	// DeploymentType is the flavor-specific deployment type (for example
	// SINGLE_AZ_1, MULTI_AZ_1, PERSISTENT_2). Empty when the flavor does not
	// report one.
	DeploymentType string
	// ThroughputCapacityMBps is the sustained throughput in MBps for flavors
	// that report it (Windows, ONTAP, OpenZFS). Nil when not reported.
	ThroughputCapacityMBps *int32
	// PerUnitStorageThroughput is the FSx for Lustre per-unit storage
	// throughput. Nil for other flavors.
	PerUnitStorageThroughput *int32
	// FileSystemTypeVersion is the Lustre version (2.10, 2.12, 2.15) when
	// reported.
	FileSystemTypeVersion string
	Tags                  map[string]string
}

// StorageVirtualMachine is the scanner-owned representation of one FSx for
// NetApp ONTAP storage virtual machine. The SVM admin password is never
// mapped. The self-managed AD service-account user name, password, DNS IPs,
// and domain-join secret ARN are excluded; only the directory ID (when the SVM
// joins an AWS Managed Microsoft AD) is kept as a relationship join key.
type StorageVirtualMachine struct {
	ID           string
	ARN          string
	Name         string
	FileSystemID string
	Lifecycle    string
	Subtype      string
	UUID         string
	NetBiosName  string
	// ActiveDirectoryID is the AWS Managed Microsoft AD directory ID the SVM is
	// joined to. Empty for self-managed AD or unjoined SVMs.
	ActiveDirectoryID string
	Tags              map[string]string
}

// Volume is the scanner-owned representation of one FSx volume (NetApp ONTAP
// or OpenZFS). It records placement and bounded configuration metadata only;
// volume file contents are never read.
type Volume struct {
	ID           string
	ARN          string
	Name         string
	FileSystemID string
	VolumeType   string
	Lifecycle    string
	// StorageVirtualMachineID is set for ONTAP volumes; empty for OpenZFS.
	StorageVirtualMachineID string
	// JunctionPath is the ONTAP volume mount point inside the SVM namespace. It
	// is infrastructure metadata, not file contents.
	JunctionPath string
	// VolumePath is the OpenZFS volume path. Infrastructure metadata only.
	VolumePath string
	// SizeInMegabytes is the ONTAP volume size when reported. Nil otherwise.
	SizeInMegabytes *int32
	// StorageCapacityQuotaGiB is the OpenZFS volume quota when reported. Nil
	// otherwise.
	StorageCapacityQuotaGiB *int32
	Tags                    map[string]string
}

// Snapshot is the scanner-owned representation of one FSx volume snapshot
// (ONTAP or OpenZFS). Snapshot data is never read.
type Snapshot struct {
	ID        string
	ARN       string
	Name      string
	VolumeID  string
	Lifecycle string
	Tags      map[string]string
}

// Backup is the scanner-owned representation of one FSx backup. It carries
// backup metadata only (type, lifecycle, size, KMS key, source file system).
// Backup contents are never read.
type Backup struct {
	ID             string
	ARN            string
	Type           string
	Lifecycle      string
	OwnerID        string
	KMSKeyID       string
	SizeInBytes    *int64
	ResourceType   string
	SourceBackupID string
	// FileSystemID is the source file system the backup was taken from, when
	// reported.
	FileSystemID  string
	FileSystemARN string
	// VolumeID is the source volume for volume-level backups, when reported.
	VolumeID string
	Tags     map[string]string
}
