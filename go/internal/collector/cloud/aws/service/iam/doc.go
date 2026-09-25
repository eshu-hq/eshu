// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package iam scans AWS IAM source truth into AWS cloud fact observations.
//
// It emits IAM roles, users, managed policies, instance profiles, trust
// principals, IAM relationships, derived aws_iam_permission facts, and
// secrets_iam_posture source facts. Policy facts are normalized,
// metadata-only projections of inline, attached managed, and role trust policy
// statements (effect, action set, resource pattern, condition key/operator
// summary). The scanner never emits the raw policy JSON body or condition
// values; the SDK adapter normalizes documents at the wiring boundary.
//
// The package defines scanner-owned client interfaces so unit tests can inject
// fakes without mocking the full AWS SDK for Go v2 surface. SDK adapters belong
// at runtime wiring boundaries.
package iam
