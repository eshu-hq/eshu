// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines schema-version-1 typed payload structs for secrets_iam
// source facts other than the legacy aws_iam_principal struct in iam/v1.
//
// The package covers AWS IAM source-detail facts, GCP IAM facts, Kubernetes
// identity and RBAC facts, Vault identity and mount facts, and source coverage
// warnings. Parent factschema decode and encode functions own kind dispatch,
// schema-major validation, and classified input_invalid errors; these structs
// own only the wire payload shape.
//
// Every fact kind's wire string is UNDERSCORE-separated
// (go/internal/facts.VaultAuthRoleFactKind and siblings), matching the
// AWS/IAM/GCP/Azure convention rather than the DOTTED incident/kubernetes_live
// convention. The values in the parent package's decode.go MATCH those wire
// strings byte-for-byte; TestFactSchemaKindsMatchWireFactKinds (reducer side)
// asserts each stays byte-equal to its facts.*FactKind counterpart.
//
// Each struct's required fields are non-pointer with no omitempty tag; the
// decode seam rejects a payload that omits one, or supplies an explicit JSON
// null for one, with a classified ClassificationInputInvalid error naming the
// field, never a zero-value struct. Optional fields are pointers or slices
// carrying omitempty, so an absent value decodes to nil and stays distinct
// from an observed zero or empty set.
//
// VaultACLPolicy.Rules is a typed []VaultACLPolicyRule, not a
// map[string]any pass-through: the collector emitter
// (secretsiam.vaultPolicyRulePayloads) always emits a well-known
// {path_fingerprint, path_depth, capabilities} shape per rule, so the nested
// array is modeled the same way the top-level struct is -- fully typed, no
// Attributes catch-all.
//
// The reducer decodes only the latest struct for each kind. Version shims for
// an older schema major live in the parent factschema package's decode seam
// (decodeLatestMajor in decode.go), never in this package or in reducer
// handler code.
package v1
