// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package main runs the eshu-core-api binary, the Wave-6 core serving
// binary (step-3 gating ruling D1).
//
// The binary answers the full forwarded query surface from stores
// directly: flag parsing, store wiring, and ListenAndServe only. Route
// registration, the auth middleware chain, and secret-free handler wiring
// live in go/internal/query (serve.go); the secret-sealed auth adapters
// live here in package main because go/internal/query must never import
// secretcrypto. The thin main mounts only the import-cycle-forced extras
// (CLI-support handlers, the service-intelligence report, and the ask
// engine) on the constructor's bare mux.
//
// One-way no-loop invariant: this binary takes no --core-base-url flag and
// never dials any API backend. Traffic flows one way, satellite into this
// Service. The satellite keeps its own --core-base-url flag.
package main
