// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storagegateway

import "context"

// Client lists metadata-only AWS Storage Gateway observations for one claimed
// account and region. It exposes only read APIs; gateway activation, deletion,
// cache refresh, and volume/share creation stay outside the interface.
type Client interface {
	// ListGateways returns the gateways in the claimed boundary, each enriched
	// with the safe subset of DescribeGatewayInformation.
	ListGateways(ctx context.Context) ([]Gateway, error)
	// ListVolumes returns the cached/stored iSCSI volumes for the gateways in
	// the claimed boundary.
	ListVolumes(ctx context.Context) ([]Volume, error)
	// ListFileShares returns the NFS and SMB S3 file shares for the gateways in
	// the claimed boundary, each enriched with the safe subset of the matching
	// Describe*FileShares response.
	ListFileShares(ctx context.Context) ([]FileShare, error)
}

// Gateway is the scanner-owned Storage Gateway view. It carries gateway
// identity, type, state, endpoint type, and the activation VPC endpoint and
// audit log group reported by DescribeGatewayInformation. Network-interface IP
// addresses, host environment IDs, and software-update internals stay outside
// the contract.
type Gateway struct {
	ARN                   string
	ID                    string
	Name                  string
	Type                  string
	State                 string
	OperationalState      string
	EndpointType          string
	HostEnvironment       string
	Timezone              string
	EC2InstanceID         string
	EC2InstanceRegion     string
	SoftwareVersion       string
	CloudWatchLogGroup    string
	VPCEndpoint           string
	Tags                  map[string]string
	NetworkInterfaceCount int
}

// Volume is the scanner-owned Storage Gateway iSCSI volume view. It records
// volume identity, type, size, attachment status, and the parent gateway ARN.
type Volume struct {
	ARN              string
	ID               string
	Type             string
	SizeInBytes      int64
	AttachmentStatus string
	GatewayARN       string
	GatewayID        string
}

// FileShare is the scanner-owned Storage Gateway S3 file share view across the
// NFS and SMB protocols. It records share identity, type, status, the parent
// gateway ARN, and the reported S3 location, IAM role, KMS key, and audit log
// group ARNs. Client allow lists, admin/user lists, and object contents stay
// outside the contract.
type FileShare struct {
	ARN                 string
	ID                  string
	Name                string
	Protocol            string
	Type                string
	Status              string
	GatewayARN          string
	LocationARN         string
	BucketRegion        string
	Role                string
	KMSKey              string
	EncryptionType      string
	DefaultStorageClass string
	ObjectACL           string
	AuditDestinationARN string
	ReadOnly            bool
}
