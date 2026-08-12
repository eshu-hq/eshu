// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/redact"
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

// valueAttributeDecode is one side's normalized comparable-value evidence:
// what was read, and what could not be. The two are separate fields rather
// than one map with holes in it because a hole is ambiguous -- a key missing
// from Attributes may be a value the resource does not have, or one this pass
// failed to read, and only the second may stop cloudruntime.Classify from
// converging and retiring a finding (#5861).
//
// Fields mirror cloudruntime.ResourceRow so the callers copy across without
// reinterpreting anything.
type valueAttributeDecode struct {
	// Attributes holds the readable allowlisted scalars, keyed as
	// cloudruntime.ValueAttributeAllowlistFor names them. Never contains a key
	// listed in DegradedAttributes.
	Attributes map[string]string
	// DegradedAttributes names the allowlisted scalars this side carried but
	// could not use -- a redaction marker, or a value the side's own evidence
	// says must exist while carrying none -- in allowlist order.
	DegradedAttributes []string
	// ContainerImages, ContainerImagesTruncated, and ContainerImagesDegraded
	// carry the ECS container-image comparison, which is extracted rather than
	// read as a scalar. See container_image_extract.go.
	ContainerImages          []string
	ContainerImagesTruncated bool
	ContainerImagesDegraded  bool
}

// applyTo copies this side's comparable-value evidence onto row.
//
// Centralized because four decode sites across two loaders build these rows,
// and a site that copied Attributes while forgetting DegradedAttributes would
// silently reinstate the #5861 convergence for its provider alone -- with every
// unit test still green, because the readable attribute is all they assert on.
func (d valueAttributeDecode) applyTo(row *cloudruntime.ResourceRow) {
	row.Attributes = d.Attributes
	row.DegradedAttributes = d.DegradedAttributes
	row.ContainerImages = d.ContainerImages
	row.ContainerImagesTruncated = d.ContainerImagesTruncated
	row.ContainerImagesDegraded = d.ContainerImagesDegraded
}

// cloudObservedValueAttributes normalizes the bounded set of AWS-observed
// comparable values off one aws_resource payload's attributes object onto
// the SAME map keys the Terraform-state side uses (see
// cloudruntime.ValueAttributeAllowlistFor), keyed by the AWS collector's own
// resource_type string. Returns a zero result for any resource type value drift
// does not cover. The degraded fields report that a value EXISTED but could not
// be read (see cloudruntime.ContainerImageExtractionResult.Degraded), which must
// not be confused with the value being absent (#5837, #5861).
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
func cloudObservedValueAttributes(resourceType string, attributes map[string]any) valueAttributeDecode {
	if len(attributes) == 0 {
		return valueAttributeDecode{}
	}
	switch resourceType {
	case cloudResourceTypeEC2Instance:
		// "ami_id" is the observed field name; "ami" is the shared allowlist key
		// both sides normalize onto, so the degraded key is reported as "ami".
		return ec2InstanceScalarDecode(attributes, "ami_id")
	case cloudResourceTypeLambdaFunction, cloudResourceTypeLambdaFunctionProd:
		return lambdaScalarDecode(attributes)
	case cloudResourceTypeECSTaskDefinition, cloudResourceTypeECSTaskDefinitionProd:
		result := cloudruntime.ExtractObservedContainerImages(attributes["containers"])
		return valueAttributeDecode{
			ContainerImages:          result.Images,
			ContainerImagesTruncated: result.Truncated,
			ContainerImagesDegraded:  result.Degraded,
		}
	}
	return valueAttributeDecode{}
}

// stateDeclaredValueAttributes normalizes the bounded set of Terraform-
// declared comparable values off one terraform_state_resource payload's
// attributes object, keyed by the Terraform provider's resource type name.
// Returns a zero result for any resource type value drift does not cover; the
// degraded fields carry the same unreadable-versus-absent distinction as the
// observed side (#5837, #5861).
//
// container_definitions is a JSON-encoded STRING that can carry environment
// variables and secret ARN references; cloudruntime.ExtractDeclaredContainerImages
// is the ONLY function permitted to parse it, and it decodes into a struct
// with just an Image field so every other key is discarded by
// json.Unmarshal itself (#5453 SECURITY).
//
// Shared verbatim with the multi-cloud loader
// (multi_cloud_runtime_drift_evidence.go).
func stateDeclaredValueAttributes(resourceType string, attributes map[string]any) valueAttributeDecode {
	if len(attributes) == 0 {
		return valueAttributeDecode{}
	}
	switch resourceType {
	case terraformResourceTypeAWSInstance:
		return ec2InstanceScalarDecode(attributes, "ami")
	case terraformResourceTypeAWSLambdaFunction:
		return lambdaScalarDecode(attributes)
	case terraformResourceTypeAWSECSTaskDefinition:
		result := cloudruntime.ExtractDeclaredContainerImages(attributes["container_definitions"])
		return valueAttributeDecode{
			ContainerImages:          result.Images,
			ContainerImagesTruncated: result.Truncated,
			ContainerImagesDegraded:  result.Degraded,
		}
	}
	return valueAttributeDecode{}
}

