// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command audit-preflight validates a competitive-audit issue body against the
// Eshu preflight contract.
//
// It reads the issue body from -file or stdin, runs
// github.com/eshu-hq/eshu/go/internal/auditpreflight, prints any findings, and
// exits non-zero when the issue is missing required evidence or uses an invalid
// gap class or owner surface.
//
//	go run ./cmd/audit-preflight -file issue.md
//	gh issue view <N> --json body -q .body | go run ./cmd/audit-preflight
package main
