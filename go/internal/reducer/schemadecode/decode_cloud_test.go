// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemadecode

import (
	"testing"

	awsv1 "github.com/eshu-hq/eshu-sdk/factschema/aws/v1"
	azurev1 "github.com/eshu-hq/eshu-sdk/factschema/azure/v1"
	gcpv1 "github.com/eshu-hq/eshu-sdk/factschema/gcp/v1"
	iamv1 "github.com/eshu-hq/eshu-sdk/factschema/iam/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// cloudDecodeCases covers the cloud-family seams (AWS, GCP, Azure): each
// case proves a fully-populated envelope decodes with identity values
// intact, and that removing one required field quarantines as
// input_invalid naming that field.
func cloudDecodeCases() []decodeCase {
	return []decodeCase{
		{
			name: "AWSResource", kind: "aws_resource", target: &awsv1.Resource{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeAWSResource(env) },
			requiredField: "account_id",
			spotFields:    map[string]any{"account_id": "123456789012", "region": "us-east-1", "resource_type": "aws_s3_bucket"},
		},
		{
			name: "AWSRelationship", kind: "aws_relationship", target: &awsv1.Relationship{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeAWSRelationship(env) },
			requiredField: "relationship_type",
			spotFields:    map[string]any{"account_id": "123456789012", "relationship_type": "ATTACHED_TO"},
		},
		{
			name: "AWSSecurityGroupRule", kind: "aws_security_group_rule", target: &awsv1.SecurityGroupRule{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeAWSSecurityGroupRule(env) },
			requiredField: "group_id",
			spotFields:    map[string]any{"group_id": "sg-0123456789abcdef0", "direction": "ingress", "ip_protocol": "tcp"},
		},
		{
			name: "EC2InstancePosture", kind: "ec2_instance_posture", target: &awsv1.EC2InstancePosture{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeEC2InstancePosture(env) },
			requiredField: "account_id",
			spotFields:    map[string]any{"account_id": "123456789012", "region": "us-west-2"},
		},
		{
			name: "S3BucketPosture", kind: "s3_bucket_posture", target: &awsv1.S3BucketPosture{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeS3BucketPosture(env) },
			requiredField: "region",
			spotFields:    map[string]any{"account_id": "123456789012", "region": "eu-west-1"},
		},
		{
			name: "RDSInstancePosture", kind: "rds_instance_posture", target: &awsv1.RDSInstancePosture{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeRDSInstancePosture(env) },
			requiredField: "account_id",
			spotFields:    map[string]any{"account_id": "123456789012", "region": "us-east-1", "backup_retention_period": 7},
		},
		{
			name: "S3ExternalPrincipalGrant", kind: "s3_external_principal_grant", target: &awsv1.S3ExternalPrincipalGrant{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeS3ExternalPrincipalGrant(env) },
			requiredField: "principal_value",
			spotFields:    map[string]any{"principal_kind": "CanonicalUser", "principal_value": "canonical-user-id", "grant_outcome": "allowed"},
		},
		{
			name: "AWSIAMPermission", kind: "aws_iam_permission", target: &iamv1.Permission{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeAWSIAMPermission(env) },
			requiredField: "principal_arn",
			spotFields:    map[string]any{"principal_arn": "arn:aws:iam::123456789012:role/app", "effect": "Allow"},
		},
		{
			name: "AWSResourcePolicyPermission", kind: "aws_resource_policy_permission", target: &iamv1.ResourcePolicyPermission{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeAWSResourcePolicyPermission(env) },
			requiredField: "resource_arn",
			spotFields:    map[string]any{"resource_arn": "arn:aws:s3:::example-bucket", "effect": "Deny"},
		},
		{
			name: "AWSIAMPrincipal", kind: "aws_iam_principal", target: &iamv1.Principal{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeAWSIAMPrincipal(env) },
			requiredField: "principal_type",
			spotFields:    map[string]any{"principal_arn": "arn:aws:iam::123456789012:user/deployer", "principal_type": "User"},
		},
		{
			name: "GCPCloudResource", kind: "gcp_cloud_resource", target: &gcpv1.Resource{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeGCPCloudResource(env) },
			requiredField: "full_resource_name",
			spotFields:    map[string]any{"full_resource_name": "//compute.googleapis.com/projects/p/zones/z/instances/i", "asset_type": "compute.googleapis.com/Instance"},
		},
		{
			name: "GCPCloudRelationship", kind: "gcp_cloud_relationship", target: &gcpv1.Relationship{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeGCPCloudRelationship(env) },
			requiredField: "relationship_type",
			spotFields:    map[string]any{"source_full_resource_name": "//compute.googleapis.com/projects/p/zones/z/instances/a", "relationship_type": "USES"},
		},
		{
			name: "AzureCloudResource", kind: "azure_cloud_resource", target: &azurev1.CloudResource{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeAzureCloudResource(env) },
			requiredField: "arm_resource_id",
			spotFields:    map[string]any{"arm_resource_id": "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm", "subscription_id": "sub-123"},
		},
		{
			name: "AzureCloudRelationship", kind: "azure_cloud_relationship", target: &azurev1.CloudRelationship{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeAzureCloudRelationship(env) },
			requiredField: "source_arm_resource_id",
			spotFields:    map[string]any{"relationship_type": "DEPENDS_ON", "source_arm_resource_id": "/subscriptions/s/resourceGroups/rg/providers/Microsoft.Network/virtualNetworks/vnet"},
		},
		{
			name: "AWSImageReference", kind: "aws_image_reference", target: &awsv1.ImageReference{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeAWSImageReference(env) },
			requiredField: "image_digest",
			spotFields:    map[string]any{"repository_name": "app/backend", "image_digest": "sha256:abc123"},
		},
		{
			name: "AzureImageReference", kind: "azure_image_reference", target: &azurev1.ImageReference{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeAzureImageReference(env) },
			requiredField: "owning_arm_resource_id",
			spotFields:    map[string]any{"owning_arm_resource_id": "/subscriptions/s/resourceGroups/rg/providers/Microsoft.ContainerService/managedClusters/aks", "owning_resource_type": "Microsoft.ContainerService/managedClusters"},
		},
		{
			name: "GCPImageReference", kind: "gcp_image_reference", target: &gcpv1.ImageReference{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeGCPImageReference(env) },
			requiredField: "owning_full_resource_name",
			spotFields:    map[string]any{"owning_full_resource_name": "//container.googleapis.com/projects/p/locations/z/clusters/c"},
		},
	}
}

func TestCloudDecodeSeams(t *testing.T) {
	t.Parallel()
	runDecodeCases(t, cloudDecodeCases())
}
