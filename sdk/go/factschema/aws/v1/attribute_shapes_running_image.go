// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import "fmt"

// ResourceECSTaskContainerImage is one container's running-image evidence off
// an aws_ecs_task aws_resource fact's nested Attributes["attributes"].containers
// entry (issue #5450). Image is the tag-or-digest reference the container was
// started from; ImageDigest is the resolved digest of the image actually
// running (distinct from a task DEFINITION's tag-only container image, which
// carries no running digest).
type ResourceECSTaskContainerImage struct {
	// Image is the container's image reference as started (commonly
	// tag-qualified), when reported.
	Image string
	// ImageDigest is the resolved digest of the image actually running, when
	// reported.
	ImageDigest string
}

// ResourceECSTaskAttributes is the typed shape of the nested
// Attributes["attributes"].containers field the running-image CloudResource
// node-prop consumer reads off an ECS running-task aws_resource fact (issue
// #5450).
type ResourceECSTaskAttributes struct {
	// Containers lists the task's running containers, in payload order.
	Containers []ResourceECSTaskContainerImage
}

// DecodeResourceECSTaskAttributes decodes the nested
// Attributes["attributes"].containers image/image_digest fields off an
// already-decoded ECS running-task aws_resource Resource (see
// ResourceECSTaskAttributes). Callers gate this on the ECS running-task
// resource_type themselves (the decode does not know the admission policy).
func DecodeResourceECSTaskAttributes(resource Resource) (ResourceECSTaskAttributes, error) {
	nested := nestedAttributes(resource.Attributes)
	raw, ok := nested["containers"]
	if !ok || raw == nil {
		return ResourceECSTaskAttributes{}, nil
	}
	entries, ok := anyMapSlice(raw)
	if !ok {
		return ResourceECSTaskAttributes{}, newAttributeShapeError(
			"attributes.containers", fmt.Sprintf("want array, got %T", raw),
		)
	}
	out := make([]ResourceECSTaskContainerImage, 0, len(entries))
	for i, entry := range entries {
		m, ok := entry.(map[string]any)
		if !ok {
			return ResourceECSTaskAttributes{}, newAttributeShapeError(
				fmt.Sprintf("attributes.containers[%d]", i), fmt.Sprintf("want object, got %T", entry),
			)
		}
		image, err := attributeString(m, fmt.Sprintf("attributes.containers[%d].image", i), "image")
		if err != nil {
			return ResourceECSTaskAttributes{}, err
		}
		digest, err := attributeString(m, fmt.Sprintf("attributes.containers[%d].image_digest", i), "image_digest")
		if err != nil {
			return ResourceECSTaskAttributes{}, err
		}
		out = append(out, ResourceECSTaskContainerImage{Image: image, ImageDigest: digest})
	}
	return ResourceECSTaskAttributes{Containers: out}, nil
}

// ResourceLambdaFunctionImageAttributes is the typed shape of the nested
// Attributes["attributes"] image_uri/resolved_image_uri fields the
// running-image CloudResource node-prop consumer reads off a Lambda function
// aws_resource fact (issue #5450). ResolvedImageURI carries the resolved
// registry+repository@digest reference when the function's package_type is
// Image; ImageURI is the tag-or-digest reference as configured.
type ResourceLambdaFunctionImageAttributes struct {
	// ImageURI is the Lambda function's configured container image URI, when
	// reported.
	ImageURI string
	// ResolvedImageURI is the digest-resolved container image reference AWS
	// reports for the function's currently deployed image, when reported.
	ResolvedImageURI string
}

// DecodeResourceLambdaFunctionImageAttributes decodes the nested
// Attributes["attributes"] image_uri/resolved_image_uri fields off an
// already-decoded Lambda function aws_resource Resource (see
// ResourceLambdaFunctionImageAttributes). Callers gate this on the Lambda
// function resource_type themselves (the decode does not know the admission
// policy).
func DecodeResourceLambdaFunctionImageAttributes(resource Resource) (ResourceLambdaFunctionImageAttributes, error) {
	nested := nestedAttributes(resource.Attributes)
	imageURI, err := attributeString(nested, "attributes.image_uri", "image_uri")
	if err != nil {
		return ResourceLambdaFunctionImageAttributes{}, err
	}
	resolvedImageURI, err := attributeString(nested, "attributes.resolved_image_uri", "resolved_image_uri")
	if err != nil {
		return ResourceLambdaFunctionImageAttributes{}, err
	}
	return ResourceLambdaFunctionImageAttributes{
		ImageURI:         imageURI,
		ResolvedImageURI: resolvedImageURI,
	}, nil
}

// RelationshipLambdaFunctionUsesImageAttributes is the typed shape of the
// nested Attributes["attributes"] field the AWS cloud-image edge projection
// (issue #5450) reads off a lambda_function_uses_image aws_relationship fact.
// ResolvedImageURI is the exact registry+repository@digest reference the
// two-MATCH-MERGE :ContainerImage edge join resolves against; PackageType is
// carried for completeness but not used by the join.
type RelationshipLambdaFunctionUsesImageAttributes struct {
	// PackageType is the Lambda deployment package type ("Image" for a
	// container-image function), when reported.
	PackageType string
	// ResolvedImageURI is the digest-resolved container image reference, when
	// reported.
	ResolvedImageURI string
}

// DecodeRelationshipLambdaFunctionUsesImageAttributes decodes the nested
// Attributes["attributes"] package_type/resolved_image_uri fields off an
// already-decoded lambda_function_uses_image aws_relationship Relationship
// (see RelationshipLambdaFunctionUsesImageAttributes).
func DecodeRelationshipLambdaFunctionUsesImageAttributes(rel Relationship) (RelationshipLambdaFunctionUsesImageAttributes, error) {
	nested := nestedAttributes(rel.Attributes)
	packageType, err := attributeString(nested, "attributes.package_type", "package_type")
	if err != nil {
		return RelationshipLambdaFunctionUsesImageAttributes{}, err
	}
	resolvedImageURI, err := attributeString(nested, "attributes.resolved_image_uri", "resolved_image_uri")
	if err != nil {
		return RelationshipLambdaFunctionUsesImageAttributes{}, err
	}
	return RelationshipLambdaFunctionUsesImageAttributes{
		PackageType:      packageType,
		ResolvedImageURI: resolvedImageURI,
	}, nil
}
