// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package mcpsetup builds and installs MCP client configuration for `eshu mcp
// setup` (and its `eshu m` alias).
//
// The package renders platform-specific snippets for local stdio and hosted
// HTTP transport, resolves the auth posture (per-user token, SSO, or the
// legacy shared key) via RFC 9728 discovery, merges the eshu server entry
// into an existing client config file, and runs staged reachability
// verification. It never embeds a raw secret in generated output: hosted
// snippets always reference a credential by env-var name, and every
// credential-bearing type documents which posture it applies to.
//
// The package is process-neutral in the sense that matters here: it reads no
// cobra flags, resolves no Eshu config or credential from the process
// environment, and prints nothing. It does touch the environment twice, both
// incidental to that boundary -- os.UserHomeDir in DescribeWriteTarget, to
// shorten a path for display, and the RFC 9728 discovery probe's own
// short-timeout HTTP client. go/cmd/eshu's mcp_setup_cmd.go is the
// thin cobra wrapper that resolves process state -- flags, the shared
// *APIClient, and output streams -- and passes it in as parameters or as the
// HealthProber/QueryProber interfaces this package exports. This split is
// mechanical, not a design choice specific to this package: cmd/eshu is
// `package main`, so nothing can import it, and any symbol reading cobra
// flags, process env, or the exit-code contract has to stay there.
package mcpsetup
