// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package auth holds the OpenAPI path fragments for the browser session,
// CSRF-safe dashboard, and admin identity-provider routes under
// /api/v0/auth/...: provider discovery and sign-in posture (Routes), the
// local-account setup flow (Setup), session token issuance and refresh
// (Tokens), TOTP enrollment and verification (TOTP), admin-facing identity
// reads (AdminReads), admin mutations (AdminMutations), provider-config
// admin CRUD (AdminProviderConfigs), and the tenant sign-in policy
// (SignInPolicy). openapi/spec.go concatenates all eight in the order
// listed here.
//
// Each file holds exactly one exported string constant. This package
// imports nothing beyond the standard library string literals; it MUST NOT
// import the openapi parent (spec.go imports this package, not the other
// way around).
package auth
