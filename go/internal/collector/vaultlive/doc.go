// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package vaultlive is the live HashiCorp Vault source lane for the
// secrets/IAM posture collector family (issue #25).
//
// It observes Vault identity, trust, and secret-metadata posture and maps it to
// redacted secretsiam source facts (vault_auth_mount, vault_auth_role,
// vault_acl_policy, vault_identity_entity, vault_identity_alias,
// vault_kv_metadata, vault_secret_engine_mount). It is metadata-only by
// construction: the Client seam exposes no operation that reads a secret value,
// and the package never touches Vault KV /data endpoints, tokens, AppRole
// secret_id, or any credential material. Mount paths, key names, and accessors
// are fingerprinted by the secretsiam envelope builders before emission.
//
// Collectors observe source truth; the reducer decides graph truth. This
// package emits Eshu fact envelopes only and performs no graph writes and no
// trust-chain correlation.
package vaultlive
