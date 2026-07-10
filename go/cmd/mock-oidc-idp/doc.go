// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package main runs the mock-oidc-idp binary, a minimal, self-contained
// OpenID Connect Authorization Code identity provider used only by local and
// CI browser-auth E2E suites (issue #4971, epic #4962) to prove Eshu's SSO
// login flow against a real OIDC counterparty without depending on a live
// third-party IdP.
//
// It serves four endpoints — GET /.well-known/openid-configuration, GET
// /authorize, POST /token, and GET /jwks — backed by one configured
// synthetic example.test identity (Server.identity) and a fixed, in-code RSA
// keypair (see keys.go). /authorize redirects back to the caller's
// redirect_uri immediately, with no login form: there is exactly one
// identity to choose from, selected at process startup through MOCK_OIDC_*
// environment variables, not at request time. This binary must never be
// pointed at by anything other than a test client_id; it carries no client
// registry, no credential check, and no rate limiting.
package main
