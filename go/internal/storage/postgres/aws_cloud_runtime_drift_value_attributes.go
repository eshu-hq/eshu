// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
)

// cloudResourceTypeEC2Instance and its siblings are the AWS collector's OWN
// resource_type strings (aws_resource.payload.resource_type). For Lambda and
// ECS these come in TWO forms that the observed side must both accept, exactly
// like the sibling running-image reducer (aws_resource_running_image.go): the
// dot-separated cassette short-name ("lambda.function" / "ecs.task_definition")
// AND the live collector's own production strings ("aws_lambda_function" /
// "aws_ecs_task_definition", awscloud.ResourceTypeLambdaFunction /
// ResourceTypeECSTaskDefinition in constants_lambda.go / constants_ecs.go).
// Matching only the cassette short-name would make cloudObservedValueAttributes
// silently return nil for every real production Lambda/ECS observation, so
// value drift would never fire for them outside the fixtures (#5453 codex/owner
// P0). EC2 already uses the production "aws_ec2_instance" string in both.
const (
	cloudResourceTypeEC2Instance = "aws_ec2_instance"
	// cassette short-name form
	cloudResourceTypeLambdaFunction    = "lambda.function"
	cloudResourceTypeECSTaskDefinition = "ecs.task_definition"
	// live-collector production form (awscloud.ResourceTypeLambdaFunction /
	// ResourceTypeECSTaskDefinition)
	cloudResourceTypeLambdaFunctionProd    = "aws_lambda_function"
	cloudResourceTypeECSTaskDefinitionProd = "aws_ecs_task_definition"
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
// resource_type string. Returns a zero result for any resource type value drift
// does not cover. The trailing degraded flag reports that a container-definitions
// value EXISTED but could not be read (see
// cloudruntime.ContainerImageExtractionResult.Degraded), which must not be
// confused with the value being absent (#5837).
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
) (attrs map[string]string, containerImages []string, truncated bool, degraded bool) {
	if len(attributes) == 0 {
		return nil, nil, false, false
	}
	switch resourceType {
	case cloudResourceTypeEC2Instance:
		if v := strings.TrimSpace(coerceJSONString(attributes["ami_id"])); v != "" {
			return map[string]string{"ami": v}, nil, false, false
		}
	case cloudResourceTypeLambdaFunction, cloudResourceTypeLambdaFunctionProd:
		out := map[string]string{}
		if v := strings.TrimSpace(coerceJSONString(attributes["image_uri"])); v != "" {
			out["image_uri"] = v
		}
		if v := strings.TrimSpace(coerceJSONString(attributes["version"])); v != "" {
			out["version"] = v
		}
		if len(out) > 0 {
			return out, nil, false, false
		}
	case cloudResourceTypeECSTaskDefinition, cloudResourceTypeECSTaskDefinitionProd:
		result := cloudruntime.ExtractObservedContainerImages(attributes["containers"])
		return nil, result.Images, result.Truncated, result.Degraded
	}
	return nil, nil, false, false
}

// stateDeclaredValueAttributes normalizes the bounded set of Terraform-
// declared comparable values off one terraform_state_resource payload's
// attributes object, keyed by the Terraform provider's resource type name.
// Returns a zero result for any resource type value drift does not cover; the
// trailing degraded flag carries the same unreadable-versus-absent distinction
// as the observed side (#5837).
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
) (attrs map[string]string, containerImages []string, truncated bool, degraded bool) {
	if len(attributes) == 0 {
		return nil, nil, false, false
	}
	switch resourceType {
	case terraformResourceTypeAWSInstance:
		if v := strings.TrimSpace(coerceJSONString(attributes["ami"])); v != "" {
			return map[string]string{"ami": v}, nil, false, false
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
			return out, nil, false, false
		}
	case terraformResourceTypeAWSECSTaskDefinition:
		result := cloudruntime.ExtractDeclaredContainerImages(attributes["container_definitions"])
		return nil, result.Images, result.Truncated, result.Degraded
	}
	return nil, nil, false, false
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

// containerImagesUnreadableWarning returns the "container_images_unreadable"
// warning flag when either side carried a container-definitions value that
// could not be parsed -- the terraform-state collector's fail-closed redaction
// marker being the case this exists for (#5837).
//
// It is separate from the truncation warning because the operator action
// differs: truncation means the bound was hit and the comparison is on a
// partial set, unreadable means no comparison happened at all and the finding
// this ARN carries is value_comparison_inconclusive rather than a verdict.
func containerImagesUnreadableWarning(cloud, state *cloudruntime.ResourceRow) []string {
	degraded := (cloud != nil && cloud.ContainerImagesDegraded) || (state != nil && state.ContainerImagesDegraded)
	if !degraded {
		return nil
	}
	return []string{"container_images_unreadable"}
}