// ec2InstanceScalarDecode reads aws_instance's single comparable off one side.
// sourceKey is that side's own field name ("ami_id" observed, "ami" declared);
// both normalize onto the shared allowlist key "ami".
//
// The degraded outcome changes no verdict for this resource type -- "ami" is
// the only comparable, so an unreadable one already leaves Compared == 0 and
// reports value_comparison_inconclusive. It is reported anyway so the
// inconclusive finding's evidence names WHY, and so adding a second
// aws_instance comparable later inherits the rule rather than the #5861 shape.
func ec2InstanceScalarDecode(attributes map[string]any, sourceKey string) valueAttributeDecode {
	value, degraded := comparableScalarAttr(attributes, sourceKey)
	if degraded {
		return valueAttributeDecode{DegradedAttributes: []string{"ami"}}
	}
	if value == "" {
		return valueAttributeDecode{}
	}
	return valueAttributeDecode{Attributes: map[string]string{"ami": value}}
}

// comparableScalarAttr reads one leaf attribute intended for allowlisted
// value-drift comparison (cloudruntime.ValueAttributeAllowlistFor) off a
// JSON-decoded attributes object. It returns "" when the attribute is
// absent, blank, or is still a redaction marker (see redactedAnywhere)
// rather than genuine declared/observed data.
//
// A redacted scalar must never reach cloudruntime.attrValue as a non-empty
// string: coerceJSONString has no redaction concept and falls through its
// default fmt.Sprint(value) branch for an unrecognized map, which previously
// rendered a redacted "ami" as a garbage string like
// "map[marker:redacted:hmac-sha256:... reason:unknown_provider_schema
// source:resources.*.attributes.ami]". That string is present and non-empty,
// so it compared unequal to a real observed value and fired a false
// image_version_drift finding whose "declared" evidence was an internal
// collector encoding, not a value Terraform ever declared (#5859).
//
// Recognition happens here, at the decoder boundary, rather than by teaching
// coerceJSONString or cloudruntime about the redact.Value shape: this keeps
// the general-purpose leaf coercion helper (used for resource_type, address,
// and other identity fields that are never redacted) and the
// backend/provider-neutral cloudruntime package both ignorant of a
// collector-specific encoding detail. The terraform-state collector already
// counts every redaction at emission time via the
// eshu_dp_tfstate_redactions_applied_total{reason} counter (see
// go/internal/collector/tfstateruntime/metrics.go); this function only stops
// that already-recorded condition from being misread as comparable data
// downstream, so it does not need its own counter.
// The trailing degraded flag separates the redacted case from the absent one.
// Both yield an empty string, but only the first means the evidence existed and
// this pass could not use it (#5861).
func comparableScalarAttr(attributes map[string]any, key string) (value string, degraded bool) {
	raw, ok := attributes[key]
	if !ok {
		return "", false
	}
	if redactedAnywhere(raw) {
		return "", true
	}
	return strings.TrimSpace(coerceJSONString(raw)), false
}

