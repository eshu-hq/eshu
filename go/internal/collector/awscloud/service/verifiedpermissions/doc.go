// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package verifiedpermissions maps Amazon Verified Permissions policy store,
// policy, and identity source metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for policy stores, policies,
// and identity sources plus relationships for policy-in-store,
// identity-source-in-store, and identity-source-to-Cognito-user-pool evidence.
// Cedar policy statement bodies, schema bodies, policy template bodies, and any
// authorization-request payload stay outside this package contract: the scanner
// is metadata-only and emits policy and identity source ids and classifications
// only, never the Cedar source.
package verifiedpermissions
