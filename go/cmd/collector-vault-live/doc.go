// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command collector-vault-live runs the read-only, metadata-only Vault
// collector for the secrets/IAM posture lane (#25, #1356). It selects one
// claim-capable vault_live instance, claims configured Vault cluster/namespace
// work, snapshots metadata endpoints with a read-only token, and commits
// redacted source facts through the shared collector commit boundary; it never
// reads a secret value.
package main
