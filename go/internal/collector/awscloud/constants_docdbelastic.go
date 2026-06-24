// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceDocDBElastic identifies the regional Amazon DocumentDB Elastic
	// Clusters metadata-only scan slice. The scanner reads elastic cluster
	// control-plane metadata through the docdb-elastic management APIs
	// (ListClusters, GetCluster, ListTagsForResource) and never reads document
	// contents, collections, indexes, query results, or the admin password, and
	// never mutates Elastic Cluster state. It is deliberately a separate service
	// kind from the classic instance-based DocumentDB scanner (ServiceDocDB):
	// DocumentDB Elastic is a distinct sharded service with its own API surface,
	// resource types, and ARNs.
	ServiceDocDBElastic = "docdbelastic"
)

const (
	// ResourceTypeDocDBElasticCluster identifies an Amazon DocumentDB Elastic
	// Clusters cluster metadata resource. The scanner emits identity, status,
	// auth type, shard/instance topology, retention windows, maintenance window,
	// and the KMS and admin-secret references only. It never emits the cluster
	// endpoint connection string, the admin user name in plaintext, or the admin
	// password.
	ResourceTypeDocDBElasticCluster = "aws_docdbelastic_cluster"
)

const (
	// RelationshipDocDBElasticClusterInSubnet records a DocumentDB Elastic
	// cluster's placement in one VPC subnet. The target is keyed by the bare
	// EC2 subnet id (subnet-...) so the edge joins the subnet node the EC2
	// scanner publishes.
	RelationshipDocDBElasticClusterInSubnet = "docdbelastic_cluster_in_subnet"
	// RelationshipDocDBElasticClusterUsesSecurityGroup records a DocumentDB
	// Elastic cluster's attachment to one VPC security group. The target is
	// keyed by the bare EC2 security-group id (sg-...) so the edge joins the
	// security-group node the EC2 scanner publishes.
	RelationshipDocDBElasticClusterUsesSecurityGroup = "docdbelastic_cluster_uses_security_group"
	// RelationshipDocDBElasticClusterUsesKMSKey records a DocumentDB Elastic
	// cluster's reported KMS encryption key dependency. The target is keyed by
	// the reported key id or ARN so the edge joins the KMS key node the KMS
	// scanner publishes.
	RelationshipDocDBElasticClusterUsesKMSKey = "docdbelastic_cluster_uses_kms_key"
	// RelationshipDocDBElasticClusterUsesAdminSecret records a DocumentDB
	// Elastic cluster's reference to the Secrets Manager secret that holds its
	// admin credentials, emitted only for SECRET_ARN auth. The target is keyed
	// by the secret ARN so the edge joins the secret node the Secrets Manager
	// scanner publishes. The secret value is never read.
	RelationshipDocDBElasticClusterUsesAdminSecret = "docdbelastic_cluster_uses_admin_secret"
)
