// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetestools

import (
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// Route selects the internal HTTP request for a Kubernetes-correlation tool
// without executing it. It reports handled only for tools owned by this
// package.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "list_kubernetes_correlations":
		return correlationsRequest(args), true
	default:
		return routecontract.Request{}, false
	}
}

// correlationsRequest maps list_kubernetes_correlations to the bounded
// read-only route GET /api/v0/kubernetes/correlations, which
// query.KubernetesHandler serves; the handler owns the limit bound (limit is
// required and must be 1..200, anything else is a 400), the anchor rule (at
// least one of scope_id, cluster_id, workload_object_id, namespace, image_ref,
// or source_digest), and the fact_id keyset paging behind
// after_correlation_id.
//
// Ten keys travel together, and they do not fail alike when one is lost.
// limit is required, so dropping it 400s every request. The six anchors are
// required as a group, so dropping one 400s only the caller whose sole anchor
// it was and silently widens every other caller's page past the anchor they
// named. outcome and drift_kind are optional equality filters, so dropping one
// returns 200 over every outcome or drift kind. after_correlation_id is the
// keyset cursor, so dropping it returns 200 from the first page again and a
// caller continuing a truncated page sees the rows it already has.
//
// The default limit of 50 is the dispatcher's, so the handler never sees an
// omitted limit through MCP; a caller who stringifies the number gets the
// default rather than an error, because routecontract.Arguments.IntOr does
// not parse strings.
func correlationsRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/kubernetes/correlations", Query: map[string]string{
		"after_correlation_id": args.String("after_correlation_id"),
		"cluster_id":           args.String("cluster_id"),
		"drift_kind":           args.String("drift_kind"),
		"image_ref":            args.String("image_ref"),
		"limit":                strconv.Itoa(args.IntOr("limit", 50)),
		"namespace":            args.String("namespace"),
		"outcome":              args.String("outcome"),
		"scope_id":             args.String("scope_id"),
		"source_digest":        args.String("source_digest"),
		"workload_object_id":   args.String("workload_object_id"),
	}}
}
