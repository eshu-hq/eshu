// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeownerstools

import (
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// Route selects the internal HTTP request for a CODEOWNERS ownership tool
// without executing it. It reports handled only for tools owned by this package.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "list_codeowners_ownership":
		return ownershipRequest(args), true
	default:
		return routecontract.Request{}, false
	}
}

// ownershipRequest re-dispatches list_codeowners_ownership into the HTTP
// handler GET /api/v0/codeowners/ownership (query.CodeownersOwnershipHandler)
// rather than running its own Cypher; the handler owns the bounded read and
// the effective_owner precedence resolution.
func ownershipRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/codeowners/ownership", Query: map[string]string{
		"repository_id":     args.String("repository_id"),
		"limit":             strconv.Itoa(args.IntOr("limit", 50)),
		"after_order_index": optionalIntString(args, "after_order_index"),
		"after_pattern":     args.String("after_pattern"),
		"after_ref":         args.String("after_ref"),
	}}
}

// optionalIntString formats args[key] as a decimal string when present,
// returning "" when absent. Unlike Arguments.IntOr, this has no default: a
// keyset cursor's numeric component must stay empty rather than coerce to 0
// when the caller did not supply a cursor at all, or the handler's
// all-three-or-none cursor check would misread an absent cursor as a
// half-supplied one. A present but non-numeric value still formats as "0",
// matching IntOr's fallback, because the caller did send the leg.
func optionalIntString(args routecontract.Arguments, key string) string {
	if _, ok := args[key]; !ok {
		return ""
	}
	return strconv.Itoa(args.IntOr(key, 0))
}
