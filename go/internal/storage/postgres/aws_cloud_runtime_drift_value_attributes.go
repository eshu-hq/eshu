// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
)

// cloudResourceTypeEC2Instance and its siblings are the AWS collector's OWN
// resource_type strings (aws_resource.payload.resource_type), which follow a
// different naming convention than Terraform's resource type names (compare
// terraformResourceTypeAWSInstance below). value_attributes_test.go and the
// awscloud cassette fixture are the source of truth for these exact strings.
const (
	cloudResourceTypeEC2Instance       = "aws_ec2_instance"
	cloudResourceTypeLambdaFunction    = "lambda.function"
	cloudResourceTypeECSTaskDefinition = "ecs.task_definition"
)

// terraformResourceTypeAWSInstance and its siblings are Terraform provider
// resource type names (terraform_state_resource.payload.type), the STATE-side
// ResourceRow.ResourceType value ValueAttributeAllowlistFor and
// ClassifyValueDrift key off.
const (
	terraformResourceTypeAWSInstance          = "aws_instance"
	terraformResourceTypeAWSLambdaFunction    = "aws_lambda_function"
	terraformResourceTypeAWSECSTaskDefinition = "aws_ecs_task_definition"
)

// cloudObservedValueAttributes normalizes the bounded set of AWS-observed
// comparable values off one aws_resource payload's attributes object onto
// the SAME map keys the Terraform-state side uses (see
// cloudruntime.ValueAttributeAllowlistFor), keyed by the AWS collector's own
// resource_type string. Returns (nil, nil, false) for any resource type
// value-drift does not cover.
//
// ECS container images are handled separately through
// cloudruntime.ExtractObservedContainerImages, which is the ONLY function
// permitted to read the "containers" attribute -- it bounds the extraction
// to the "image" field alone, discarding the environment/secrets fields the
// ECS collector's containerMaps also populates (#5453 SECURITY).
//
// Shared verbatim with the multi-cloud loader
// (multi_cloud_runtime_drift_evidence.go), so the AWS and provider-neutral
// paths can never disagree about which values were observed.
func cloudObservedValueAttributes(
	resourceType string,
	attributes map[string]any,
) (attrs map[string]string, containerImages []string, truncated bool) {
	if len(attributes) == 0 {
		return nil, nil, false
	}
	switch resourceType {
	case cloudResourceTypeEC2Instance:
		if v := strings.TrimSpace(coerceJSONString(attributes["ami_id"])); v != "" {
			return map[string]string{"ami": v}, nil, false
		}
	case cloudResourceTypeLambdaFunction:
		out := map[string]string{}
		if v := strings.TrimSpace(coerceJSONString(attributes["image_uri"])); v != "" {
			out["image_uri"] = v
		}
		if v := strings.TrimSpace(coerceJSONString(attributes["version"])); v != "" {
			out["version"] = v
		}
		if len(out) > 0 {
			return out, nil, false
		}
	case cloudResourceTypeECSTaskDefinition:
		result := cloudruntime.ExtractObservedContainerImages(attributes["containers"])
		return nil, result.Images, result.Truncated
	}
	return nil, nil, false
}

// stateDeclaredValueAttributes normalizes the bounded set of Terraform-
// declared comparable values off one terraform_state_resource payload's
// attributes object, keyed by the Terraform provider's resource type name.
// Returns (nil, nil, false) for any resource type value-drift does not
// cover.
//
// container_definitions is a JSON-encoded STRING that can carry environment
// variables and secret ARN references; cloudruntime.ExtractDeclaredContainerImages
// is the ONLY function permitted to parse it, and it decodes into a struct
// with just an Image field so every other key is discarded by
// json.Unmarshal itself (#5453 SECURITY).
//
// Shared verbatim with the multi-cloud loader
// (multi_cloud_runtime_drift_evidence.go).
func stateDeclaredValueAttributes(
	resourceType string,
	attributes map[string]any,
) (attrs map[string]string, containerImages []string, truncated bool) {
	if len(attributes) == 0 {
		return nil, nil, false
	}
	switch resourceType {
	case terraformResourceTypeAWSInstance:
		if v := strings.TrimSpace(coerceJSONString(attributes["ami"])); v != "" {
			return map[string]string{"ami": v}, nil, false
		}
	case terraformResourceTypeAWSLambdaFunction:
		out := map[string]string{}
		if v := strings.TrimSpace(coerceJSONString(attributes["image_uri"])); v != "" {
			out["image_uri"] = v
		}
		if v := strings.TrimSpace(coerceJSONString(attributes["version"])); v != "" {
			out["version"] = v
		}
		if len(out) > 0 {
			return out, nil, false
		}
	case terraformResourceTypeAWSECSTaskDefinition:
		result := cloudruntime.ExtractDeclaredContainerImages(attributes["container_definitions"])
		return nil, result.Images, result.Truncated
	}
	return nil, nil, false
}

// containerImagesTruncatedWarning returns the "container_images_truncated"
// warning flag when either side's ECS container-image extraction hit
// MaxContainerImagesPerResource, so the operator-facing read model can flag
// that ContainerImages may be an incomplete view of a task definition
// carrying more distinct images than the bound (#5453).
func containerImagesTruncatedWarning(cloud, state *cloudruntime.ResourceRow) []string {
	truncated := (cloud != nil && cloud.ContainerImagesTruncated) || (state != nil && state.ContainerImagesTruncated)
	if !truncated {
		return nil
	}
	return []string{"container_images_truncated"}
}
