// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcodetools

import "github.com/eshu-hq/eshu/go/internal/mcp/routecontract"

// Route selects the internal HTTP request for a dead-code tool without
// executing it. It reports handled only for the three tools this package
// owns. Family membership is an explicit name switch, never a prefix match:
// find_dead_iac shares the find_dead_ spelling but belongs to the IaC family,
// and a prefix claim would silently absorb it or any future borrower.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "find_dead_code":
		return routecontract.Request{Method: "POST", Path: "/api/v0/code/dead-code", Body: map[string]any{
			"repo_id":                args.String("repo_id"),
			"limit":                  args.IntOr("limit", 100),
			"exclude_decorated_with": args.StringSlice("exclude_decorated_with"),
		}}, true
	case "investigate_dead_code":
		return routecontract.Request{Method: "POST", Path: "/api/v0/code/dead-code/investigate", Body: map[string]any{
			"repo_id":                args.String("repo_id"),
			"language":               args.String("language"),
			"limit":                  args.IntOr("limit", 100),
			"offset":                 args.IntOr("offset", 0),
			"exclude_decorated_with": args.StringSlice("exclude_decorated_with"),
		}}, true
	case "find_cross_repo_dead_code":
		return routecontract.Request{Method: "POST", Path: "/api/v0/code/dead-code/cross-repo", Body: map[string]any{
			"repo_id":                args.String("repo_id"),
			"consumer_repo_ids":      stringValues(args, "consumer_repo_ids"),
			"language":               args.String("language"),
			"limit":                  args.IntOr("limit", 100),
			"exclude_decorated_with": args.StringSlice("exclude_decorated_with"),
		}}, true
	default:
		return routecontract.Request{}, false
	}
}

// The three bodies preserve the exact wire shape the root switch arms sent.
//
// limit defaults to 100, the same value the dead-code handlers substitute for
// anything at or below zero before clamping anything above 500 down to 500
// (deadCodeDefaultLimit and deadCodeMaxLimit in query's code_dead_code.go),
// so an omitted limit and the dispatcher's default are indistinguishable at
// the handler and no selected value can reject. offset defaults to 0, the
// first page; the investigate handler floors negatives to 0 and caps the
// offset at 2000. Only the cross-repo route rejects a blank repo_id
// ("repo_id is required"); the scan and investigate routes accept one and
// widen to every repository the caller's scope grants, which is why repo_id
// still travels as an explicit empty string rather than being dropped.
//
// exclude_decorated_with keeps the root stringSlice semantics through
// routecontract: absent or malformed input travels as a nil []any (JSON
// null), while a present empty list stays a non-nil empty []any (JSON []).
// The handlers decode both into the same empty []string, but the bytes on
// the wire are part of the preserved contract, so the tests pin nil-ness,
// not just length.

// stringValues mirrors the root dispatcher's helper of the same name for
// consumer_repo_ids: it narrows the decoded list to its non-empty string
// members and always returns a non-nil []string, so an absent argument
// serializes as [] where exclude_decorated_with would serialize as null.
// The asymmetry is inherited wire shape, not a new policy.
func stringValues(args routecontract.Arguments, key string) []string {
	raw := args.StringSlice(key)
	values := make([]string, 0, len(raw))
	for _, value := range raw {
		text, ok := value.(string)
		if ok && text != "" {
			values = append(values, text)
		}
	}
	return values
}
