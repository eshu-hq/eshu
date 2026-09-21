// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudhsmv2

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS CloudHSM v2 cluster and backup
// observations for one AWS claim. Implementations read control-plane metadata
// through the CloudHSM v2 management APIs (DescribeClusters, DescribeBackups,
// which both return the resource tag list inline) and never read or persist
// key material, certificate private bodies, certificate signing requests, or
// the cluster's Pre-Crypto Officer password.
type Client interface {
	// Snapshot returns every CloudHSM v2 cluster and backup visible to the
	// configured AWS credentials, carrying control-plane metadata only.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures CloudHSM v2 cluster and backup metadata plus non-fatal
// scan warnings.
type Snapshot struct {
	// Clusters is the metadata-only set of CloudHSM v2 clusters.
	Clusters []Cluster
	// Backups is the metadata-only set of CloudHSM v2 backups.
	Backups []Backup
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// Cluster is the scanner-owned CloudHSM v2 cluster model. It carries
// control-plane metadata only and intentionally excludes any cryptographic key
// material, certificate PEM body, certificate signing request body, and the
// Pre-Crypto Officer password.
type Cluster struct {
	// ID is the cluster identifier (cluster-…). CloudHSM v2 clusters have no
	// API ARN, so this id is the cluster node's resource_id.
	ID string
	// State is the cluster lifecycle state (for example ACTIVE).
	State string
	// StateMessage is the human-readable description of the cluster state.
	StateMessage string
	// HsmType is the HSM hardware type the cluster contains.
	HsmType string
	// Mode is the cluster mode (for example FIPS or NON_FIPS).
	Mode string
	// NetworkType is the cluster network type (IPV4 or DUALSTACK).
	NetworkType string
	// VPCID is the bare identifier (vpc-…) of the VPC that contains the cluster.
	VPCID string
	// SecurityGroupID is the bare identifier (sg-…) of the AWS-managed cluster
	// security group, when CloudHSM reports one.
	SecurityGroupID string
	// SourceBackupID is the identifier of the backup the cluster was created
	// from, when applicable.
	SourceBackupID string
	// BackupPolicy is the reported backup policy (for example DEFAULT).
	BackupPolicy string
	// BackupRetentionType is the reported backup-retention policy type (for
	// example DAYS).
	BackupRetentionType string
	// BackupRetentionValue is the reported backup-retention policy value (for
	// the DAYS type, the number of days as a string). It is plain metadata, not
	// a secret.
	BackupRetentionValue string
	// SubnetMappings maps an availability zone to the cluster's subnet id in
	// that zone. Subnet ids are bare (subnet-…).
	SubnetMappings []SubnetMapping
	// HSMs are the metadata-only HSM ENI observations in the cluster.
	HSMs []HSM
	// CertificatePresence records which cluster/HSM certificates are present
	// without ever carrying the certificate bodies.
	CertificatePresence CertificatePresence
	// CreateTimestamp is when the cluster was created.
	CreateTimestamp time.Time
	// Tags carries the cluster resource tags.
	Tags map[string]string
}

// SubnetMapping is one availability-zone-to-subnet pairing for a cluster.
type SubnetMapping struct {
	// AvailabilityZone is the availability zone name.
	AvailabilityZone string
	// SubnetID is the bare subnet identifier (subnet-…) in that zone.
	SubnetID string
}

// HSM is the scanner-owned CloudHSM v2 hardware-security-module model. It
// carries only ENI placement metadata, never key material.
type HSM struct {
	// ID is the HSM identifier (hsm-…).
	ID string
	// State is the HSM lifecycle state.
	State string
	// AvailabilityZone is the zone that contains the HSM.
	AvailabilityZone string
	// SubnetID is the bare subnet identifier of the HSM's ENI.
	SubnetID string
	// ENIID is the elastic network interface identifier of the HSM.
	ENIID string
	// ENIIP is the IPv4 address of the HSM's ENI.
	ENIIP string
	// ENIIPV6 is the IPv6 address of the HSM's ENI, when present.
	ENIIPV6 string
}

// CertificatePresence records which cluster/HSM certificates and the cluster
// CSR are present. It NEVER carries certificate PEM bodies or the CSR body:
// only a boolean presence flag per field. The cluster CSR and HSM certificate
// fields hold cryptographic material that the scanner must not persist.
type CertificatePresence struct {
	// ClusterCertificate reports whether the cluster certificate is present.
	ClusterCertificate bool
	// ClusterCSR reports whether the cluster certificate signing request is
	// present (it exists only while the cluster is UNINITIALIZED).
	ClusterCSR bool
	// HSMCertificate reports whether the HSM certificate is present.
	HSMCertificate bool
	// AWSHardwareCertificate reports whether the CloudHSM-signed HSM hardware
	// certificate is present.
	AWSHardwareCertificate bool
	// ManufacturerHardwareCertificate reports whether the manufacturer-signed
	// HSM hardware certificate is present.
	ManufacturerHardwareCertificate bool
}

// Backup is the scanner-owned CloudHSM v2 backup model. It carries identity,
// state, and timestamp metadata only; a backup never carries key material.
type Backup struct {
	// ID is the backup identifier (backup-…).
	ID string
	// ARN is the backup ARN. CloudHSM v2 reports an ARN for backups.
	ARN string
	// State is the backup lifecycle state.
	State string
	// ClusterID is the bare identifier of the cluster that was backed up; it is
	// the backup-of-cluster edge target.
	ClusterID string
	// HsmType is the HSM type used to create the backup.
	HsmType string
	// Mode is the mode of the cluster that was backed up.
	Mode string
	// NeverExpires reports whether the backup is exempt from the cluster
	// retention policy.
	NeverExpires bool
	// SourceBackup is the identifier of the source backup this backup was
	// copied from, when applicable.
	SourceBackup string
	// SourceCluster is the identifier of the cluster containing the source
	// backup, when applicable.
	SourceCluster string
	// SourceRegion is the region containing the source backup, when applicable.
	SourceRegion string
	// CreateTimestamp is when the backup was created.
	CreateTimestamp time.Time
	// CopyTimestamp is when the backup was copied from a source backup, when
	// applicable.
	CopyTimestamp time.Time
	// DeleteTimestamp is when the backup is scheduled for permanent deletion,
	// when pending deletion.
	DeleteTimestamp time.Time
	// Tags carries the backup resource tags.
	Tags map[string]string
}
