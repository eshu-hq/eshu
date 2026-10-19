// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package identity owns secrets and identity-posture source fact kinds:
// AWS IAM principals and policies, GCP IAM bindings, Kubernetes service
// accounts and RBAC, and Vault mounts, roles, and entities.
//
// The kinds declared here are source evidence only. Reducers own posture
// and trust-chain truth; this package decides no admission and projects
// nothing. The family declaration uses contract.Declare, the identity
// function the static registry generator reads; see the fact-kind
// registry for the generated kind rows.
//
// The package depends only on the standard library and facts/contract. It
// must not import the parent facts package: the schema registry and family
// callers reach in through this package, never the reverse. The flat
// identity/ directory (not identity/secrets_iam/) is deliberate: the whole
// file is the IAM family, so one more level would only restate the name.
package identity