// comparableScalarAttrSet reads every allowlisted scalar comparable a resource
// type is covered for, reporting the readable ones and naming the unreadable
// ones separately. forcedDegraded names keys the caller already knows are
// unreadable for a reason redaction cannot express; it may be nil.
//
// An unreadable key never lands in the returned map. That is the #5859 rule and
// it is absolute: coerceJSONString has no redaction concept, so a marker map
// renders through its fmt.Sprint default as a non-empty garbage string that
// compares unequal to any real value and fires a false image_version_drift
// whose "declared" evidence is an internal collector encoding.
//
// #5904 additionally suppressed the WHOLE set when any key was redacted,
// because erasing one of aws_lambda_function's two comparables left
// Comparable=2, Compared=1, no drift, Inconclusive()=false -- so Classify
// returned convergence, BuildCandidates dropped the ARN, and the
// generation-authoritative retire deleted whatever finding it held. That was
// the right call while cloudruntime had no way to hear about an unreadable
// attribute; the cost was that a real "version" drift alongside an unreadable
// "image_uri" reported as uncertainty rather than as the drift it is.
//
// cloudruntime.ResourceRow.DegradedAttributes now carries that fact per key, so
// the suppression is no longer needed to reach a safe verdict: the readable
// comparison keeps its evidence, and the unreadable key independently stops the
// pass from converging (#5861). Both halves are proven in
// aws_cloud_runtime_drift_value_completeness_test.go, and end to end against
// real rows in aws_cloud_runtime_drift_lambda_completeness_live_test.go.
//
// Only an unreadable comparable is reported degraded. A genuinely absent one is
// not, or every zip-packaged Lambda (no "image_uri" by design) would go
// inconclusive -- the noise objection #5861 records.
func comparableScalarAttrSet(
	attributes map[string]any,
	forcedDegraded map[string]struct{},
	keys ...string,
) (attrs map[string]string, degraded []string) {
	out := make(map[string]string, len(keys))
	// Iterating keys (allowlist order) rather than the attributes map keeps the
	// degraded list deterministic: it becomes evidence-atom values, so a
	// map-iteration order would make the same degraded pass emit a different
	// candidate on every run.
	for _, key := range keys {
		if _, forced := forcedDegraded[key]; forced || redactedAnywhere(attributes[key]) {
			degraded = append(degraded, key)
			continue
		}
		if v := strings.TrimSpace(coerceJSONString(attributes[key])); v != "" {
			out[key] = v
		}
	}
	if len(out) == 0 {
		return nil, degraded
	}
	return out, degraded
}

// lambdaScalarDecode reads aws_lambda_function's allowlisted comparables,
// adding one completeness rule comparableScalarAttrSet's redaction rule cannot
// express.
//
// comparableScalarAttrSet reports a REDACTED comparable as degraded but
// deliberately not an ABSENT one, because a zip-packaged Lambda has no image_uri
// by design and degrading on absence would report every one of them as
// value_comparison_inconclusive -- the noise objection #5861 records against
// widening that rule.
//
// package_type separates the two cases without any new collector plumbing: it
// is a real aws_lambda_function Terraform attribute AND the AWS collector
// already emits it (go/internal/collector/awscloud/services/lambda/scanner.go),
// so both decoders receive it. package_type == "Image" with no image_uri is
// therefore not "this resource has no image" -- an image-packaged function has
// one by definition -- it is "this side did not carry the image".
//
// This applies to BOTH decoders, deliberately, because the destructive outcome
// does not care which side was unreadable: whichever one is missing, Compared
// falls to 1 of 2, Classify returns "" and the retire deletes the finding.
//
//   - Observed side: Eshu's own defensive fallback in the AWS client
//     (services/lambda/awssdk/client.go) substitutes the ListFunctions
//     FunctionConfiguration when GetFunction returns a nil output, and that
//     value carries PackageType but no Code block, so mapFunction yields
//     PackageType "Image" with an empty ImageURI. This is a guarded branch
//     rather than an observed production occurrence -- a successful SDK
//     GetFunction does not return (nil, nil).
//   - Declared side: Terraform requires image_uri when package_type is Image,
//     so a state row asserting Image while carrying no image_uri is an
//     incomplete read of that state, not a function without an image. The
//     redaction rule cannot catch it, because redact never DROPS a scalar
//     (redact/policy.go) -- a redacted image_uri arrives as a marker and is
//     already suppressed, so genuine absence here means the state itself is
//     partial.
//
// Left untreated that side yields Comparable=2, Compared=1, Drifted=0,
// Inconclusive()=false, so Classify returns "" (convergence), BuildCandidates
// drops the ARN, and the generation-authoritative retire deletes a still-true
// finding -- the same destructive outcome #5904's redaction rule exists to
// prevent, reached through absence instead. Reporting image_uri as degraded
// declines that convergence while leaving the readable "version" comparison
// intact (#5861).
//
// package_type is read as a completeness SIGNAL only; it is not in
// cloudruntime's valueAttributeAllowlist and no drift is reported on it.
// Adding it there would turn an out-of-band Image-to-Zip repackaging into a
// real image_version_drift and needs its own accuracy review.
//
// A package_type that is itself a redaction marker does not match "Image" and
// falls through to the ordinary path. That is safe for the shape it actually
// occurs in -- whole-attribute redaction under an unknown provider schema hits
// every scalar, so image_uri is a marker too and the redaction rule reports it
// degraded. It would NOT be safe for a redacted package_type alongside a
// genuinely absent image_uri, which nothing here reports; that combination
// has no known producer, and this note exists so a future change that creates
// one does not inherit an unexamined assumption.
//
// One residual this rule does NOT close, named so it is not mistaken for
// solved: an absent `version` alongside an image_uri that compares equal still
// converges. package_type tells us whether an image_uri should exist; nothing
// in either payload tells us the same about `version`, so there is no evidence
// to build the equivalent completeness signal on. Both sources do carry it in
// practice (GetFunction and the ListFunctions fallback both return Version,
// and Terraform writes it as a computed attribute), which is why it is a
// narrow gap rather than a common one -- but "usually present" is not a signal
// this decoder can act on.
func lambdaScalarDecode(attributes map[string]any) valueAttributeDecode {
	var forced map[string]struct{}
	if lambdaImagePackagedWithoutImageURI(attributes) {
		forced = map[string]struct{}{"image_uri": {}}
	}
	attrs, degraded := comparableScalarAttrSet(attributes, forced, "image_uri", "version")
	return valueAttributeDecode{Attributes: attrs, DegradedAttributes: degraded}
}

