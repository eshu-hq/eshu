// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package main runs the mock-github binary, a minimal, self-contained stand-in
// for github.com's OAuth2 web-application-flow and REST identity endpoints,
// used only by the F-9 (issue #5170) auth-mcp E2E stack to prove Eshu's
// go/internal/githublogin connector end-to-end without depending on a live
// GitHub App or GitHub Enterprise Server instance.
//
// It serves the exact endpoint set githubConnector
// (go/internal/githublogin/connector.go) calls — GET
// /login/oauth/authorize, POST /login/oauth/access_token, GET /user, GET
// /user/emails, GET /user/memberships/orgs, GET /user/teams — plus an
// unauthenticated GET / root matching the reachability probe
// go/internal/githublogin/provider_connection_test_probe.go uses for the
// admin console's Test-connection button. All identity data (login, numeric
// id, email, org, team memberships) comes from one configured synthetic
// example.test identity (Server.identity), selected at process startup
// through MOCK_GITHUB_* environment variables, not at request time. This
// binary must never be pointed at by anything other than a test client_id;
// it carries no client registry, no credential check, and no rate limiting.
package main
