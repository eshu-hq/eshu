// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"net/http"
)

// mcpAuthWiring carries the transport-auth middleware and startup-gate
// signal wireAPI computes from the resolved credential sources (issue
// #5168), so main.go can wrap the MCP HTTP transport (GET /sse,
// POST /mcp/message) with the SAME credential chain protecting /api/v0/*
// and tools/call's internal dispatch, and can refuse to start an
// unauthenticated HTTP deployment.
type mcpAuthWiring struct {
	// transportAuth wraps an http.Handler with the resolved shared-token
	// (apiKey) and scoped-token credential chain, built by
	// buildTransportAuthMiddleware (cmd/mcp-server/wiring.go) -- the SAME
	// constructor and the SAME authSourceConfigured value used for the
	// /api/v0/* authedHandler, so the two enforce identically. Never nil: even
	// with no credential source configured, it is still the correct
	// middleware to apply (it behaves exactly like /api/v0/*'s dev-mode-open
	// behavior in that case) -- requireMCPHTTPCredentialSource is the
	// separate gate that decides whether that configuration is allowed to
	// boot at all.
	transportAuth func(http.Handler) http.Handler
	// credentialSourceConfigured is true when at least one explicit
	// credential source is configured: a non-empty ESHU_API_KEY (which also
	// covers a persisted or ESHU_AUTO_GENERATE_API_KEY-generated key, since
	// runtime.ResolveAPIKey already folds those into the returned value), an
	// ESHU_SCOPED_TOKENS_FILE registry, or an ESHU_AUTH_RESOURCE_URI-gated
	// IdP bearer resolver.
	//
	// It intentionally does NOT count the always-wired Postgres-backed
	// identity-token resolver (scopedtoken.PostgresIdentityResolver): that
	// resolver is constructed unconditionally whenever Postgres is
	// available (i.e. always, for this binary), so treating its mere
	// presence as "configured" would make this gate permanently
	// unsatisfiable-false -- it would never refuse to start even when an
	// operator set none of the three explicit knobs above and the
	// identity_token_metadata table is empty. This is a conservative,
	// fail-closed default: an operator who provisions identity-backed
	// tokens through cmd/api's admin flows without ALSO setting one of the
	// three explicit knobs must pass ESHU_MCP_ALLOW_UNAUTHENTICATED=true to
	// start the standalone MCP server.
	//
	// Scope note (issue #5168, closed by the auth-headerless-bypass hardening
	// under #5161): this signal gates STARTUP; the per-request headerless gap
	// it used to leave open is now closed too. A configured scoped-token file
	// (ESHU_SCOPED_TOKENS_FILE) or OIDC resolver (ESHU_AUTH_RESOURCE_URI) with
	// the shared ESHU_API_KEY unset makes this true, so such a deployment
	// starts, AND the shared credential middleware now denies a HEADERLESS
	// request instead of letting it through: wireAPI threads this SAME value
	// (as authSourceConfigured) into buildTransportAuthMiddleware
	// (cmd/mcp-server/wiring.go), which the dev-mode-open branch of
	// query.authMiddlewareWithRoutePolicy (go/internal/query/auth.go) checks
	// before falling open on an empty shared token -- so a configured scoped
	// or OIDC resolver now closes that branch instead of being ignored by it.
	// The exclusion of the always-wired identity resolver here is a
	// deliberate, coordinator-approved default: counting a resolver that is
	// always present would make the gate permanently satisfied and useless.
	credentialSourceConfigured bool
}

// requireMCPHTTPCredentialSource is the "no silent open mode over HTTP" gate
// (issue #5168): ESHU_MCP_TRANSPORT=http with no resolvable credential
// source refuses to start with an actionable error, unless the operator sets
// ESHU_MCP_ALLOW_UNAUTHENTICATED=true. The stdio transport is never gated --
// it keeps its process/filesystem trust boundary regardless of credential
// configuration, matching the local `eshu mcp start` / `eshu local-host
// mcp-stdio` embedded flow.
func requireMCPHTTPCredentialSource(transport string, wiring mcpAuthWiring, allowUnauthenticated bool) error {
	if transport != "http" {
		return nil
	}
	if wiring.credentialSourceConfigured || allowUnauthenticated {
		return nil
	}
	return fmt.Errorf(
		"ESHU_MCP_TRANSPORT=http has no resolvable credential source: set ESHU_API_KEY " +
			"(or ESHU_AUTO_GENERATE_API_KEY=true), ESHU_SCOPED_TOKENS_FILE, or " +
			"ESHU_AUTH_RESOURCE_URI, or explicitly set ESHU_MCP_ALLOW_UNAUTHENTICATED=true " +
			"for loopback/dev use only (never expose this port publicly with the escape hatch set)",
	)
}
