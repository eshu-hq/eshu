// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 KMS client into the
// metadata-only KMS scanner interface.
//
// The adapter owns KMS pagination and per-key control-plane reads
// (ListKeys, DescribeKey, ListAliases, ListGrants, ListKeyPolicies,
// GetKeyRotationStatus, ListResourceTags), throttle classification, and
// per-call AWS API telemetry. It intentionally excludes every cryptographic
// operation (Encrypt, Decrypt, GenerateDataKey, GenerateDataKeyPair,
// GenerateDataKeyPairWithoutPlaintext, GenerateDataKeyWithoutPlaintext,
// Sign, Verify, ReEncrypt, GenerateMac, VerifyMac, DeriveSharedSecret) and
// every lifecycle mutation (CreateKey, ScheduleKeyDeletion,
// CancelKeyDeletion, EnableKey, DisableKey, EnableKeyRotation,
// DisableKeyRotation, PutKeyPolicy, CreateGrant, RevokeGrant, RetireGrant,
// ReplicateKey, ImportKeyMaterial, DeleteImportedKeyMaterial,
// UpdateKeyDescription, CreateAlias, UpdateAlias, DeleteAlias,
// TagResource, UntagResource). It never calls GetKeyPolicy, so key policy
// Statement bodies stay outside the scanner's read surface; only the
// bounded list of policy revision names from ListKeyPolicies is reported.
package awssdk
