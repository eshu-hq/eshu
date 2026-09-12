// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package routes

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/chain"
	"github.com/eshu-hq/eshu/go/internal/query/graph/rows"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// labelSet indexes the code-entity labels a route handler may carry
// (mirrors chain.AnchorLabelDisjunction). It gates any label interpolated
// into a Cypher traversal so the label text is never attacker-influenced.
var labelSet = func() map[string]struct{} {
	set := map[string]struct{}{}
	for _, label := range strings.Split(chain.AnchorLabelDisjunction, "|") {
		set[label] = struct{}{}
	}
	return set
}()

// LabelAllowed reports whether label is a known code-entity label.
func LabelAllowed(label string) bool {
	_, ok := labelSet[label]
	return ok
}

// JoinRouteRows left-joins endpoint rows with their handler rows on
// the endpoint id, emitting one row per (endpoint, handler) and a single
// null-handler row for endpoints with no handler (the prior OPTIONAL MATCH
// semantics). Ordering mirrors the prior
// `ORDER BY repo_id, path, http_method, handler_name, handler_id`.
//
// The two reads feeding the join stay in codequery/route_handlers.go: their
// *CodeHandler bodies carry queryplan source_sha256 pins
// (grandfathered_non_hot.go) and can never change text. The join itself
// is unpinned, so it lives here.
func JoinRouteRows(endpointRows, handlerRows []map[string]any, limit int) []map[string]any {
	handlersByEndpoint := map[string][]map[string]any{}
	for _, hr := range handlerRows {
		eid := querycontract.StringVal(hr, "endpoint_id")
		handlersByEndpoint[eid] = append(handlersByEndpoint[eid], hr)
	}
	out := make([]map[string]any, 0, len(endpointRows))
	for _, ep := range endpointRows {
		eid := querycontract.StringVal(ep, "endpoint_id")
		epFramework := querycontract.StringVal(ep, "endpoint_framework")
		newBase := func() map[string]any {
			return map[string]any{"endpoint_id": eid, "path": querycontract.StringVal(ep, "path"), "repo_id": querycontract.StringVal(ep, "repo_id")}
		}
		handlers := handlersByEndpoint[eid]
		if len(handlers) == 0 {
			row := newBase()
			row["framework"] = epFramework
			out = append(out, row)
			continue
		}
		for _, hr := range handlers {
			row := newBase()
			row["http_method"] = querycontract.StringVal(hr, "http_method")
			row["framework"] = querycontract.FirstNonEmpty(querycontract.StringVal(hr, "route_framework"), epFramework)
			row["handler_id"] = querycontract.StringVal(hr, "handler_id")
			row["handler_name"] = querycontract.StringVal(hr, "handler_name")
			row["handler_file_path"] = querycontract.StringVal(hr, "handler_file_path")
			row["handler_language"] = querycontract.StringVal(hr, "handler_language")
			row["handler_start_line"] = querycontract.IntVal(hr, "handler_start_line")
			row["handler_end_line"] = querycontract.IntVal(hr, "handler_end_line")
			out = append(out, row)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return routeRowSortKey(out[i]) < routeRowSortKey(out[j])
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func routeRowSortKey(row map[string]any) string {
	return querycontract.StringVal(row, "repo_id") + "\x00" + querycontract.StringVal(row, "path") + "\x00" +
		querycontract.StringVal(row, "http_method") + "\x00" + querycontract.StringVal(row, "handler_name") + "\x00" +
		querycontract.StringVal(row, "handler_id")
}

// RelationshipRows returns the directional CALLS rows around a handler
// whose label the caller already resolved: one single-clause directional
// path per direction with the KNOWN handler as the (labelled) path start,
// projecting raw nodes(path); the discovered caller/callee is the far
// endpoint, extracted in Go. (The prior UNION-in-CALL shape is what the
// pinned NornicDB build corrupts to literal expression text (#5287); node
// identity inequality stays on id/uid because NornicDB mis-evaluates a
// `<>` between whole nodes.) An empty label resolves to no rows without
// touching the graph.
//
// The label resolution itself stays in codequery/route_handlers.go: its
// *CodeHandler body carries a queryplan source_sha256 pin
// (grandfathered_non_hot.go) and can never change text.
func RelationshipRows(
	ctx context.Context,
	graph querycontract.GraphQuery,
	handlerID string,
	req Request,
	access querycontract.RepositoryAccessFilter,
	label string,
) ([]map[string]any, error) {
	if label == "" {
		return []map[string]any{}, nil
	}
	incoming, err := DirectionRows(ctx, graph, handlerID, req, access, label, "incoming")
	if err != nil {
		return nil, err
	}
	outgoing, err := DirectionRows(ctx, graph, handlerID, req, access, label, "outgoing")
	if err != nil {
		return nil, err
	}
	return append(incoming, outgoing...), nil
}

// DirectionRows runs one directional CALLS traversal anchored on the
// labelled handler start and returns the far-endpoint entities as relationship
// rows tagged with the direction.
func DirectionRows(
	ctx context.Context,
	graph querycontract.GraphQuery,
	handlerID string,
	req Request,
	access querycontract.RepositoryAccessFilter,
	label string,
	direction string,
) ([]map[string]any, error) {
	// Fail closed on an unvalidated label: this is an exported leaf
	// function, so injection safety cannot rest on callers alone.
	if !LabelAllowed(label) {
		return []map[string]any{}, nil
	}
	far := "callee"
	pattern := "(handler:" + label + ")-[:CALLS*1.." + strconv.Itoa(req.MaxDepth) + "]->(callee)"
	if direction == "incoming" {
		far = "caller"
		pattern = "(handler:" + label + ")<-[:CALLS*1.." + strconv.Itoa(req.MaxDepth) + "]-(caller)"
	}
	cypher := "MATCH path = " + pattern + `
		WHERE coalesce(handler.id, handler.uid) = $handler_id
		  AND coalesce(` + far + `.id, ` + far + `.uid) <> coalesce(handler.id, handler.uid)` +
		EntityAccessPredicate(access, far) + PathAccessPredicate(access, "path") + `
		RETURN length(path) as depth, nodes(path) as chain
		ORDER BY depth, coalesce(` + far + `.id, ` + far + `.uid)
		LIMIT $limit`
	params := AccessParams(access, map[string]any{"handler_id": handlerID, "limit": req.Limit + 1})
	records, err := graph.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(records))
	for _, row := range records {
		entity := rows.RouteToCallerEntityFromChain(row["chain"])
		if entity == nil || querycontract.StringVal(entity, "entity_id") == "" {
			continue
		}
		entity["direction"] = direction
		entity["depth"] = querycontract.IntVal(row, "depth")
		out = append(out, entity)
	}
	return out, nil
}
