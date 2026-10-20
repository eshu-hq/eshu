// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 IAM client to the IAM scanner
// contract.
//
// The package owns IAM pagination, AWS API telemetry, throttle detection, trust
// policy decoding, permissions-boundary detail, OIDC provider metadata
// fingerprinting, and policy-document normalization for source records returned
// by AWS. It reads inline and attached managed policy documents and normalizes
// them into metadata-only iam.PolicyStatement values (effect, action set,
// resource pattern, condition key/operator summary); it never returns the raw
// policy JSON body or condition values. OIDC provider URLs are fingerprinted,
// and client IDs and thumbprints are counted only. The per-principal managed policy document
// fan-out is bounded to avoid an N+1 against IAM. Scanner packages own fact
// selection and do not import the AWS SDK directly.
package awssdk
