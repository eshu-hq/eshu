// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK IAM Access Analyzer responses into scanner-owned
// Access Analyzer records.
//
// The adapter owns analyzer, archive-rule, finding-list, and unused-access
// detail pagination plus per-call AWS API telemetry. It maps only safe metadata:
// aggregate finding counts, archive-rule names, analyzer bindings, and
// per-resource unused-access timestamps.
package awssdk
