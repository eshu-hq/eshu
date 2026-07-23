// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// ecsRunningTaskResourceTypes and lambdaFunctionResourceTypes are the closed,
// bounded resource_type gates for the running-image CloudResource node-prop
// enrichment (issue #5450). Each set carries two aliases deliberately: the
// literal short form the golden-corpus awscloud cassette
// (testdata/cassettes/awscloud/supply-chain-demo.json) and issue #5450 itself
// name ("ecs.task" / "lambda.function"), and the live collector's own
// awscloud.ResourceTypeECSTask / awscloud.ResourceTypeLambdaFunction constants
// ("aws_ecs_task" / "aws_lambda_function", go/internal/collector/awscloud/
// constants_ecs.go, constants_lambda.go). The reducer package does not import
// the collector package (package-boundary rule), so the constants are
// duplicated here as literals rather than imported; gating on both aliases
// keeps the enrichment correct for real production resource_type values AND
// exercised by the existing fixture without requiring either side to change.
var (
	ecsRunningTaskResourceTypes = map[string]struct{}{
		"ecs.task":     {},
		"aws_ecs_task": {},
	}
	lambdaFunctionResourceTypes = map[string]struct{}{
		"lambda.function":     {},
		"aws_lambda_function": {},
	}
)

// cloudResourceRunningImageFields returns the running_image_ref /
// running_image_digest CloudResource node properties for an already-decoded
// aws_resource struct, when the resource's resource_type is an ECS running
// task or a Lambda function AND the running image resolves unambiguously.
// Returns a nil map (not an error) when the resource_type is not gated, when
// no image evidence is present, or when the evidence is ambiguous (more than
// one ECS container) — an unresolvable running image is a legitimate "no
// running-image truth to publish" outcome, not a fabricated one, mirroring
// cloudResourceServiceAnchorFields' ambiguous-stays-unpromoted pattern. A
// non-nil error means the payload carried a present-but-malformed image field
// the caller MUST route to the same quarantine/dead-letter path an envelope
// decode failure uses (reducer/AGENTS.md: never emit an empty-string
// CloudResource uid or a silently-substituted value).
func cloudResourceRunningImageFields(resource awsv1.Resource) (map[string]any, error) {
	switch {
	case isGatedResourceType(resource.ResourceType, ecsRunningTaskResourceTypes):
		return ecsRunningTaskImageFields(resource)
	case isGatedResourceType(resource.ResourceType, lambdaFunctionResourceTypes):
		return lambdaFunctionImageFields(resource)
	default:
		return nil, nil
	}
}

func isGatedResourceType(resourceType string, gate map[string]struct{}) bool {
	_, ok := gate[resourceType]
	return ok
}

// ecsRunningTaskImageFields decodes an ECS running task's containers[] and
// surfaces running_image_ref/running_image_digest only when exactly one
// container reports a non-empty image: a multi-container task (sidecars) has
// no single "the" running image, so promoting one container's image as if it
// were the task's own would fabricate a false single-image truth. This
// mirrors the reducer's ambiguous-stays-unpromoted convention
// (cloudResourceServiceAnchorDecision) rather than guessing an "essential"
// container — TaskContainer (unlike the task DEFINITION's Container) carries
// no essential flag to disambiguate by.
func ecsRunningTaskImageFields(resource awsv1.Resource) (map[string]any, error) {
	attrs, err := awsv1.DecodeResourceECSTaskAttributes(resource)
	if err != nil {
		return nil, err
	}
	if len(attrs.Containers) != 1 {
		return nil, nil
	}
	container := attrs.Containers[0]
	if container.Image == "" {
		return nil, nil
	}
	fields := map[string]any{"running_image_ref": container.Image}
	if container.ImageDigest != "" {
		fields["running_image_digest"] = container.ImageDigest
	}
	return fields, nil
}

// lambdaFunctionImageFields decodes a Lambda function's image_uri /
// resolved_image_uri and surfaces them as running_image_ref /
// running_image_digest. A Lambda function is single-image by AWS's own model
// (one function = one deployed package), so there is no multi-container
// ambiguity to resolve.
func lambdaFunctionImageFields(resource awsv1.Resource) (map[string]any, error) {
	attrs, err := awsv1.DecodeResourceLambdaFunctionImageAttributes(resource)
	if err != nil {
		return nil, err
	}
	if attrs.ImageURI == "" {
		return nil, nil
	}
	fields := map[string]any{"running_image_ref": attrs.ImageURI}
	if attrs.ResolvedImageURI != "" {
		fields["running_image_digest"] = attrs.ResolvedImageURI
	}
	return fields, nil
}
