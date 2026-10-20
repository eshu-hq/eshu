// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Organizations client to the
// scanner-owned Organizations metadata contract.
//
// The adapter uses the us-east-1 Organizations endpoint, records bounded API
// call telemetry, traverses roots/OUs/accounts, reads policy summaries and
// target bindings, and lists delegated-administrator service bindings. It
// never calls Organizations mutation APIs or policy body reads.
package awssdk
