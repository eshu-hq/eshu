// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload structs for the
// "aws" fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md), decoded through the parent
// factschema package's kind-keyed seam (decode.go, decode_aws.go).
//
// AWS fact kinds live here: Resource (aws_resource), Relationship
// (aws_relationship), DNSRecord (aws_dns_record), ImageReference
// (aws_image_reference), SecurityGroupRule (aws_security_group_rule), Warning
// (aws_warning), EC2InstancePosture (ec2_instance_posture),
// RDSInstancePosture (rds_instance_posture), S3BucketPosture
// (s3_bucket_posture), and S3ExternalPrincipalGrant
// (s3_external_principal_grant). Each struct's required fields are
// non-pointer with no omitempty tag; the decode seam rejects a payload that
// omits one, or supplies an explicit JSON null for one, with a classified
// ClassificationInputInvalid error naming the field, never a zero-value
// struct. Optional fields are pointers, slices, or maps carrying omitempty, so
// an absent value decodes to nil and stays distinct from an observed zero.
//
// Resource and Relationship are polymorphic generic envelopes: one fact kind
// carries every AWS resource type or relationship verb, so each struct types
// and validates only the shared identity contract and the common fields
// multiple consumers read. Every remaining, service- or verb-specific
// payload key passes through untyped in the struct's Attributes field. The
// collector emitter does not flatten those fields to the top level: it
// nests them one level deep under a single "attributes" object
// (payload["attributes"] = {"engine": ..., "role_arns": ...}), so a service
// field lands at Attributes["attributes"][key], not directly on Attributes.
//
// attribute_shapes.go types the BOUNDED SUBSET of that pass-through a
// consumer actually reads today (issue #4631): a small set of
// resource_type/relationship_type-keyed structs
// (ResourceEC2VolumeAttributes, ResourceKMSKeyAttributes,
// ResourceIAMInstanceProfileAttributes,
// RelationshipCloudWatchAlarmObservesMetricAttributes,
// RelationshipXRaySamplingRuleMatchesServiceAttributes) plus two
// resource-type-agnostic anchor shapes (ResourceAnchorAttributes,
// ResourceNestedAnchorAttributes) for the workload/service-anchor tags a
// resource of any type may carry. Each has a validating Decode* accessor
// (DecodeResourceEC2VolumeAttributes, DecodeResourceKMSKeyAttributes, …) that
// a reducer consumer calls on the already-decoded Resource/Relationship
// instead of reading Attributes["attributes"][key] (or, for the anchor
// shapes, Attributes[key]) directly. A present-but-wrong-JSON-typed value
// returns a classified *AttributeShapeError the caller MUST route through the
// same input_invalid dead-letter path a missing required identity field
// already uses — never substitute a silently coerced or zero value. This is
// a deliberately bounded catalog, not a general per-resource-type schema: the
// remaining ~470+ AWS resource types and their service-specific fields stay
// untyped in Attributes exactly as before.
//
// Escape hatch: when a NEW consumer needs to read a service-specific field
// this file does not yet type, add a typed accessor here following the same
// pattern (a small struct plus a Decode<Resource|Relationship><Shape>
// function that validates each field's JSON type) rather than reverting to a
// raw payloadString/payloadStrings/payloadAttributes map lookup in the
// reducer handler. If the field instead represents a wholly new kind of fact
// (not a refinement of an existing aws_resource/aws_relationship envelope),
// mint a dedicated fact kind instead of overloading the polymorphic
// Attributes pass-through further. Typing the remaining, not-yet-consumed
// service-specific fields per resource_type ahead of a real consumer is
// deferred as a distinct, larger increment (design §7, remaining families);
// it does not weaken the identity validation this package adds today.
//
// Non-polymorphic structs are each scoped to one fact kind with a known field
// set and carry no Attributes pass-through.
//
// The reducer decodes only the latest struct for each kind. Version shims
// for an older schema major live in the parent factschema package's decode
// seam (decodeLatestMajor in decode.go), never in this package or in
// reducer handler code.
package v1
