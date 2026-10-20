// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package kms maps AWS Key Management Service metadata into AWS cloud
// collector facts.
//
// The package owns scanner-level fact selection for customer master keys,
// AWS-managed keys when AWS makes them listable, aliases, and grants. It
// emits reported evidence only: key identity, usage, origin, manager, key
// state, rotation status, policy revision metadata, alias-to-key edges, and
// grant identity with the bounded operation list. It also emits the
// normalized, derived aws_resource_policy_permission fact per key-policy
// statement (effect, normalized action/resource patterns, condition
// key/operator NAMES, and derived grantee principal facts) — the resource-side analog of
// aws_iam_permission (PR4b of #1134).
//
// It does not call cryptographic operations (Encrypt, Decrypt,
// GenerateDataKey, Sign, Verify, ReEncrypt, GenerateMac, VerifyMac,
// GenerateDataKeyPair, GenerateDataKeyWithoutPlaintext) and does not call
// lifecycle mutation APIs (CreateKey, ScheduleKeyDeletion, CancelKeyDeletion,
// EnableKey, DisableKey, PutKeyPolicy, CreateGrant, RevokeGrant, RetireGrant,
// ReplicateKey, ImportKeyMaterial, DeleteImportedKeyMaterial). The awssdk
// adapter reads the key policy (GetKeyPolicy, owner-approved for the derived
// resource-policy fact) transiently; it never persists the raw key policy
// Statement bodies or condition values (only the bounded policy revision names,
// the normalized statement projection, and condition key/operator names), never
// persists grant encryption contexts, and never persists key material.
package kms
