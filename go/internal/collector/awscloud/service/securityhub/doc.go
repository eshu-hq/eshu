// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package securityhub maps AWS Security Hub configuration and posture metadata
// into AWS cloud collector facts.
//
// The scanner emits hub, enabled-standard, control, member-account,
// custom-action-target, insight-summary, and aggregate finding-count facts. It
// intentionally excludes Security Hub finding bodies, insight filter
// expressions, resource details, remediation text, notes, product fields,
// user-defined fields, network details, process details, and mutation APIs.
package securityhub
