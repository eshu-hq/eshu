// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import (
	"fmt"
	"strings"
)

// AttributeShapeError reports that a service-specific attribute value a
// reducer consumer reads out of Resource.Attributes or Relationship.Attributes
// was present but did not match the type the consumer requires (issue #4631,
// the bounded-subset typing increment on top of Contract System v1 §3.3's
// polymorphic Attributes pass-through).
//
// This is distinct from the fact-kind-level *factschema.DecodeError: this
// package cannot import factschema (factschema imports aws/v1, so the reverse
// import would cycle), and the failure this type reports is scoped to one
// service-specific attribute field inside an otherwise identity-valid
// aws_resource/aws_relationship fact, not the whole envelope. A caller (a
// reducer handler) that receives a non-nil error from one of this file's
// Decode* functions MUST treat the field as malformed and route it through the
// same quarantine/dead-letter path an envelope-level decode failure uses
// (input_invalid), never substitute a zero value silently — silently
// substituting a zero value for a present-but-malformed field is exactly the
// "wrong graph value that looks fine" failure mode this typing closes.
type AttributeShapeError struct {
	// Field names the attribute path that failed validation, e.g.
	// "attributes.encrypted" or "attributes.attachments[1].instance_id".
	Field string
	// Reason is a short human-readable description of what was wrong.
	Reason string
}

// Error implements the error interface.
func (e *AttributeShapeError) Error() string {
	return fmt.Sprintf("aws attribute shape: field %q: %s", e.Field, e.Reason)
}

func newAttributeShapeError(field, reason string) *AttributeShapeError {
	return &AttributeShapeError{Field: field, Reason: reason}
}

// ResourceAnchorAttributes is the typed shape of the workload/service anchor
// tags an aws_resource fact of ANY resource_type may carry at the TOP LEVEL of
// its Attributes pass-through (workload_id/workload_ids,
// service_name/service_names, environment). These are collector-side
// workload-correlation tags, distinct from the AWS-scanner service-specific
// fields nested one level deeper under Attributes["attributes"] (see
// Resource.Attributes doc and ResourceNestedAnchorAttributes below). Every
// field is optional: a resource with no workload/service anchor observed
// leaves them at the zero value, which the workload-cloud-relationship
// materialization and service-anchor consumers already treat as "no anchor"
// rather than an error — only a PRESENT value of the wrong JSON type is a
// decode failure.
type ResourceAnchorAttributes struct {
	// WorkloadIDs is the union of the scalar "workload_id" value (if any) and
	// the "workload_ids" array (if any), in that order, mirroring the
	// reducer's payloadStrings(attrs, "workload_id", "workload_ids") union so
	// a caller applying the same dedup/sort it already applies gets a
	// byte-identical result for valid facts.
	WorkloadIDs []string
	// ServiceNames is the same scalar+slice union for
	// "service_name"/"service_names".
	ServiceNames []string
	// Environment is the top-level "environment" string tag.
	Environment string
}

// ResourceNestedAnchorAttributes is the typed shape of the service_name /
// service_names fields a small allow-listed set of resource types publish
// inside the NESTED Attributes["attributes"] object rather than at the top
// level (go/internal/reducer's shouldAdmitAWSAttributeServiceAnchor names the
// allow-listed resource types: aws_apprunner_service, aws_ecs_service,
// aws_proton_service, aws_vpclattice_listener, aws_vpclattice_service,
// aws_xray_sampling_rule).
type ResourceNestedAnchorAttributes struct {
	// ServiceNames is the scalar+slice union for the nested
	// "service_name"/"service_names" keys.
	ServiceNames []string
}

// ResourceEC2VolumeAttributes is the typed shape of the nested
// Attributes["attributes"] fields the block-device KMS posture consumer reads
// off an aws_ec2_volume aws_resource fact.
type ResourceEC2VolumeAttributes struct {
	// Encrypted reports the volume's encryption flag, when the scanner
	// observed it. Nil means unreported, distinct from an observed false.
	Encrypted *bool
	// Attachments lists the volume's instance attachments, when observed.
	Attachments []EC2VolumeAttachment
}

