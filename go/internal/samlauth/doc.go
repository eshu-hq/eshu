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
//
// The SAML library is crewjam/saml, replaced in go.mod by the eshu-hq/saml fork
// for GO-2026-6909. This package parses metadata only and never calls the
// affected ParseResponse path; go/internal/query owns that call and recovers a
// library panic.
package samlauth
