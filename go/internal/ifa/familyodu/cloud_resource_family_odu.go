// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
	azurev1 "github.com/eshu-hq/eshu/sdk/go/factschema/azure/v1"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
)

// The cloud_resource family Odù (#8358, route 2 of the #6228 arbiter
// verdict's follow-up).
//
// A DIRECT-materialization, PREDICATE-SCOPED family: three reducer handlers
// (AWS, GCP, Azure relationship materialization) write through one port name,
// WriteCloudResourceEdges, to three writers that each prefix an open
// per-provider verb vocabulary (AWS_<verb>, GCP_<verb>, AZURE_<verb>). No
// finite edge-type set exists to register, so coverage scopes by the
// predicate the writers guarantee — type prefix + the handler's
// evidence_source + CloudResource endpoints — registered in
// cypher/edge/materialized/predicate_families.go.
//
// The Odù therefore carries one scope generation per provider (the reducer
// handlers each load one scope generation's facts), with the resource facts
// each join index resolves against and the relationship facts that drive
// emission. Every provider contributes at least one materialized edge and at
// least one deliberate non-edge, so the guard proves resolution behaviour
// per writer, not just union cardinality:
//
//   - AWS: two distinct verbs from one source (the open vocabulary is the
//     family's whole point — a second verb is a second edge, never a
//     collapsed or rejected one), a duplicate fact the extractor dedups, an
//     unresolved target ARN, and a self-loop. Exactly two edges.
//   - GCP: one supported relationship, one partial (target treated as
//     unresolved), one supported-but-unresolved. Exactly one edge.
//   - Azure: one managed_by relationship and one unsupported verb the
//     extractor screens out (Azure's vocabulary is closed inside the open
//     family). Exactly one edge.
//
// Facts are built as typed factschema values and encoded through the
// contracts seam, never hand-built maps (Contract System v1).

const (
	// CloudResourceFamilyOduName is this Odù's catalog name, the ref a
	// materialized_edges:cloud_resource coverage row names.
	CloudResourceFamilyOduName = "odu:ifa-cloud-resource-family"

	// cloudResourceFamilyGenerationID is the one generation every scope in
	// the Odù replays. The reducer handlers load a single scope
	// generation's facts per intent, so each provider's facts share it
	// within their own scope.
	cloudResourceFamilyGenerationID = "gen-ifa-cloud-resource-family-1"

	// Per-provider scope ids. Each mirrors the scope shape its collector
	// stamps: an AWS account scope, a GCP project scope, an Azure
	// subscription scope.
	cloudResourceFamilyAWSScopeID   = "aws:eshu-fixture-cloud-resource-account"
	cloudResourceFamilyGCPScopeID   = "gcp:project:eshu-fixture-project"
	cloudResourceFamilyAzureScopeID = "azure:eshu-fixture-tenant:subscription:eshu-fixture-rg"

	cloudResourceFamilyAWSAccountID = "123456789012"
	cloudResourceFamilyAWSRegion    = "us-east-1"

	cloudResourceFamilyGCPProjectID = "eshu-fixture-project"
	cloudResourceFamilyGCPLocation  = "us-central1-a"

	cloudResourceFamilyAzureSubscriptionID = "eshu-fixture-subscription"
	cloudResourceFamilyAzureLocation       = "eastus"

	cloudResourceFamilySourceConfidence = "observed"
)

// AWS fixture identities. Resource ids and ARNs are both carried because the
// join index keys resources by each and relationship resolution prefers the
// ARN index (resolution_mode "arn").
const (
	cloudResourceFamilyAWSFunctionID  = "eshu-fixture-fn"
	cloudResourceFamilyAWSFunctionARN = "arn:aws:lambda:us-east-1:123456789012:function:eshu-fixture-fn"
	cloudResourceFamilyAWSRoleID      = "eshu-fixture-fn-role"
	cloudResourceFamilyAWSRoleARN     = "arn:aws:iam::123456789012:role/eshu-fixture-fn-role"
	cloudResourceFamilyAWSSubnetID    = "subnet-eshufixt1"
	cloudResourceFamilyAWSSubnetARN   = "arn:aws:ec2:us-east-1:123456789012:subnet/subnet-eshufixt1"
	// cloudResourceFamilyAWSMissingSubnetARN is deliberately never declared
	// by an aws_resource fact: the relationship naming it must resolve to
	// nothing.
	cloudResourceFamilyAWSMissingSubnetARN = "arn:aws:ec2:us-east-1:123456789012:subnet/subnet-neverscanned"
)

