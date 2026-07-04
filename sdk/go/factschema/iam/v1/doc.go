// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload structs for the AWS
// IAM fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md), decoded through the parent
// factschema package's kind-keyed seam (decode.go, decode_iam.go).
//
// Three fact kinds live here: Permission (aws_iam_permission),
// ResourcePolicyPermission (aws_resource_policy_permission), and Principal
// (aws_iam_principal). Each is a normalized, metadata-only projection of an
// IAM policy statement or principal; none carries the raw policy JSON body
// or any condition value. Each struct's required fields are non-pointer with
// no omitempty tag; the decode seam rejects a payload that omits one, or
// supplies an explicit JSON null for one, with a classified
// ClassificationInputInvalid error naming the field, never a zero-value
// struct. Optional fields are pointers or slices carrying omitempty, so an
// absent value decodes to nil and stays distinct from an observed zero or
// empty set.
//
// Unlike aws/v1.Resource and aws/v1.Relationship, every struct in this
// package is fully typed: an IAM policy statement's fields (actions,
// resources, principals, effect, source) are a closed, well-known set, so
// there is no Attributes pass-through here.
//
// The reducer decodes only the latest struct for each kind. Version shims
// for an older schema major live in the parent factschema package's decode
// seam (decodeLatestMajor in decode.go), never in this package or in
// reducer handler code.
package v1
