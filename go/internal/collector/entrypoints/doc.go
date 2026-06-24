// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package entrypoints renders manifest-backed collector command entrypoints.
//
// The package owns deterministic generation for the shared hosted collector
// startup wrapper, claim service wiring, and generic claim-runtime configuration
// selection. Provider-specific target decoding stays in each collector command
// package so credentials, target validation, and source contracts remain local
// to the provider that owns them.
package entrypoints
