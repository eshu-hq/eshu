// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package main provides the hosted CI/CD run collector command.
//
// In its default live mode the command selects one enabled claim-driven
// ci_cd_run collector instance, resolves provider credential environment
// references at runtime, and commits bounded GitHub Actions run facts through
// the shared collector service. Passing -mode=cassette -cassette-file=<path>
// instead replays recorded ci.run/ci.artifact facts through a credential-free
// collector.Service, which the golden-corpus gate uses to exercise the CI lane
// without live GitHub Actions credentials. It exposes the standard hosted
// runtime health, readiness, metrics, and admin status endpoints.
package main
