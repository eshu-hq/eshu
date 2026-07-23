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

// runningImageFieldsAbsent is the explicit-empty running_image_ref /
// running_image_digest pair cloudResourceRunningImageFields returns whenever
// there is no running-image truth to publish for a resource: not gated, no
// image evidence, or ambiguous evidence (more than one ECS container). Both
// keys are ALWAYS present with "" rather than omitted (issue #5450, following
// the #4995 precedent — see gcpCloudResourceNodeRow's parity-key comment in
// gcp_resource_materialization.go): the pinned NornicDB backend does not
// evaluate a key MISSING from one row of a heterogeneous UNWIND $rows list as
// null in canonicalCloudResourceUpsertCypher's SET clause, it persists a
// stringified representation of the row expression instead ("row.
// running_image_ref" as a literal, non-empty string), which would corrupt the
// property for every CloudResource this map's key is missing from and
// silently defeat the golden-corpus gate's non-empty presence check. Omitting
// the key was correct Cypher-null intent but wrong for this backend's
// UNWIND+SET map-shape handling.
var runningImageFieldsAbsent = map[string]any{"running_image_ref": "", "running_image_digest": ""}

// cloudResourceRunningImageFields returns the running_image_ref /
// running_image_digest CloudResource node properties for an already-decoded
// aws_resource struct. Both keys are always present in the returned map
// (never omitted — see runningImageFieldsAbsent); they carry real values only
// when the resource's resource_type is an ECS running task or a Lambda
// function AND the running image resolves unambiguously, and "" otherwise —
// an unresolvable running image is a legitimate "no running-image truth to
// publish" outcome, not a fabricated one, mirroring
// cloudResourceServiceAnchorFields' ambiguous-stays-unpromoted pattern (that
// function's own nil-map omission carries the pre-existing #4995 gap for
// AWS resources with no service-anchor decision; out of scope for #5450, not
// duplicated here). A non-nil error means the payload carried a
// present-but-malformed image field the caller MUST route to the same
// quarantine/dead-letter path an envelope decode failure uses (reducer/
// AGENTS.md: never emit an empty-string CloudResource uid or a
// silently-substituted value).
func cloudResourceRunningImageFields(resource awsv1.Resource) (map[string]any, error) {
	switch {
	case isGatedResourceType(resource.ResourceType, ecsRunningTaskResourceTypes):
		return ecsRunningTaskImageFields(resource)
	case isGatedResourceType(resource.ResourceType, lambdaFunctionResourceTypes):
		return lambdaFunctionImageFields(resource)
	default:
		return runningImageFieldsAbsent, nil
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
		return runningImageFieldsAbsent, nil
	}
	container := attrs.Containers[0]
	if container.Image == "" {
		return runningImageFieldsAbsent, nil
	}
	// Both keys are always present (never omitted — see
	// runningImageFieldsAbsent's doc): running_image_digest falls back to ""
	// rather than being dropped when the container reports no digest, so a
	// digest-less row never collides with the pinned NornicDB
	// missing-map-key-in-UNWIND bug.
	fields := map[string]any{"running_image_ref": container.Image, "running_image_digest": ""}
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
//
// running_image_digest is normalized to the BARE digest ("sha256:<hex>",
// parsed out of resolved_image_uri's "registry/repository@sha256:<hex>" shape
// via the shared digestFromImageRef helper) so the property carries the
// IDENTICAL shape the ECS running-task path already writes
// (TaskContainer.ImageDigest is bare from the AWS API). Before this
// normalization, running_image_digest carried the bare digest for ECS but the
// full registry/repository@digest reference for Lambda — same property name,
// two incompatible shapes, which would silently mis-handle any consumer that
// joins or pattern-matches on this property expecting one shape. The full
// digest-bearing reference is still available in full via running_image_ref.
// A resolved_image_uri present but not digest-qualified (an unexpected shape)
// decodes as no running_image_digest rather than a fabricated/truncated
// value — the same "unresolvable stays absent" convention as every other
// field in this file.
func lambdaFunctionImageFields(resource awsv1.Resource) (map[string]any, error) {
	attrs, err := awsv1.DecodeResourceLambdaFunctionImageAttributes(resource)
	if err != nil {
		return nil, err
	}
	if attrs.ImageURI == "" {
		return runningImageFieldsAbsent, nil
	}
	// Both keys are always present (never omitted — see
	// runningImageFieldsAbsent's doc): running_image_digest falls back to ""
	// rather than being dropped when it does not resolve, so a digest-less row
	// never collides with the pinned NornicDB missing-map-key-in-UNWIND bug.
	fields := map[string]any{"running_image_ref": attrs.ImageURI, "running_image_digest": ""}
	if digest := digestFromImageRef(attrs.ResolvedImageURI); digest != "" {
		fields["running_image_digest"] = digest
	}
	return fields, nil
}