// lambdaImagePackagedWithoutImageURI reports whether one side's attributes
// claim an image-packaged Lambda while carrying no image_uri. It is written to
// be side-agnostic: the caller applies it to the observed and the declared
// decoder alike, and in both cases the claim and the missing value contradict
// each other, which is the signal.
//
// Comparison is case-insensitive because the value is a provider/API string
// ("Image" from the AWS SDK, "Image" in Terraform state) rather than an
// identifier this repo mints.
// The image_uri test comes first even though package_type is the
// discriminating signal, because the two conditions commute and this order is
// cheaper on the healthy path: a Lambda that carries an image_uri -- every
// image-packaged function that was read successfully -- returns after one map
// read and one TrimSpace, without ever touching package_type. Ordering
// package_type first measured consistently slower, for no behavioral
// difference. Direction only: this package's README records no magnitude FOR
// THAT ORDERING COMPARISON, because the host it ran on could not support one.
func lambdaImagePackagedWithoutImageURI(attributes map[string]any) bool {
	if strings.TrimSpace(coerceJSONString(attributes["image_uri"])) != "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(coerceJSONString(attributes["package_type"])), "Image")
}

// redactedAnywhere reports whether value is itself a redaction marker map, or
// an array wrapping one.
//
// The two shapes come from different branches of the collector, and the
// distinction is worth stating precisely because the array one is NOT
// produced by the nil-resolver condition #5859 is about. Under
// redact.ActionRedact the terraform-state parser replaces the whole attribute
// with a single redactionMap (terraformstate/attributes.go:231), which is the
// bare-map shape. The array shape comes from the other branch: an
// ActionPreserve composite goes through applyLeafClassification, which
// recurses into array elements (attributes.go:264-268) and classifies each
// leaf with redact.SchemaKnown hardcoded (attributes.go:272), so a
// sensitive-NAMED leaf inside a repeated block is redacted individually and
// decodes as []any{map[string]any{marker}}.
//
// So this is a latent guard, not a live fix, on two counts: none of today's
// three allowlisted keys (ami, image_uri, version) is a composite, and the
// producing condition is a sensitive-key match under a known schema rather
// than #5859's unknown one. It is kept because the cost is one type
// assertion and the failure mode is silent -- allowlisting a composite
// attribute would otherwise reintroduce the exact garbage-string bug
// comparableScalarAttr exists to prevent.
// TestComparableScalarAttrTreatsArrayWrappedRedactionMarkerAsAbsent pins the
// boundary by failing if this function is reverted to a bare
// redact.IsRedactedValue(value) check.
func redactedAnywhere(value any) bool {
	if redact.IsRedactedValue(value) {
		return true
	}
	arr, ok := value.([]any)
	if !ok {
		return false
	}
	for _, elem := range arr {
		if redact.IsRedactedValue(elem) {
			return true
		}
	}
	return false
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
