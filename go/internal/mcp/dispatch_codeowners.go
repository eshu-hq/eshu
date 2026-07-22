// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "strconv"

// codeownersOwnershipRoute re-dispatches list_codeowners_ownership into the
// HTTP handler GET /api/v0/codeowners/ownership (query.CodeownersOwnershipHandler)
// rather than running its own Cypher; the handler owns the bounded read and
// the effective_owner precedence resolution.
func codeownersOwnershipRoute(args map[string]any) *route {
	return &route{method: "GET", path: "/api/v0/codeowners/ownership", query: map[string]string{
		"repository_id":     str(args, "repository_id"),
		"limit":             strconv.Itoa(intOr(args, "limit", 50)),
		"after_order_index": optionalIntString(args, "after_order_index"),
		"after_pattern":     str(args, "after_pattern"),
		"after_ref":         str(args, "after_ref"),
	}}
}

// optionalIntString formats args[key] as a decimal string when present,
// returning "" when absent. Unlike intString/intOr, this has no default: a
// keyset cursor's numeric component must stay empty rather than coerce to 0
// when the caller did not supply a cursor at all, or the handler's
// all-three-or-none cursor check would misread an absent cursor as a
// half-supplied one.
func optionalIntString(args map[string]any, key string) string {
	if _, ok := args[key]; !ok {
		return ""
	}
	return strconv.Itoa(intOr(args, key, 0))
}
