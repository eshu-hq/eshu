// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package transfer

import "context"

// Client lists metadata-only AWS Transfer Family observations for one claimed
// account and region. Implementations never read host keys, SSH public key
// bodies, user policy JSON, or any credential material.
type Client interface {
	// ListServers returns the Transfer Family servers in the boundary, each
	// enriched with safe DescribeServer metadata.
	ListServers(ctx context.Context) ([]Server, error)
	// ListUsers returns the service-managed users across the boundary's
	// servers, each enriched with safe DescribeUser metadata. The user carries
	// its parent server ID so the scanner can scope identities.
	ListUsers(ctx context.Context) ([]User, error)
}

// Server is the scanner-owned AWS Transfer Family server view. Host key
// fingerprints, host key material, pre/post authentication login banners, and
// identity-provider invocation secrets stay outside the contract. Only the VPC
// endpoint ID, address allocation IDs, FTPS certificate ARN, logging role ARN,
// and structured-log destination ARNs survive as relationship anchors.
type Server struct {
	ARN                  string
	ServerID             string
	Domain               string
	EndpointType         string
	IdentityProviderType string
	State                string
	Protocols            []string
	UserCount            int32
	SecurityPolicyName   string
	IPAddressType        string

	// VPCEndpointID is the AWS-reported interface VPC endpoint ID (vpce-...). It
	// is set only when the server's EndpointType is VPC_ENDPOINT (the legacy
	// interface-endpoint mode); it is empty for the PUBLIC and VPC endpoint
	// types.
	VPCEndpointID string
	// VPCID is the AWS-reported VPC ID for VPC-hosted endpoints.
	VPCID string
	// AddressAllocationIDs are the AWS-reported Elastic IP allocation IDs
	// (eipalloc-...) attached to a VPC endpoint server.
	AddressAllocationIDs []string
	// SubnetIDs are the AWS-reported subnet IDs the VPC endpoint spans.
	SubnetIDs []string
	// SecurityGroupIDs are the AWS-reported security group IDs the VPC
	// endpoint uses.
	SecurityGroupIDs []string

	// CertificateARN is the AWS-reported ACM certificate ARN backing FTPS.
	// Empty when FTPS is not enabled.
	CertificateARN string
	// LoggingRoleARN is the AWS-reported IAM role ARN used for CloudWatch
	// logging.
	LoggingRoleARN string
	// StructuredLogDestinations are the AWS-reported CloudWatch Logs log group
	// ARNs receiving structured logs.
	StructuredLogDestinations []string
}

// User is the scanner-owned AWS Transfer Family service-managed user view. SSH
// public key bodies, user policy JSON, and POSIX UID/GID credential material
// stay outside the contract. Home-directory mappings are recorded as paths
// only.
type User struct {
	ServerID          string
	ARN               string
	UserName          string
	HomeDirectory     string
	HomeDirectoryType string
	RoleARN           string
	// HomeDirectoryMappings are the LOGICAL home-directory mapping entries.
	// Each carries the virtual entry path and the backing target path only.
	HomeDirectoryMappings []HomeDirectoryMapping
}

// HomeDirectoryMapping is one LOGICAL home-directory mapping entry. Entry is
// the virtual path the client sees; Target is the backing S3 or EFS path. Both
// are paths, never object or file contents.
type HomeDirectoryMapping struct {
	Entry  string
	Target string
}
