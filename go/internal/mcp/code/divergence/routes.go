// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergencetools

import (
	routecontract "github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

// Route selects the internal HTTP request for a code-divergence tool
// without executing it. It reports handled only for the two tools this
// package owns. Family membership is an explicit name switch, never a prefix
// match.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "find_code_divergence":
		return routecontract.Request{Method: "POST", Path: "/api/v0/code/divergence/findings", Body: map[string]any{
			"repo_id":       args.String("repo_id"),
			"kind":          args.String("kind"),
			"limit":         args.IntOr("limit", 25),
			"offset":        args.IntOr("offset", 0),
			"include_tests": args.BoolOr("include_tests", false),
		}}, true
	case "investigate_code_divergence":
		return routecontract.Request{Method: "POST", Path: "/api/v0/code/divergence/investigate", Body: map[string]any{
			"repo_id":       args.String("repo_id"),
			"kind":          args.String("kind"),
			"fingerprint":   args.String("fingerprint"),
			"include_tests": args.BoolOr("include_tests", false),
		}}, true
	default:
		return routecontract.Request{}, false
	}
}
