// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package vaultapi is the net/http implementation of the vaultlive.Client
// metadata-only Vault seam. It talks to Vault's REST API using only the
// standard library — no Vault SDK dependency — and is metadata-only by
// construction: doRequest rejects any path containing a KV "/data/" segment, so
// no code path can read a secret value, and the adapter only ever issues the
// metadata, list, and describe calls the secrets/IAM posture lane needs.
//
// The adapter holds a short-lived read-only token supplied by the caller and
// bound to the secrets/IAM read-only policy; Eshu never persists it. Vault
// addresses, paths, names, and policy bodies are handed to the vaultlive source
// and secretsiam envelope builders, which fingerprint them before emission. The
// adapter hashes ACL policy bodies itself so the raw policy body never leaves
// this package; it performs no other redaction.
package vaultapi