// EC2VolumeAttachment is one instance-attachment entry inside an EC2 volume's
// nested attachments list.
type EC2VolumeAttachment struct {
	// InstanceID is the attached EC2 instance id, when reported.
	InstanceID string
	// State is the attachment lifecycle state, when reported.
	State string
}

// ResourceKMSKeyAttributes is the typed shape of the nested
// Attributes["attributes"] field the block-device KMS posture consumer reads
// off an aws_kms_key aws_resource fact.
type ResourceKMSKeyAttributes struct {
	// KeyManager is the KMS key's manager designation (e.g. "CUSTOMER" or
	// "AWS"), when reported.
	KeyManager string
}

// ResourceIAMInstanceProfileAttributes is the typed shape of the nested
// Attributes["attributes"] field the IAM instance-profile HAS_ROLE edge
// extractor reads off an aws_iam_instance_profile aws_resource fact.
type ResourceIAMInstanceProfileAttributes struct {
	// RoleARNs lists the ARNs of IAM roles attached to the instance profile.
	RoleARNs []string
}

// ResourceEC2InstanceAttributes is the typed shape of the nested
// Attributes["attributes"] field the EC2 instance identity node projection
// (#5448) reads off an aws_ec2_instance aws_resource fact. It carries only the
// AMI identity the running instance was launched from; every other EC2
// instance property is intentionally scoped to the separate
// ec2_instance_posture fact and its own CloudResource node materialization
// (#1146 PR-A), which this identity fact never disturbs.
type ResourceEC2InstanceAttributes struct {
	// AMIID is the AMI (ImageId) the instance was launched from, when
	// reported.
	AMIID string
}

// RelationshipCloudWatchAlarmObservesMetricAttributes is the typed shape of
// the nested Attributes["attributes"] field the observability coverage
// correlation consumer reads off a cloudwatch_alarm_observes_metric
// aws_relationship fact.
type RelationshipCloudWatchAlarmObservesMetricAttributes struct {
	// Dimensions lists the alarm's redacted CloudWatch metric dimension
	// summary. A dimension whose Value is empty carries no resolvable
	// resource identity (a customer-tag dimension redacted at the scanner)
	// and is not itself a decode failure — the caller filters it the same way
	// it does today.
	Dimensions []MetricDimension
}

// MetricDimension is one CloudWatch metric dimension entry.
type MetricDimension struct {
	// Value is the dimension's resource-identity value, when it is an
	// unredacted AWS system dimension (InstanceId, FunctionName, …).
	Value string
}

// RelationshipXRaySamplingRuleMatchesServiceAttributes is the typed shape of
// the nested Attributes["attributes"] field the observability coverage
// correlation consumer reads off an xray_sampling_rule_matches_service
// aws_relationship fact.
type RelationshipXRaySamplingRuleMatchesServiceAttributes struct {
	// ServiceName is the X-Ray sampling rule's matched service name.
	ServiceName string
}

// nestedAttributes returns attrs["attributes"] as a map[string]any, mirroring
// the reducer's payloadAttributes helper. It is duplicated here (rather than
// imported) because this package cannot import go/internal/reducer, and the
// logic is small enough that duplicating it is cheaper than a new shared
// module boundary. A missing, nil, or wrong-typed "attributes" key returns nil
// — this is not itself a decode failure; the AWS resource/relationship
// identity is still valid with no nested attributes.
func nestedAttributes(attrs map[string]any) map[string]any {
	raw, ok := attrs["attributes"]
	if !ok || raw == nil {
		return nil
	}
	switch typed := raw.(type) {
	case map[string]any:
		return typed
	case map[string]string:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[key] = value
		}
		return out
	default:
		return nil
	}
}

// attributeString reads attrs[key] as a string. An absent or nil value
// decodes as "" with no error (the normal "unreported" state); a PRESENT
// value of any other JSON type is a decode failure named by field.
func attributeString(attrs map[string]any, field, key string) (string, error) {
	value, ok := attrs[key]
	if !ok || value == nil {
		return "", nil
	}
	s, ok := value.(string)
	if !ok {
		return "", newAttributeShapeError(field, fmt.Sprintf("want string, got %T", value))
	}
	// Trim to preserve the pre-typing payloadString normalization
	// (strings.TrimSpace): a valid-but-padded value like " prod " must
	// normalize to "prod", and " CUSTOMER " must match the "CUSTOMER" check
	// (#5243). Wrong JSON types are still rejected above.
	return strings.TrimSpace(s), nil
}