// GCP fixture identities: full resource names are the join keys and the
// resource_id half of the CloudResource uid.
const (
	cloudResourceFamilyGCPInstanceName = "//compute.googleapis.com/projects/eshu-fixture-project/zones/us-central1-a/instances/eshu-fixture-vm"
	cloudResourceFamilyGCPDiskName     = "//compute.googleapis.com/projects/eshu-fixture-project/zones/us-central1-a/disks/eshu-fixture-disk"
	// cloudResourceFamilyGCPMissingDiskName is never declared by a
	// gcp_cloud_resource fact.
	cloudResourceFamilyGCPMissingDiskName = "//compute.googleapis.com/projects/eshu-fixture-project/zones/us-central1-a/disks/never-scanned-disk"
	cloudResourceFamilyGCPPartialDiskName = "//compute.googleapis.com/projects/eshu-fixture-project/zones/us-central1-a/disks/partial-disk"

	cloudResourceFamilyGCPInstanceType = "compute.googleapis.com/Instance"
	cloudResourceFamilyGCPDiskType     = "compute.googleapis.com/Disk"
)

// Azure fixture identities: ARM ids, with the normalized (lower-cased) form
// the reducer prefers for uid derivation and join resolution.
const (
	cloudResourceFamilyAzureVMARMID            = "/subscriptions/eshu-fixture-subscription/resourceGroups/eshu-fixture-rg/providers/Microsoft.Compute/virtualMachines/eshu-fixture-vm"
	cloudResourceFamilyAzureVMNormalizedID     = "/subscriptions/eshu-fixture-subscription/resourcegroups/eshu-fixture-rg/providers/microsoft.compute/virtualmachines/eshu-fixture-vm"
	cloudResourceFamilyAzureVMType             = "microsoft.compute/virtualmachines"
	cloudResourceFamilyAzureIdentityARMID      = "/subscriptions/eshu-fixture-subscription/resourceGroups/eshu-fixture-rg/providers/Microsoft.ManagedIdentity/userAssignedIdentities/eshu-fixture-id"
	cloudResourceFamilyAzureIdentityNormalized = "/subscriptions/eshu-fixture-subscription/resourcegroups/eshu-fixture-rg/providers/microsoft.managedidentity/userassignedidentities/eshu-fixture-id"
	cloudResourceFamilyAzureIdentityType       = "microsoft.managedidentity/userassignedidentities"
)

// cloudResourceFamilyPtr returns a pointer to v for the typed structs'
// optional string fields.
func cloudResourceFamilyPtr(v string) *string { return &v }

