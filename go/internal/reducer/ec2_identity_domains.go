// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

const (
	// DomainEC2InstanceIdentityMaterialization projects the #5448 aws_ec2_instance
	// aws_resource fact's ami_id onto the already-materialized EC2 instance
	// CloudResource node. It is AUGMENT-ONLY on the existing cloud_resource_uid
	// keyspace (no new node type, never creates): the node itself is owned by
	// DomainEC2InstanceNodeMaterialization (#1146 PR-A), which publishes its own
	// canonical_nodes_committed phase under a distinct entity key
	// ("ec2_instance_node_materialization:<scope>"). This domain gates on that
	// exact phase — mirroring DomainRDSPostureMaterialization's single-key gate
	// on DomainAWSResourceMaterialization's phase — and writes only the disjoint
	// ami_id / ec2_identity_* properties, never the base identity/posture fields
	// the node-owning domain already sets.
	DomainEC2InstanceIdentityMaterialization Domain = "ec2_instance_identity_materialization"
)
