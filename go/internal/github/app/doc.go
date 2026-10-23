// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package app mints GitHub App installation tokens for collector
// runtimes that cannot hold a long-lived credential.
//
// Both the repository-discovery runtime and the security-alert runtime mint
// at most one token per unit of work from the same RS256 JWT shape, the same
// bounded retry budget, and the same final-versus-retryable fault classes.
// Minted tokens are returned to the caller and never stored, logged, or
// attached to telemetry by this package.
package app