// cloudResourceFamilyAWSFacts builds the AWS scope's facts: three resources
// and five relationships (two materializing verbs, one duplicate, one
// unresolved target, one self-loop).
func cloudResourceFamilyAWSFacts() []facts.Envelope {
	resources := []awsv1.Resource{
		{AccountID: cloudResourceFamilyAWSAccountID, Region: cloudResourceFamilyAWSRegion, ResourceType: awsv1.ResourceTypeLambdaFunction, ResourceID: cloudResourceFamilyAWSFunctionID, ARN: cloudResourceFamilyPtr(cloudResourceFamilyAWSFunctionARN)},
		{AccountID: cloudResourceFamilyAWSAccountID, Region: cloudResourceFamilyAWSRegion, ResourceType: awsv1.ResourceTypeIAMRole, ResourceID: cloudResourceFamilyAWSRoleID, ARN: cloudResourceFamilyPtr(cloudResourceFamilyAWSRoleARN)},
		{AccountID: cloudResourceFamilyAWSAccountID, Region: cloudResourceFamilyAWSRegion, ResourceType: awsv1.ResourceTypeEC2Subnet, ResourceID: cloudResourceFamilyAWSSubnetID, ARN: cloudResourceFamilyPtr(cloudResourceFamilyAWSSubnetARN)},
	}
	relationships := []awsv1.Relationship{
		{AccountID: cloudResourceFamilyAWSAccountID, Region: cloudResourceFamilyAWSRegion, RelationshipType: "lambda_function_uses_execution_role", SourceResourceID: cloudResourceFamilyAWSFunctionID, SourceARN: cloudResourceFamilyPtr(cloudResourceFamilyAWSFunctionARN), TargetResourceID: cloudResourceFamilyAWSRoleID, TargetARN: cloudResourceFamilyPtr(cloudResourceFamilyAWSRoleARN), TargetType: cloudResourceFamilyPtr(awsv1.ResourceTypeIAMRole)},
		// DUPLICATE of the first relationship: the extractor dedups by
		// (source_uid, relationship_type, target_uid), so this must not
		// produce a second edge.
		{AccountID: cloudResourceFamilyAWSAccountID, Region: cloudResourceFamilyAWSRegion, RelationshipType: "lambda_function_uses_execution_role", SourceResourceID: cloudResourceFamilyAWSFunctionID, SourceARN: cloudResourceFamilyPtr(cloudResourceFamilyAWSFunctionARN), TargetResourceID: cloudResourceFamilyAWSRoleID, TargetARN: cloudResourceFamilyPtr(cloudResourceFamilyAWSRoleARN), TargetType: cloudResourceFamilyPtr(awsv1.ResourceTypeIAMRole)},
		// SECOND VERB, same source: the open-vocabulary half of the family.
		{AccountID: cloudResourceFamilyAWSAccountID, Region: cloudResourceFamilyAWSRegion, RelationshipType: "lambda_function_uses_subnet", SourceResourceID: cloudResourceFamilyAWSFunctionID, SourceARN: cloudResourceFamilyPtr(cloudResourceFamilyAWSFunctionARN), TargetResourceID: cloudResourceFamilyAWSSubnetID, TargetARN: cloudResourceFamilyPtr(cloudResourceFamilyAWSSubnetARN), TargetType: cloudResourceFamilyPtr(awsv1.ResourceTypeEC2Subnet)},
		// UNRESOLVED TARGET: no aws_resource fact declares this subnet.
		{AccountID: cloudResourceFamilyAWSAccountID, Region: cloudResourceFamilyAWSRegion, RelationshipType: "lambda_function_uses_subnet", SourceResourceID: cloudResourceFamilyAWSFunctionID, SourceARN: cloudResourceFamilyPtr(cloudResourceFamilyAWSFunctionARN), TargetResourceID: "subnet-neverscanned", TargetARN: cloudResourceFamilyPtr(cloudResourceFamilyAWSMissingSubnetARN), TargetType: cloudResourceFamilyPtr(awsv1.ResourceTypeEC2Subnet)},
		// SELF-LOOP: both endpoints resolve to the function itself; the
		// extractor skips it without counting it unresolved.
		{AccountID: cloudResourceFamilyAWSAccountID, Region: cloudResourceFamilyAWSRegion, RelationshipType: "lambda_function_uses_execution_role", SourceResourceID: cloudResourceFamilyAWSFunctionID, SourceARN: cloudResourceFamilyPtr(cloudResourceFamilyAWSFunctionARN), TargetResourceID: cloudResourceFamilyAWSFunctionID, TargetARN: cloudResourceFamilyPtr(cloudResourceFamilyAWSFunctionARN), TargetType: cloudResourceFamilyPtr(awsv1.ResourceTypeLambdaFunction)},
	}

	out := make([]facts.Envelope, 0, len(resources)+len(relationships))
	for _, resource := range resources {
		payload, err := factschema.EncodeAWSResource(resource)
		if err != nil {
			panic(fmt.Sprintf("familyodu: catalog_seed %s: encode aws_resource %q: %v", CloudResourceFamilyOduName, resource.ResourceID, err))
		}
		out = append(out, facts.Envelope{
			ScopeID:          cloudResourceFamilyAWSScopeID,
			GenerationID:     cloudResourceFamilyGenerationID,
			FactKind:         cloud.AWSResourceFactKind,
			StableFactKey:    fmt.Sprintf("aws:%s:%s:%s:%s", cloudResourceFamilyAWSAccountID, cloudResourceFamilyAWSRegion, resource.ResourceType, resource.ResourceID),
			SchemaVersion:    cloud.AWSResourceSchemaVersion,
			CollectorKind:    "aws",
			SourceConfidence: cloudResourceFamilySourceConfidence,
			Payload:          payload,
		})
	}
	for i, relationship := range relationships {
		payload, err := factschema.EncodeAWSRelationship(relationship)
		if err != nil {
			panic(fmt.Sprintf("familyodu: catalog_seed %s: encode aws_relationship %d: %v", CloudResourceFamilyOduName, i, err))
		}
		out = append(out, facts.Envelope{
			ScopeID:          cloudResourceFamilyAWSScopeID,
			GenerationID:     cloudResourceFamilyGenerationID,
			FactKind:         cloud.AWSRelationshipFactKind,
			StableFactKey:    fmt.Sprintf("aws:%s:%s:relationship:%s:%s->%s:%d", cloudResourceFamilyAWSAccountID, cloudResourceFamilyAWSRegion, relationship.RelationshipType, relationship.SourceResourceID, relationship.TargetResourceID, i),
			SchemaVersion:    cloud.AWSRelationshipSchemaVersion,
			CollectorKind:    "aws",
			SourceConfidence: cloudResourceFamilySourceConfidence,
			Payload:          payload,
		})
	}
	return out
}

