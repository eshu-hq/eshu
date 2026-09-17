// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeflowtools

import "github.com/eshu-hq/eshu/go/internal/mcp/routecontract"

// Route selects the internal HTTP request for a code-flow tool without
// executing it. It reports handled only for the four tools this package owns.
// Family membership is an explicit name-to-path map, never a prefix match:
// the four names share the dispatch_ prefix with nothing else in the
// registry, but a prefix claim would silently absorb any future tool that
// borrowed the spelling.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	paths := map[string]string{
		"dispatch_taint_path":   "/api/v0/code/flow/taint-path",
		"dispatch_reaching_def": "/api/v0/code/flow/reaching-def",
		"dispatch_cfg_summary":  "/api/v0/code/flow/cfg-summary",
		"dispatch_pdg_summary":  "/api/v0/code/flow/pdg-summary",
	}
	path, ok := paths[toolName]
	if !ok {
		return routecontract.Request{}, false
	}
	return flowRequest(path, args), true
}

// flowRequest builds the one six-key body all four code-flow tools share and
// pairs it with the tool's own POST path under /api/v0/code/flow/, which
// query's code handler mounts. The handler owns every bound; nothing selected
// here can turn into a 400 except a blank repo_id, the one field the handler
// requires ("repo_id is required" after trimming).
//
// The two integers deliberately default differently. limit defaults to 25,
// the same value the handler substitutes for anything at or below zero before
// clamping anything above 100 down to 100 (codeFlowDefaultLimit and
// codeFlowMaxLimit in query's code_flow.go), so an omitted limit and the
// dispatcher's default are indistinguishable at the handler and no value
// rejects; the advertised schema's 1..100 range describes the handler's clamp,
// not a dispatcher-side check. line defaults to 0, which is not a filter
// value: the handler floors negatives to 0 and treats 0 as "no line filter",
// and a symbol-only query that matches several functions is reported as
// ambiguous exactly when line is 0, so forwarding a positive default would
// silently suppress the handler's ambiguity signal for callers who set no
// line at all.
//
// The four string keys travel even when empty so the handler sees an explicit
// blank filter rather than a missing field; language, symbol, and file_path
// are optional narrowing filters whose loss widens a page silently rather
// than failing it.
func flowRequest(path string, args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "POST", Path: path, Body: map[string]any{
		"repo_id":   args.String("repo_id"),
		"language":  args.String("language"),
		"symbol":    args.String("symbol"),
		"file_path": args.String("file_path"),
		"line":      args.IntOr("line", 0),
		"limit":     args.IntOr("limit", 25),
	}}
}
