// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package apilib is the pre-1.0 slim extraction of the eshu-core proof-read
// surface (readiness plus the admin proof reads) for the eshu-api satellite
// (Refs eshu-hq/eshu-api#456).
//
// The API is unstable and the audience is internal: only the eshu-api
// satellite and core-owned promotion tooling import this module. Do not
// re-export it to external consumers, and do not widen it beyond the proof
// reads without a new extraction decision. Breaking changes land without a
// deprecation window until the module reaches v1.
package apilib