// cloudResourceFamilyGCPFacts builds the GCP scope's facts: two resources
// and three relationships (one supported, one partial, one unresolved).
func cloudResourceFamilyGCPFacts() []facts.Envelope {
	resources := []gcpv1.Resource{
		{FullResourceName: cloudResourceFamilyGCPInstanceName, AssetType: cloudResourceFamilyGCPInstanceType, ProjectID: cloudResourceFamilyPtr(cloudResourceFamilyGCPProjectID), Location: cloudResourceFamilyPtr(cloudResourceFamilyGCPLocation)},
		{FullResourceName: cloudResourceFamilyGCPDiskName, AssetType: cloudResourceFamilyGCPDiskType, ProjectID: cloudResourceFamilyPtr(cloudResourceFamilyGCPProjectID), Location: cloudResourceFamilyPtr(cloudResourceFamilyGCPLocation)},
	}
	relationships := []gcpv1.Relationship{
		{SourceFullResourceName: cloudResourceFamilyGCPInstanceName, TargetFullResourceName: cloudResourceFamilyGCPDiskName, RelationshipType: "instance_uses_disk", SourceAssetType: cloudResourceFamilyPtr(cloudResourceFamilyGCPInstanceType), TargetAssetType: cloudResourceFamilyPtr(cloudResourceFamilyGCPDiskType), SupportState: cloudResourceFamilyPtr("supported")},
		// PARTIAL: the provider described the relationship incompletely, so
		// the target is treated as unresolved and no edge materializes.
		{SourceFullResourceName: cloudResourceFamilyGCPInstanceName, TargetFullResourceName: cloudResourceFamilyGCPPartialDiskName, RelationshipType: "instance_uses_disk", SourceAssetType: cloudResourceFamilyPtr(cloudResourceFamilyGCPInstanceType), TargetAssetType: cloudResourceFamilyPtr(cloudResourceFamilyGCPDiskType), SupportState: cloudResourceFamilyPtr("partial")},
		// UNRESOLVED TARGET: supported, but no gcp_cloud_resource fact
		// declares the disk.
		{SourceFullResourceName: cloudResourceFamilyGCPInstanceName, TargetFullResourceName: cloudResourceFamilyGCPMissingDiskName, RelationshipType: "instance_uses_disk", SourceAssetType: cloudResourceFamilyPtr(cloudResourceFamilyGCPInstanceType), TargetAssetType: cloudResourceFamilyPtr(cloudResourceFamilyGCPDiskType), SupportState: cloudResourceFamilyPtr("supported")},
	}

	out := make([]facts.Envelope, 0, len(resources)+len(relationships))
	for _, resource := range resources {
		payload, err := factschema.EncodeGCPCloudResource(resource)
		if err != nil {
			panic(fmt.Sprintf("familyodu: catalog_seed %s: encode gcp_cloud_resource %q: %v", CloudResourceFamilyOduName, resource.FullResourceName, err))
		}
		out = append(out, facts.Envelope{
			ScopeID:          cloudResourceFamilyGCPScopeID,
			GenerationID:     cloudResourceFamilyGenerationID,
			FactKind:         cloud.GCPCloudResourceFactKind,
			StableFactKey:    fmt.Sprintf("gcp:project:%s:%s:%s", cloudResourceFamilyGCPProjectID, resource.AssetType, resource.FullResourceName),
			SchemaVersion:    cloud.GCPCloudResourceSchemaVersion,
			CollectorKind:    "gcp",
			SourceConfidence: cloudResourceFamilySourceConfidence,
			Payload:          payload,
		})
	}
	for i, relationship := range relationships {
		payload, err := factschema.EncodeGCPCloudRelationship(relationship)
		if err != nil {
			panic(fmt.Sprintf("familyodu: catalog_seed %s: encode gcp_cloud_relationship %d: %v", CloudResourceFamilyOduName, i, err))
		}
		out = append(out, facts.Envelope{
			ScopeID:          cloudResourceFamilyGCPScopeID,
			GenerationID:     cloudResourceFamilyGenerationID,
			FactKind:         cloud.GCPCloudRelationshipFactKind,
			StableFactKey:    fmt.Sprintf("gcp:project:%s:relationship:%s:%s->%s:%d", cloudResourceFamilyGCPProjectID, relationship.RelationshipType, relationship.SourceFullResourceName, relationship.TargetFullResourceName, i),
			SchemaVersion:    cloud.GCPCloudRelationshipSchemaVersion,
			CollectorKind:    "gcp",
			SourceConfidence: cloudResourceFamilySourceConfidence,
			Payload:          payload,
		})
	}
	return out
}

