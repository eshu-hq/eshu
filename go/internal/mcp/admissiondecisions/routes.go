// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admissiondecisionstools

import (
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// Route selects the internal HTTP request for an admission-decisions tool
// without executing it. It reports handled only for tools owned by this
// package.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "list_admission_decisions":
		return decisionsRequest(args), true
	default:
		return routecontract.Request{}, false
	}
}

// decisionsRequest maps list_admission_decisions to the bounded read-only
// route GET /api/v0/evidence/admission-decisions, which
// query.EvidenceHandler serves; the handler owns the limit bound (nonpositive
// becomes the 50-row default, over-200 caps at 200), the
// state vocabulary, and the anchor-pair rule.
//
// Eight keys travel together, and they do not fail alike when one is lost.
// domain, scope_id, and generation_id are required, so dropping any of them
// 400s every request. anchor_kind and anchor_id must arrive together, so
// dropping one half 400s while dropping both silently widens the page past
// the anchor the caller named. state, include_evidence, and limit each have a
// handler default, so losing one returns 200 with a wider state set, no
// evidence rows, or a 50-row page the caller did not ask for.
//
// include_evidence is always sent as an explicit "true" or "false". Only a
// Go bool is honoured -- the strings "true" and "1" fall back to false -- so
// a client that stringifies the flag silently gets no evidence rows.
func decisionsRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/evidence/admission-decisions", Query: map[string]string{
		"anchor_id":        args.String("anchor_id"),
		"anchor_kind":      args.String("anchor_kind"),
		"domain":           args.String("domain"),
		"generation_id":    args.String("generation_id"),
		"include_evidence": strconv.FormatBool(args.BoolOr("include_evidence", false)),
		"limit":            strconv.Itoa(args.IntOr("limit", 50)),
		"scope_id":         args.String("scope_id"),
		"state":            args.String("state"),
	}}
}
