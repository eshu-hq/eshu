// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dax

import "context"

// Client lists Amazon DAX metadata for one claimed account and region. It is the
// scanner-facing surface that adapter packages implement; the contract is
// intentionally narrow so the scanner cannot reach for cached DynamoDB items,
// query results, node endpoint reads, or any mutation API.
type Client interface {
	// ListClusters returns DAX cluster metadata, including resource tags.
	ListClusters(ctx context.Context) ([]Cluster, error)
	// ListSubnetGroups returns DAX subnet group metadata. DAX subnet groups
	// have no ARN, so they are keyed by name.
	ListSubnetGroups(ctx context.Context) ([]SubnetGroup, error)
	// ListParameterGroups returns DAX parameter group metadata. Only the group
	// name and description are read; individual parameter values are never
	// fetched.
	ListParameterGroups(ctx context.Context) ([]ParameterGroup, error)
}

// Cluster is the scanner-owned DAX cluster model. It carries control-plane
// metadata only and intentionally excludes cached DynamoDB item data, query
// results, and any payload material. The discovery endpoint address is recorded
// as plain connection metadata, not as a secret. DAX does not expose a
// server-side-encryption KMS key ARN through DescribeClusters, so the scanner
// records only the SSE status and never synthesizes a KMS key reference.
type Cluster struct {
	// ARN uniquely identifies the cluster and is preferred as the resource_id.
	ARN string
	// Name is the DAX cluster name.
	Name string
	// Description is the operator-supplied cluster description.
	Description string
	// Status is the reported cluster lifecycle state (for example "available").
	Status string
	// NodeType is the EC2 node type that backs the cluster nodes.
	NodeType string
	// ActiveNodes is the number of nodes currently able to serve requests.
	ActiveNodes int32
	// TotalNodes is the configured number of nodes in the cluster.
	TotalNodes int32
	// NetworkType is the cluster IP address type (ipv4, ipv6, or dual_stack).
	NetworkType string
	// EndpointEncryptionType reports whether the cluster discovery endpoint uses
	// TLS. It is a non-secret transport posture signal, never a key.
	EndpointEncryptionType string
	// IAMRoleARN is the role DAX assumes at runtime to access DynamoDB.
	IAMRoleARN string
	// ParameterGroupName is the parameter group the cluster nodes use.
	ParameterGroupName string
	// SubnetGroupName is the subnet group the cluster is deployed into.
	SubnetGroupName string
	// SecurityGroupIDs are the VPC security group ids attached to the nodes.
	SecurityGroupIDs []string
	// SSEStatus is the reported server-side-encryption state (for example
	// "ENABLED"). DAX does not return the KMS key ARN, so no KMS edge is emitted.
	SSEStatus string
	// PreferredMaintenanceWindow is the weekly maintenance window.
	PreferredMaintenanceWindow string
	// DiscoveryEndpointAddress is the cluster discovery endpoint DNS name. It is
	// plain connection metadata, not a secret.
	DiscoveryEndpointAddress string
	// DiscoveryEndpointPort is the cluster discovery endpoint port.
	DiscoveryEndpointPort int32
	// Tags are raw AWS resource tags.
	Tags map[string]string
}

// SubnetGroup is the scanner-owned DAX subnet group model. DAX subnet groups
// have no ARN, so the scanner keys the resource by name and records the VPC id
// and member subnet ids it reports.
type SubnetGroup struct {
	// Name is the subnet group name and its resource_id (DAX subnet groups have
	// no ARN).
	Name string
	// Description is the operator-supplied subnet group description.
	Description string
	// VPCID is the bare VPC id (vpc-...) the subnet group belongs to.
	VPCID string
	// SubnetIDs are the bare member subnet ids (subnet-...).
	SubnetIDs []string
}

// ParameterGroup is the scanner-owned DAX parameter group model.
// DescribeParameterGroups exposes only the name and description; individual
// parameter values can reveal operational posture and stay outside this type by
// design (DescribeParameters is never called).
type ParameterGroup struct {
	// Name is the parameter group name and its resource_id (DAX parameter groups
	// have no ARN).
	Name string
	// Description is the operator-supplied parameter group description.
	Description string
}