// cloudResourceFamilyAzureFacts builds the Azure scope's facts: two
// resources and two relationships (one managed_by, one unsupported verb).
func cloudResourceFamilyAzureFacts() []facts.Envelope {
	resources := []azurev1.CloudResource{
		{ARMResourceID: cloudResourceFamilyAzureVMARMID, ResourceType: cloudResourceFamilyAzureVMType, SubscriptionID: cloudResourceFamilyAzureSubscriptionID, Location: cloudResourceFamilyAzureLocation, NormalizedResourceID: cloudResourceFamilyPtr(cloudResourceFamilyAzureVMNormalizedID)},
		{ARMResourceID: cloudResourceFamilyAzureIdentityARMID, ResourceType: cloudResourceFamilyAzureIdentityType, SubscriptionID: cloudResourceFamilyAzureSubscriptionID, Location: cloudResourceFamilyAzureLocation, NormalizedResourceID: cloudResourceFamilyPtr(cloudResourceFamilyAzureIdentityNormalized)},
	}
	relationships := []azurev1.CloudRelationship{
		{RelationshipType: "managed_by", SourceARMResourceID: cloudResourceFamilyAzureVMARMID, TargetARMResourceID: cloudResourceFamilyAzureIdentityARMID, SourceNormalizedResourceID: cloudResourceFamilyPtr(cloudResourceFamilyAzureVMNormalizedID), TargetNormalizedResourceID: cloudResourceFamilyPtr(cloudResourceFamilyAzureIdentityNormalized), TargetResourceType: cloudResourceFamilyPtr(cloudResourceFamilyAzureIdentityType), SupportState: cloudResourceFamilyPtr("supported")},
		// UNSUPPORTED VERB: Azure's vocabulary is closed at managed_by, so
		// the extractor screens this out even though both endpoints resolve.
		{RelationshipType: "depends_on", SourceARMResourceID: cloudResourceFamilyAzureVMARMID, TargetARMResourceID: cloudResourceFamilyAzureIdentityARMID, SourceNormalizedResourceID: cloudResourceFamilyPtr(cloudResourceFamilyAzureVMNormalizedID), TargetNormalizedResourceID: cloudResourceFamilyPtr(cloudResourceFamilyAzureIdentityNormalized), TargetResourceType: cloudResourceFamilyPtr(cloudResourceFamilyAzureIdentityType), SupportState: cloudResourceFamilyPtr("supported")},
	}

	out := make([]facts.Envelope, 0, len(resources)+len(relationships))
	for _, resource := range resources {
		payload, err := factschema.EncodeAzureCloudResource(resource)
		if err != nil {
			panic(fmt.Sprintf("familyodu: catalog_seed %s: encode azure_cloud_resource %q: %v", CloudResourceFamilyOduName, resource.ARMResourceID, err))
		}
		out = append(out, facts.Envelope{
			ScopeID:          cloudResourceFamilyAzureScopeID,
			GenerationID:     cloudResourceFamilyGenerationID,
			FactKind:         cloud.AzureCloudResourceFactKind,
			StableFactKey:    fmt.Sprintf("azure:%s:%s:%s", cloudResourceFamilyAzureSubscriptionID, resource.ResourceType, resource.ARMResourceID),
			SchemaVersion:    cloud.AzureCloudResourceSchemaVersion,
			CollectorKind:    "azure",
			SourceConfidence: cloudResourceFamilySourceConfidence,
			Payload:          payload,
		})
	}
	for i, relationship := range relationships {
		payload, err := factschema.EncodeAzureCloudRelationship(relationship)
		if err != nil {
			panic(fmt.Sprintf("familyodu: catalog_seed %s: encode azure_cloud_relationship %d: %v", CloudResourceFamilyOduName, i, err))
		}
		out = append(out, facts.Envelope{
			ScopeID:          cloudResourceFamilyAzureScopeID,
			GenerationID:     cloudResourceFamilyGenerationID,
			FactKind:         cloud.AzureCloudRelationshipFactKind,
			StableFactKey:    fmt.Sprintf("azure:%s:relationship:%s:%s->%s:%d", cloudResourceFamilyAzureSubscriptionID, relationship.RelationshipType, relationship.SourceARMResourceID, relationship.TargetARMResourceID, i),
			SchemaVersion:    cloud.AzureCloudRelationshipSchemaVersion,
			CollectorKind:    "azure",
			SourceConfidence: cloudResourceFamilySourceConfidence,
			Payload:          payload,
		})
	}
	return out
}

// CloudResourceFamilyOdu builds the cataloged Odù for the cloud_resource
// predicate-scoped direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard resolves it by name. It panics on an encode
// failure for the same reason IAMInstanceProfileRoleFamilyOdu does: a
// failure means the payload contract moved under a committed fixture.
func CloudResourceFamilyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0, 15)
	factsForOdu = append(factsForOdu, cloudResourceFamilyAWSFacts()...)
	factsForOdu = append(factsForOdu, cloudResourceFamilyGCPFacts()...)
	factsForOdu = append(factsForOdu, cloudResourceFamilyAzureFacts()...)
	return CatalogOdu{
		Odu:    Odu{Name: CloudResourceFamilyOduName, Facts: factsForOdu},
		Detail: "fifteen facts across one AWS, one GCP, and one Azure scope generation for the predicate-scoped cloud_resource family: two AWS verbs plus duplicate/unresolved/self-loop controls, one supported GCP relationship plus partial and unresolved controls, and one Azure managed_by plus an unsupported-verb control",
	}
}
