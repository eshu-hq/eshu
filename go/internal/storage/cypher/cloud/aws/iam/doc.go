// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package iam owns the AWS IAM graph writers: the can-assume, can-perform,
// escalation, and instance-profile-role edge writers, and the secrets/IAM
// graph writer (SecretsIAMGraphWriter) for vault-backed identity posture.
//
// The parent cypher package owns the shared Executor, Statement, Operation,
// and retry contracts; this leaf imports them as sourcecypher and the parent
// never imports this leaf. Reducer wiring in go/cmd/reducer imports this
// leaf directly for construction.
package iam
