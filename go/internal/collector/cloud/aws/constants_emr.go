// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceEMR identifies the regional Amazon EMR metadata-only scan slice.
	// One service kind covers EMR on EC2 clusters, EMR Serverless
	// applications, and EMR Studio; resource types distinguish the three
	// sub-surfaces. The scanner never invokes job, step, or lifecycle mutation
	// APIs (RunJobFlow, TerminateJobFlows, AddJobFlowSteps, CancelSteps,
	// ModifyInstanceGroups, ModifyInstanceFleet, StartJobRun, CancelJobRun,
	// Create/Delete/Start/Stop Application, Create/Delete/Update Studio or
	// StudioSessionMapping) and never persists step command lines, bootstrap
	// action script bodies, security configuration policy bodies, or Serverless
	// job-run entry-point arguments.
	ServiceEMR = "emr"
)

const (
	// ResourceTypeEMRCluster identifies an EMR on EC2 cluster metadata
	// resource. The scanner emits running and recently terminated clusters.
	ResourceTypeEMRCluster = "aws_emr_cluster"
	// ResourceTypeEMRInstanceGroup identifies an EMR uniform instance group.
	ResourceTypeEMRInstanceGroup = "aws_emr_instance_group"
	// ResourceTypeEMRInstanceFleet identifies an EMR instance fleet.
	ResourceTypeEMRInstanceFleet = "aws_emr_instance_fleet"
	// ResourceTypeEMRSecurityConfiguration identifies an EMR security
	// configuration metadata resource. Only the name and creation time are
	// emitted; the encryption and authentication policy body is never
	// persisted.
	ResourceTypeEMRSecurityConfiguration = "aws_emr_security_configuration"
	// ResourceTypeEMRServerlessApplication identifies an EMR Serverless
	// application metadata resource. The wire string uses the
	// "<service>_serverless_<kind>" shape shared with Redshift Serverless
	// (aws_redshift_serverless_namespace) rather than the AWS service id
	// "emrserverless".
	ResourceTypeEMRServerlessApplication = "aws_emr_serverless_application"
	// ResourceTypeEMRStudio identifies an EMR Studio metadata resource.
	ResourceTypeEMRStudio = "aws_emr_studio"
	// ResourceTypeEMRStudioSessionMapping identifies an EMR Studio
	// session-mapping metadata resource (identity binding only; the session
	// policy ARN is recorded as a reference, never the policy body).
	ResourceTypeEMRStudioSessionMapping = "aws_emr_studio_session_mapping"
)

const (
	// RelationshipEMRClusterUsesSubnet records the EC2 subnet an EMR on EC2
	// cluster places its instances in. The cluster-to-VPC join is derived from
	// subnet membership downstream; the EMR cluster API does not report a VPC
	// id directly.
	RelationshipEMRClusterUsesSubnet = "emr_cluster_uses_subnet"
	// RelationshipEMRClusterUsesSecurityGroup records an EC2 security group
	// associated with an EMR on EC2 cluster (managed and additional groups for
	// master, slave, and service access).
	RelationshipEMRClusterUsesSecurityGroup = "emr_cluster_uses_security_group"
	// RelationshipEMRClusterUsesIAMRole records an IAM role used by an EMR on
	// EC2 cluster (EMR service role and auto-scaling role).
	RelationshipEMRClusterUsesIAMRole = "emr_cluster_uses_iam_role"
	// RelationshipEMRClusterUsesInstanceProfile records the EC2 instance
	// profile an EMR on EC2 cluster assigns to its instances.
	RelationshipEMRClusterUsesInstanceProfile = "emr_cluster_uses_instance_profile"
	// RelationshipEMRClusterUsesSecurityConfiguration records the named EMR
	// security configuration an EMR on EC2 cluster references.
	RelationshipEMRClusterUsesSecurityConfiguration = "emr_cluster_uses_security_configuration"
	// RelationshipEMRClusterUsesKMSKey records the KMS key an EMR on EC2
	// cluster uses for log encryption.
	RelationshipEMRClusterUsesKMSKey = "emr_cluster_uses_kms_key"
	// RelationshipEMRClusterHasInstanceGroup records uniform instance group
	// membership on an EMR on EC2 cluster.
	RelationshipEMRClusterHasInstanceGroup = "emr_cluster_has_instance_group"
	// RelationshipEMRClusterHasInstanceFleet records instance fleet membership
	// on an EMR on EC2 cluster.
	RelationshipEMRClusterHasInstanceFleet = "emr_cluster_has_instance_fleet"
	// RelationshipEMRServerlessApplicationUsesSubnet records an EC2 subnet in
	// an EMR Serverless application's network configuration. The
	// application-to-VPC join is derived from subnet membership downstream; the
	// EMR Serverless API does not report a VPC id directly.
	RelationshipEMRServerlessApplicationUsesSubnet = "emr_serverless_application_uses_subnet"
	// RelationshipEMRServerlessApplicationUsesSecurityGroup records an EC2
	// security group in an EMR Serverless application's network configuration.
	RelationshipEMRServerlessApplicationUsesSecurityGroup = "emr_serverless_application_uses_security_group"
	// RelationshipEMRServerlessApplicationUsesKMSKey records the KMS key an EMR
	// Serverless application uses for disk encryption.
	RelationshipEMRServerlessApplicationUsesKMSKey = "emr_serverless_application_uses_kms_key"
	// RelationshipEMRStudioInVPC records the VPC an EMR Studio is attached to.
	RelationshipEMRStudioInVPC = "emr_studio_in_vpc"
	// RelationshipEMRStudioUsesSubnet records an EC2 subnet an EMR Studio is
	// attached to.
	RelationshipEMRStudioUsesSubnet = "emr_studio_uses_subnet"
	// RelationshipEMRStudioUsesSecurityGroup records an EC2 security group an
	// EMR Studio uses (engine and workspace security groups).
	RelationshipEMRStudioUsesSecurityGroup = "emr_studio_uses_security_group"
	// RelationshipEMRStudioUsesIAMRole records an IAM role an EMR Studio uses
	// (service role and user role).
	RelationshipEMRStudioUsesIAMRole = "emr_studio_uses_iam_role"
	// RelationshipEMRStudioUsesKMSKey records the KMS key an EMR Studio uses
	// for workspace encryption.
	RelationshipEMRStudioUsesKMSKey = "emr_studio_uses_kms_key"
	// RelationshipEMRStudioHasSessionMapping records session-mapping membership
	// on an EMR Studio.
	RelationshipEMRStudioHasSessionMapping = "emr_studio_has_session_mapping"
)
