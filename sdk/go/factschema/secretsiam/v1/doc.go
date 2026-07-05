// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload structs for the
// secrets_iam VAULT and K8S source-fact lanes (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md), decoded through the parent
// factschema package's kind-keyed seam (decode.go, decode_secretsiam.go).
//
// This package is scoped to Wave 4d of the typed-payload-decode migration
// (issue #4566/#4582), which partitions the secrets_iam family into three
// lanes:
//
//   - AWS IAM lane (aws_iam_principal, aws_iam_trust_policy, ...): already
//     migrated in #4568 and typed in sdk/go/factschema/iam/v1. Not this
//     package's concern.
//   - VAULT lane (vault_auth_role, vault_acl_policy, vault_kv_metadata): typed
//     here.
//   - K8S lane (k8s_service_account, k8s_workload_identity_use,
//     eks_irsa_annotation, eks_pod_identity_association,
//     k8s_gcp_workload_identity_binding): typed here.
//   - GCP IAM lane (gcp_iam_principal, gcp_iam_trust_policy,
//     gcp_iam_permission_policy): deferred to a future wave. The reducer's
//     secrets_iam_trust_chain_gcp.go continues reading those three kinds
//     through raw payloadString lookups, marked with an explicit "deferred:
//     gcp_iam lane" comment at each read site.
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
