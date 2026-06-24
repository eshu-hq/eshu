// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package samlauth validates SAML service-provider inputs for Eshu-owned login.
//
// The package wraps maintained SAML metadata and service-provider primitives
// with Eshu-specific invariants: metadata must match the configured issuer,
// assertions must normalize to hash-only subjects and group claims, and replay
// fingerprints must never expose raw SAML identifiers. Callers still own durable
// provider configuration, atomic replay reservation, role/grant resolution, and
// browser-session creation.
package samlauth
