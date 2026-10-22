// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// RouteResolver maps a tool call to the HTTP route dispatch would send. The
// mcp root injects its resolveRoute; this package cannot import it.
type RouteResolver func(name string, arguments map[string]any) (*routecontract.Request, error)

// routeClass is the auth-relevant classification of one dispatched route.
type routeClass struct {
	// resolved is false when no route could be built; such a tool is
	// admitted only for an all-access caller.
	resolved bool
	// sharedKeyOnly marks #5167 Group C routes (raw Cypher, visualize,
	// operator-wide suppression) that only the shared key may call.
	sharedKeyOnly bool
	// pendingRowFiltering marks #5167 Group B routes no scoped caller may call.
	pendingRowFiltering bool
	// allowlisted marks routes on the scoped-token tenant-filter allowlist.
	allowlisted bool
}

// classifyRoute runs the query package's own route ledgers, so the router
// agrees with the API auth middleware by construction.
func classifyRoute(method, path string) routeClass {
	req := &http.Request{Method: method, URL: &url.URL{Path: path}}
	return routeClass{
		resolved:            true,
		sharedKeyOnly:       query.IsSharedKeyOnlyRoute(req),
		pendingRowFiltering: query.IsPendingRowFilteringRoute(req),
		allowlisted:         query.ScopedHTTPRouteSupportsTenantFilter(req),
	}
}

// RouteProbeArguments returns, in order, the argument maps the engine tries
// when it classifies a tool's route without a real call: the required-only
// example, then a placeholder for every property (some routes need one of
// several optional selectors), then no arguments. The mcp root tests that one
// of them resolves for every registered tool and lands on the same auth class
// as the dispatch tests' own arguments.
func RouteProbeArguments(schema any) []map[string]any {
	return []map[string]any{
		ExampleArguments(schema),
		placeholderArguments(schema, true),
		{},
	}
}

// classifyTool resolves a tool's route with the first RouteProbeArguments map
// that builds one. A tool with no buildable route is unresolved.
func classifyTool(resolve RouteResolver, tool toolcontract.ToolDefinition) routeClass {
	if resolve == nil {
		return routeClass{}
	}
	for _, args := range RouteProbeArguments(tool.InputSchema) {
		if route, err := resolve(tool.Name, args); err == nil && route != nil && route.Path != "" {
			return classifyRoute(route.Method, route.Path)
		}
	}
	return routeClass{}
}

// access is the per-call credential view the router filters with.
type access struct {
	auth    query.AuthContext
	present bool
}

// accessFromContext reads the AuthContext the transport attached to the
// request. Its absence (stdio, ESHU_MCP_ALLOW_UNAUTHENTICATED) means the
// router cannot filter and must say so.
func accessFromContext(ctx context.Context) access {
	auth, ok := query.AuthContextFromContext(ctx)
	return access{auth: auth, present: ok}
}

// admits reports whether a credential may call a route of class c. It refuses
// only what the API auth middleware always refuses for that credential, so it
// never hides a tool the caller could use. Decisions the middleware makes
// from deployment policy (the all-scope console session off the allowlist,
// and all-scope callers on grant-bound routes) are admitted here and left to
// the middleware, which still enforces them at dispatch.
func (a access) admits(c routeClass) bool {
	if !a.present || a.auth.Mode == query.AuthModeShared {
		return true
	}
	if !c.resolved || c.sharedKeyOnly {
		return false
	}
	if c.pendingRowFiltering && a.auth.Mode != query.AuthModeBrowserSession {
		// A #5167 Group B route refuses every scoped bearer; only the
		// console-session policy below can admit it.
		return false
	}
	if c.allowlisted {
		return true
	}
	if a.auth.Mode == query.AuthModeBrowserSession {
		// Off the allowlist, pending row-filtering routes included, the
		// middleware admits only a tenant-bound all-scopes console session,
		// and only under deployment policy.
		return a.auth.AllScopes &&
			strings.TrimSpace(a.auth.TenantID) != "" &&
			strings.TrimSpace(a.auth.WorkspaceID) != ""
	}
	// A scoped bearer (or an unrecognized mode) off the allowlist, pending
	// row-filtering routes included, is always refused.
	return false
}

// accessView is the find_tool response block that says whether the
// candidates were filtered for the caller's credential.
type accessView struct {
	Filtered bool   `json:"filtered"`
	Reason   string `json:"reason,omitempty"`
}

func (a access) view() accessView {
	if !a.present {
		return accessView{Filtered: false, Reason: "no_auth_context"}
	}
	return accessView{Filtered: true}
}
