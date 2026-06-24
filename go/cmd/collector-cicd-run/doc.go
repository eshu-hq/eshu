// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package main provides the hosted CI/CD run collector command.
//
// The command selects one enabled claim-driven ci_cd_run collector instance,
// resolves provider credential environment references at runtime, and commits
// bounded GitHub Actions run facts through the shared collector service. It
// exposes the standard hosted runtime health, readiness, metrics, and admin
// status endpoints.
package main
