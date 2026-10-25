// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package familyodu

import (
	"fmt"

	"github.com/eshu-hq/eshu-sdk/factschema"
	awsv1 "github.com/eshu-hq/eshu-sdk/factschema/aws/v1"
	ociregistryv1 "github.com/eshu-hq/eshu-sdk/factschema/ociregistry/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

const (
	// CloudResourceContainerImageFamilyOduName is this Odù's catalog name,
	// the ref a materialized_edges:cloud_resource_container_image coverage
	// row would name to resolve through it.
	CloudResourceContainerImageFamilyOduName = "odu:ifa-cloud-resource-container-image-family"

	// cloudResourceContainerImageFamilyScopeID is the single AWS account
	// scope every fact in this Odù belongs to, including the one OCI
	// manifest below (which rides this scope the way the built_from
	// sibling's OCI manifests ride its git scope). The reducer handler
	// loads one scope generation's aws_resource and aws_relationship
	// facts, so a fixture spanning scopes would not mirror any real
	// intent. It is a dedicated account, not shared with any sibling
	// cassette: scopes carry one ACTIVE generation, so driving a second
	// generation into an occupied scope supersedes the first family's
	// generation.
	cloudResourceContainerImageFamilyScopeID = "aws:eshu-fixture-cloud-resource-container-image-account"

	// cloudResourceContainerImageFamilyAccountID and
	// cloudResourceContainerImageFamilyRegion are two of the four inputs to
	// the CloudResource uid the edge source is keyed by
	// (payloadcore.CloudResourceUID over account_id, region, resource_type,
	// resource_id). They are synthetic, matching the collector's own
	// redaction posture.
	cloudResourceContainerImageFamilyAccountID = "123456789012"
	cloudResourceContainerImageFamilyRegion    = "us-east-1"

	// cloudResourceContainerImageFamilyGenerationID is the one scope
	// generation the Odù replays.
	cloudResourceContainerImageFamilyGenerationID = "gen-ifa-cloud-resource-container-image-family-1"

	// cloudResourceContainerImageFamilyCollectorKind and
	// cloudResourceContainerImageFamilySchemaVersion mirror what the
	// awscloud collector stamps on aws_resource and aws_relationship facts.
	cloudResourceContainerImageFamilyCollectorKind = "aws"
	cloudResourceContainerImageFamilySchemaVersion = "1.0.0"

	// cloudResourceContainerImageFamilyOCICollectorKind is the collector
	// kind the OCI manifest below carries, mirroring the built_from
	// sibling: OCI facts ride the same single scope as the aws facts,
	// and the canonical projector materializes the target ContainerImage
	// node from them. Without it the image edge writer's
	// filterRowsToExistingContainerImageIdentity drops both edge rows as
	// target_not_materialized and the live gate sees zero edges (CI
	// determinism-matrix red on the twelve-fact cassette).
	cloudResourceContainerImageFamilyOCICollectorKind = "oci_registry"

	// cloudResourceContainerImageFamilyManifestRepositoryID is the OCI
	// registry repository id of the shared digest, in the oci-registry://
	// form the canonical projector's ociDescriptorUID needs to derive the
	// oci-descriptor:// target uid the edge writer MATCHes.
	cloudResourceContainerImageFamilyManifestRepositoryID = "oci-registry://123456789012.dkr.ecr.us-east-1.amazonaws.com/demo"

	// cloudResourceContainerImageFamilyImageRegistry is the ECR registry
	// hosting the fixture image. Lambda container images are exclusively
	// ECR-hosted (a platform restriction, not a convention), so the
	// resolved_image_uri values below are ECR references.
	cloudResourceContainerImageFamilyImageRegistry = "123456789012.dkr.ecr.us-east-1.amazonaws.com/demo"

	// cloudResourceContainerImageFamilyDigest is the exact image digest both
	// edge-producing functions resolve. Synthetic but full-length, so the
	// ref parser exercises the real digest-qualified path.
	cloudResourceContainerImageFamilyDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// cloudResourceContainerImageFamilyResource describes one aws_resource fact
// in the Odù: a Lambda function or ECS task definition the join index can
// resolve a relationship source against.
type cloudResourceContainerImageFamilyResource struct {
	// ResourceType is the provider-defined type. It is a uid input, so it
	// is what the expected-edge fixture's source identities derive from.
	ResourceType string
	// ResourceID is the provider-assigned id (an ARN here). The ARN is
	// also set, so the uid derives through the ARN index path the
	// extractor prefers.
	ResourceID string
}

// cloudResourceContainerImageFamilyRelationship describes one
// aws_relationship fact in the Odù.
type cloudResourceContainerImageFamilyRelationship struct {
	// RelationshipType is the raw aws_relationship verb. Only
	// lambda_function_uses_image resolves to an exact digest reference;
	// ecs_task_definition_uses_image is tag-only by policy and every other
	// verb is out of this domain's scope.
	RelationshipType string
	// SourceARN names the relationship source. Empty (with a bare source
	// id) proves the resource-id fallback path; a value naming no scanned
	// resource proves the source-unresolved skip.
	SourceARN string
	// SourceResourceID is the bare source identity.
	SourceResourceID string
	// ResolvedImageURI is the Lambda-resolved exact image reference. Empty
	// proves the no-digest skip; a tag-only value proves the unparseable
	// skip. Nil means the attribute is absent entirely (the ECS shape,
	// which never carries one).
	ResolvedImageURI *string
}

// cloudResourceContainerImageFamilyResources holds the scanned-function
// half of the fixture: every relationship source that must resolve.
var cloudResourceContainerImageFamilyResources = []cloudResourceContainerImageFamilyResource{
	{ResourceType: "aws_lambda_function", ResourceID: "arn:aws:lambda:us-east-1:123456789012:function:demo"},
	{ResourceType: "aws_lambda_function", ResourceID: "arn:aws:lambda:us-east-1:123456789012:function:worker"},
	{ResourceType: "aws_lambda_function", ResourceID: "arn:aws:lambda:us-east-1:123456789012:function:zipped"},
	{ResourceType: "aws_lambda_function", ResourceID: "arn:aws:lambda:us-east-1:123456789012:function:badref"},
	{ResourceType: "aws_ecs_task_definition", ResourceID: "arn:aws:ecs:us-east-1:123456789012:task-definition/demo:1"},
}

// cloudResourceContainerImageFamilyRelationships holds the relationship
// half: two edge-producing lambda_function_uses_image facts and five
// deliberate non-producers covering the tag-only policy, no-digest,
// unparseable-ref, source-unresolved and foreign-verb branches.
var cloudResourceContainerImageFamilyRelationships = []cloudResourceContainerImageFamilyRelationship{
	{
		// EDGE: exact digest reference on a resolved function.
		RelationshipType: "lambda_function_uses_image",
		SourceARN:        "arn:aws:lambda:us-east-1:123456789012:function:demo",
		SourceResourceID: "arn:aws:lambda:us-east-1:123456789012:function:demo",
		ResolvedImageURI: stringPtr(cloudResourceContainerImageFamilyImageRegistry + "@" + cloudResourceContainerImageFamilyDigest),
	},
	{
		// EDGE: a second function on the same digest. The dedup key is
		// (source, target), so a shared image fans out to one edge per
		// function rather than collapsing to one.
		RelationshipType: "lambda_function_uses_image",
		SourceARN:        "arn:aws:lambda:us-east-1:123456789012:function:worker",
		SourceResourceID: "arn:aws:lambda:us-east-1:123456789012:function:worker",
		ResolvedImageURI: stringPtr(cloudResourceContainerImageFamilyImageRegistry + "@" + cloudResourceContainerImageFamilyDigest),
	},
	{
		// NO EDGE: tag-only ECS reference. The #5472 EXACT-ONLY policy
		// keeps it Postgres-only even with a resolved source.
		RelationshipType: "ecs_task_definition_uses_image",
		SourceARN:        "arn:aws:ecs:us-east-1:123456789012:task-definition/demo:1",
		SourceResourceID: "arn:aws:ecs:us-east-1:123456789012:task-definition/demo:1",
	},
	{
		// NO EDGE: a zip-packaged function carries no resolved digest.
		RelationshipType: "lambda_function_uses_image",
		SourceARN:        "arn:aws:lambda:us-east-1:123456789012:function:zipped",
		SourceResourceID: "arn:aws:lambda:us-east-1:123456789012:function:zipped",
		ResolvedImageURI: stringPtr(""),
	},
	{
		// NO EDGE: a tag-only resolved reference is not a fabricable
		// ContainerImage identity.
		RelationshipType: "lambda_function_uses_image",
		SourceARN:        "arn:aws:lambda:us-east-1:123456789012:function:badref",
		SourceResourceID: "arn:aws:lambda:us-east-1:123456789012:function:badref",
		ResolvedImageURI: stringPtr(cloudResourceContainerImageFamilyImageRegistry + ":latest"),
	},
	{
		// NO EDGE: an unscanned function resolves no source endpoint.
		RelationshipType: "lambda_function_uses_image",
		SourceARN:        "arn:aws:lambda:us-east-1:123456789012:function:ghost",
		SourceResourceID: "arn:aws:lambda:us-east-1:123456789012:function:ghost",
		ResolvedImageURI: stringPtr(cloudResourceContainerImageFamilyImageRegistry + "@" + cloudResourceContainerImageFamilyDigest),
	},
	{
		// NO EDGE: a verb outside this domain's scope is ignored, even on
		// a resolved source with a digest-bearing target.
		RelationshipType: "uses_kms_key",
		SourceARN:        "arn:aws:lambda:us-east-1:123456789012:function:demo",
		SourceResourceID: "arn:aws:lambda:us-east-1:123456789012:function:demo",
		ResolvedImageURI: stringPtr(cloudResourceContainerImageFamilyImageRegistry + "@" + cloudResourceContainerImageFamilyDigest),
	},
}

// stringPtr boxes one string for the optional SDK pointers the fixture
// builders below need.
func stringPtr(value string) *string { return &value }

// CloudResourceContainerImageFamilyOdu builds the cataloged Odù for the
// cloud_resource_container_image direct-materialization family.
//
// Exported because catalog_seed.go registers it at package-init time and
// materializededges' guard test resolves it by name. Facts are built as
// typed awsv1.Resource / awsv1.Relationship values and encoded through
// factschema.EncodeAWSResource / factschema.EncodeAWSRelationship, never as
// hand-built maps (Contract System v1). It panics on an encode failure for
// the same reason WorkloadCloudRelationshipFamilyOdu does: a failure means
// the payload contract moved under a committed fixture, and every coverage
// claim built on it is already void.
func CloudResourceContainerImageFamilyOdu() CatalogOdu {
	factsForOdu := make([]facts.Envelope, 0,
		len(cloudResourceContainerImageFamilyResources)+len(cloudResourceContainerImageFamilyRelationships)+1)
	for _, resource := range cloudResourceContainerImageFamilyResources {
		arn := resource.ResourceID
		payload, err := factschema.EncodeAWSResource(awsv1.Resource{
			AccountID:    cloudResourceContainerImageFamilyAccountID,
			ResourceID:   resource.ResourceID,
			Region:       cloudResourceContainerImageFamilyRegion,
			ResourceType: resource.ResourceType,
			ARN:          &arn,
		})
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode aws_resource payload for %q: %v",
				CloudResourceContainerImageFamilyOduName, resource.ResourceID, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          cloudResourceContainerImageFamilyScopeID,
			GenerationID:     cloudResourceContainerImageFamilyGenerationID,
			FactKind:         cloud.AWSResourceFactKind,
			StableFactKey:    fmt.Sprintf("aws:%s:%s:%s", cloudResourceContainerImageFamilyRegion, resource.ResourceType, resource.ResourceID),
			SchemaVersion:    cloudResourceContainerImageFamilySchemaVersion,
			CollectorKind:    cloudResourceContainerImageFamilyCollectorKind,
			SourceConfidence: facts.SourceConfidenceObserved,
			Payload:          payload,
		})
	}
	for _, relationship := range cloudResourceContainerImageFamilyRelationships {
		attrs := map[string]any{}
		if relationship.ResolvedImageURI != nil {
			attrs["attributes"] = map[string]any{
				"package_type":       "Image",
				"resolved_image_uri": *relationship.ResolvedImageURI,
			}
		}
		sourceARN := relationship.SourceARN
		payload, err := factschema.EncodeAWSRelationship(awsv1.Relationship{
			AccountID:        cloudResourceContainerImageFamilyAccountID,
			Region:           cloudResourceContainerImageFamilyRegion,
			RelationshipType: relationship.RelationshipType,
			SourceResourceID: relationship.SourceResourceID,
			TargetResourceID: cloudResourceContainerImageFamilyImageRegistry + ":latest",
			SourceARN:        &sourceARN,
			TargetType:       stringPtr("container_image"),
			Attributes:       attrs,
		})
		if err != nil {
			panic(fmt.Sprintf(
				"familyodu: catalog_seed %s: encode aws_relationship payload for %q: %v",
				CloudResourceContainerImageFamilyOduName, relationship.SourceResourceID, err,
			))
		}
		factsForOdu = append(factsForOdu, facts.Envelope{
			ScopeID:          cloudResourceContainerImageFamilyScopeID,
			GenerationID:     cloudResourceContainerImageFamilyGenerationID,
			FactKind:         cloud.AWSRelationshipFactKind,
			StableFactKey:    fmt.Sprintf("aws-rel:%s:%s:%s", cloudResourceContainerImageFamilyRegion, relationship.RelationshipType, relationship.SourceResourceID),
			SchemaVersion:    cloudResourceContainerImageFamilySchemaVersion,
			CollectorKind:    cloudResourceContainerImageFamilyCollectorKind,
			SourceConfidence: facts.SourceConfidenceObserved,
			Payload:          payload,
		})
	}

	// One OCI manifest for the shared digest. The canonical projector
	// materializes the target ContainerImage node from it; without the
	// node the image edge writer drops both edge rows as
	// target_not_materialized and the live gate sees zero edges. The
	// non-producer digests deliberately get no manifest: badref is
	// unparseable, ghost is unscanned, and the zip has no digest, so
	// their targets must stay absent.
	manifestPayload, err := factschema.EncodeOCIImageManifest(ociregistryv1.ImageManifest{
		RepositoryID: cloudResourceContainerImageFamilyManifestRepositoryID,
		Digest:       cloudResourceContainerImageFamilyDigest,
		MediaType:    stringPtr("application/vnd.oci.image.manifest.v1+json"),
	})
	if err != nil {
		panic(fmt.Sprintf(
			"familyodu: catalog_seed %s: encode oci manifest payload for %q: %v",
			CloudResourceContainerImageFamilyOduName, cloudResourceContainerImageFamilyDigest, err,
		))
	}
	factsForOdu = append(factsForOdu, facts.Envelope{
		ScopeID:          cloudResourceContainerImageFamilyScopeID,
		GenerationID:     cloudResourceContainerImageFamilyGenerationID,
		FactKind:         chain.OCIImageManifestFactKind,
		StableFactKey:    "oci-manifest:" + cloudResourceContainerImageFamilyDigest,
		SchemaVersion:    cloudResourceContainerImageFamilySchemaVersion,
		CollectorKind:    cloudResourceContainerImageFamilyOCICollectorKind,
		SourceConfidence: facts.SourceConfidenceObserved,
		Payload:          manifestPayload,
	})

	return CatalogOdu{
		Odu:    Odu{Name: CloudResourceContainerImageFamilyOduName, Facts: factsForOdu},
		Detail: "thirteen facts for the direct-materialization cloud_resource_container_image family: five scanned resources, seven relationships, and one OCI manifest for the shared digest — two edge-producing lambda_function_uses_image facts on one shared digest plus five deliberate non-producers covering the tag-only policy, no-digest, unparseable-ref, source-unresolved and foreign-verb branches, so the AWS_lambda_function_uses_image expected set proves both promotion and restraint",
	}
}
