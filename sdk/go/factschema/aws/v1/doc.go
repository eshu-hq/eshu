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
// A consumer reaches it through the decoded struct with the reducer's
// payloadAttributes(resource.Attributes) helper, which returns
// Attributes["attributes"] as a map — never env.Payload["attributes"][key].
// Typing those service-specific fields per resource_type is deferred as a
// distinct, larger increment (design §7, remaining families); it does not
// weaken the identity validation this package adds today.
//
// Non-polymorphic structs are each scoped to one fact kind with a known field
// set and carry no Attributes pass-through.
//
// The reducer decodes only the latest struct for each kind. Version shims
// for an older schema major live in the parent factschema package's decode
// seam (decodeLatestMajor in decode.go), never in this package or in
// reducer handler code.
package v1