// attributeBool reads attrs[key] as a *bool. An absent or nil value decodes as
// a nil pointer (unreported, distinct from an observed false) with no error; a
// PRESENT value of any other JSON type is a decode failure named by field.
func attributeBool(attrs map[string]any, field, key string) (*bool, error) {
	value, ok := attrs[key]
	if !ok || value == nil {
		return nil, nil
	}
	b, ok := value.(bool)
	if !ok {
		return nil, newAttributeShapeError(field, fmt.Sprintf("want bool, got %T", value))
	}
	return &b, nil
}

// attributeStringSlice reads attrs[key] as a []string, matching the JSON
// decode's native []any-of-strings shape (or, for a struct built directly in
// Go without a JSON round trip, a plain []string). An absent or nil value
// decodes as nil with no error; a PRESENT value that is not an array, or an
// array containing a non-string entry, is a decode failure named by field
// (the entry's own field path for a bad entry).
func attributeStringSlice(attrs map[string]any, field, key string) ([]string, error) {
	value, ok := attrs[key]
	if !ok || value == nil {
		return nil, nil
	}
	if strs, ok := value.([]string); ok {
		out := make([]string, 0, len(strs))
		for _, s := range strs {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out, nil
	}
	raw, ok := value.([]any)
	if !ok {
		return nil, newAttributeShapeError(field, fmt.Sprintf("want array, got %T", value))
	}
	out := make([]string, 0, len(raw))
	for i, entry := range raw {
		s, ok := entry.(string)
		if !ok {
			return nil, newAttributeShapeError(fmt.Sprintf("%s[%d]", field, i), fmt.Sprintf("want string, got %T", entry))
		}
		// Trim + drop empty-after-trim to match the pre-typing payloadStrings
		// normalization (#5243).
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out, nil
}

// attributeStringUnion returns the union of a scalar key's single value (if
// any) and a slice key's values (if any), in that order, mirroring the
// reducer's payloadStrings(attrs, scalarKey, sliceKey) helper. Either half
// being a present-but-wrong-typed value is a decode failure.
func attributeStringUnion(attrs map[string]any, scalarField, scalarKey, sliceField, sliceKey string) ([]string, error) {
	var out []string
	scalar, err := attributeString(attrs, scalarField, scalarKey)
	if err != nil {
		return nil, err
	}
	if scalar != "" {
		out = append(out, scalar)
	}
	sliceValues, err := attributeStringSlice(attrs, sliceField, sliceKey)
	if err != nil {
		return nil, err
	}
	out = append(out, sliceValues...)
	return out, nil
}

// DecodeResourceAnchorAttributes decodes the top-level workload/service anchor
// tags off an already-decoded aws_resource Resource (see
// ResourceAnchorAttributes). Call this instead of reading
// resource.Attributes["workload_id"]/["workload_ids"]/["service_name"]/
// ["service_names"]/["environment"] directly.
func DecodeResourceAnchorAttributes(resource Resource) (ResourceAnchorAttributes, error) {
	attrs := resource.Attributes
	workloadIDs, err := attributeStringUnion(attrs, "workload_id", "workload_id", "workload_ids", "workload_ids")
	if err != nil {
		return ResourceAnchorAttributes{}, err
	}
	serviceNames, err := attributeStringUnion(attrs, "service_name", "service_name", "service_names", "service_names")
	if err != nil {
		return ResourceAnchorAttributes{}, err
	}
	environment, err := attributeString(attrs, "environment", "environment")
	if err != nil {
		return ResourceAnchorAttributes{}, err
	}
	return ResourceAnchorAttributes{
		WorkloadIDs:  workloadIDs,
		ServiceNames: serviceNames,
		Environment:  environment,
	}, nil
}

// DecodeResourceNestedAnchorAttributes decodes the nested
// Attributes["attributes"] service_name/service_names fields off an
// already-decoded aws_resource Resource (see ResourceNestedAnchorAttributes).
// Callers gate this on the allow-listed resource types themselves (the decode
// does not know the admission policy).
func DecodeResourceNestedAnchorAttributes(resource Resource) (ResourceNestedAnchorAttributes, error) {
	nested := nestedAttributes(resource.Attributes)
	serviceNames, err := attributeStringUnion(nested, "attributes.service_name", "service_name", "attributes.service_names", "service_names")
	if err != nil {
		return ResourceNestedAnchorAttributes{}, err
	}
	return ResourceNestedAnchorAttributes{ServiceNames: serviceNames}, nil
}

// DecodeResourceEC2VolumeAttributes decodes the nested
// Attributes["attributes"] encrypted/attachments fields off an
// already-decoded aws_ec2_volume aws_resource Resource (see
// ResourceEC2VolumeAttributes).
func DecodeResourceEC2VolumeAttributes(resource Resource) (ResourceEC2VolumeAttributes, error) {
	nested := nestedAttributes(resource.Attributes)
	encrypted, err := attributeBool(nested, "attributes.encrypted", "encrypted")
	if err != nil {
		return ResourceEC2VolumeAttributes{}, err
	}
	attachments, err := decodeEC2VolumeAttachments(nested)
	if err != nil {
		return ResourceEC2VolumeAttributes{}, err
	}
	return ResourceEC2VolumeAttributes{Encrypted: encrypted, Attachments: attachments}, nil
}

// anyMapSlice normalizes a payload array-of-objects value to []any, tolerating
// both the []any-of-map[string]any shape a real JSON decode always produces
// and the native []map[string]any shape a Resource/Relationship value built
// directly in Go (as reducer tests and any in-process synthetic fact do) may
// carry instead. Both represent the identical valid shape; a value of any
// other type returns ok=false so the caller reports the field-specific decode
// error.
func anyMapSlice(raw any) ([]any, bool) {
	switch typed := raw.(type) {
	case []any:
		return typed, true
	case []map[string]any:
		out := make([]any, len(typed))
		for i, entry := range typed {
			out[i] = entry
		}
		return out, true
	case []map[string]string:
		// The ECS running-task collector builds its containers[] attribute as
		// []map[string]string (awscloud/services/ecs.taskContainerMaps): a
		// direct in-Go envelope (test fixtures, any synthetic fact built
		// without a JSON round trip) preserves that concrete type, while a
		// real collected fact always round-trips through JSON storage and
		// arrives as []any of map[string]any instead. Both represent the
		// identical valid shape.
		out := make([]any, len(typed))
		for i, entry := range typed {
			converted := make(map[string]any, len(entry))
			for key, value := range entry {
				converted[key] = value
			}
			out[i] = converted
		}
		return out, true
	default:
		return nil, false
	}
}

func decodeEC2VolumeAttachments(nested map[string]any) ([]EC2VolumeAttachment, error) {
	raw, ok := nested["attachments"]
	if !ok || raw == nil {
		return nil, nil
	}
	entries, ok := anyMapSlice(raw)
	if !ok {
		return nil, newAttributeShapeError("attributes.attachments", fmt.Sprintf("want array, got %T", raw))
	}
	out := make([]EC2VolumeAttachment, 0, len(entries))
	for i, entry := range entries {
		m, ok := entry.(map[string]any)
		if !ok {
			return nil, newAttributeShapeError(fmt.Sprintf("attributes.attachments[%d]", i), fmt.Sprintf("want object, got %T", entry))
		}
		instanceID, err := attributeString(m, fmt.Sprintf("attributes.attachments[%d].instance_id", i), "instance_id")
		if err != nil {
			return nil, err
		}
		state, err := attributeString(m, fmt.Sprintf("attributes.attachments[%d].state", i), "state")
		if err != nil {
			return nil, err
		}
		out = append(out, EC2VolumeAttachment{InstanceID: instanceID, State: state})
	}
	return out, nil
}

// DecodeResourceKMSKeyAttributes decodes the nested Attributes["attributes"]
// key_manager field off an already-decoded aws_kms_key aws_resource Resource
// (see ResourceKMSKeyAttributes).
func DecodeResourceKMSKeyAttributes(resource Resource) (ResourceKMSKeyAttributes, error) {
	nested := nestedAttributes(resource.Attributes)
	keyManager, err := attributeString(nested, "attributes.key_manager", "key_manager")
	if err != nil {
		return ResourceKMSKeyAttributes{}, err
	}
	return ResourceKMSKeyAttributes{KeyManager: keyManager}, nil
}

// DecodeResourceIAMInstanceProfileAttributes decodes the nested
// Attributes["attributes"] role_arns field off an already-decoded
// aws_iam_instance_profile aws_resource Resource (see
// ResourceIAMInstanceProfileAttributes).
func DecodeResourceIAMInstanceProfileAttributes(resource Resource) (ResourceIAMInstanceProfileAttributes, error) {
	nested := nestedAttributes(resource.Attributes)
	roleARNs, err := attributeStringSlice(nested, "attributes.role_arns", "role_arns")
	if err != nil {
		return ResourceIAMInstanceProfileAttributes{}, err
	}
	return ResourceIAMInstanceProfileAttributes{RoleARNs: roleARNs}, nil
}

// DecodeResourceEC2InstanceAttributes decodes the nested
// Attributes["attributes"] ami_id field off an already-decoded
// aws_ec2_instance aws_resource Resource (see ResourceEC2InstanceAttributes).
func DecodeResourceEC2InstanceAttributes(resource Resource) (ResourceEC2InstanceAttributes, error) {
	nested := nestedAttributes(resource.Attributes)
	amiID, err := attributeString(nested, "attributes.ami_id", "ami_id")
	if err != nil {
		return ResourceEC2InstanceAttributes{}, err
	}
	return ResourceEC2InstanceAttributes{AMIID: amiID}, nil
}

// DecodeRelationshipCloudWatchAlarmObservesMetricAttributes decodes the nested
// Attributes["attributes"] dimensions field off an already-decoded
// cloudwatch_alarm_observes_metric aws_relationship Relationship (see
// RelationshipCloudWatchAlarmObservesMetricAttributes).
func DecodeRelationshipCloudWatchAlarmObservesMetricAttributes(rel Relationship) (RelationshipCloudWatchAlarmObservesMetricAttributes, error) {
	nested := nestedAttributes(rel.Attributes)
	raw, ok := nested["dimensions"]
	if !ok || raw == nil {
		return RelationshipCloudWatchAlarmObservesMetricAttributes{}, nil
	}
	entries, ok := anyMapSlice(raw)
	if !ok {
		return RelationshipCloudWatchAlarmObservesMetricAttributes{}, newAttributeShapeError(
			"attributes.dimensions", fmt.Sprintf("want array, got %T", raw),
		)
	}
	dims := make([]MetricDimension, 0, len(entries))
	for i, entry := range entries {
		m, ok := entry.(map[string]any)
		if !ok {
			return RelationshipCloudWatchAlarmObservesMetricAttributes{}, newAttributeShapeError(
				fmt.Sprintf("attributes.dimensions[%d]", i), fmt.Sprintf("want object, got %T", entry),
			)
		}
		value, err := attributeString(m, fmt.Sprintf("attributes.dimensions[%d].value", i), "value")
		if err != nil {
			return RelationshipCloudWatchAlarmObservesMetricAttributes{}, err
		}
		dims = append(dims, MetricDimension{Value: value})
	}
	return RelationshipCloudWatchAlarmObservesMetricAttributes{Dimensions: dims}, nil
}

// DecodeRelationshipXRaySamplingRuleMatchesServiceAttributes decodes the
// nested Attributes["attributes"] service_name field off an already-decoded
// xray_sampling_rule_matches_service aws_relationship Relationship (see
// RelationshipXRaySamplingRuleMatchesServiceAttributes).
func DecodeRelationshipXRaySamplingRuleMatchesServiceAttributes(rel Relationship) (RelationshipXRaySamplingRuleMatchesServiceAttributes, error) {
	nested := nestedAttributes(rel.Attributes)
	serviceName, err := attributeString(nested, "attributes.service_name", "service_name")
	if err != nil {
		return RelationshipXRaySamplingRuleMatchesServiceAttributes{}, err
	}
	return RelationshipXRaySamplingRuleMatchesServiceAttributes{ServiceName: serviceName}, nil
}
