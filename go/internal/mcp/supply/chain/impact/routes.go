// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychainimpacttools

import (
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// Route selects the internal HTTP request for a supply-chain-impact tool
// without executing it. It reports handled only for the four tools this
// package owns.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "list_supply_chain_impact_findings":
		return findingsRequest(args), true
	case "count_supply_chain_impact_findings":
		return aggregateCountRequest(args), true
	case "get_supply_chain_impact_inventory":
		return aggregateInventoryRequest(args), true
	case "explain_supply_chain_impact":
		return explanationRequest(args), true
	default:
		return routecontract.Request{}, false
	}
}

// findingsRequest maps list_supply_chain_impact_findings to the bounded
// read-only route GET /api/v0/supply-chain/impact/findings, which
// query.SupplyChainHandler.listImpactFindings serves.
//
// limit is required by the handler (requiredSupplyChainImpactFindingLimit) and
// so is a scope anchor: at least one of cve_id, advisory_id, package_id,
// repository_id, subject_digest, image_ref, impact_status, ecosystem,
// workload_id, service_id, environment, severity, priority_bucket, or a
// positive min_priority_score, or the handler 400s -- except a scoped token
// with no grants, which is answered with an empty page before the anchor is
// checked. after_finding_id is the keyset cursor; dropping it re-serves page
// one instead of failing. advisory_id also accepts the ghsa_id/osv_id aliases
// at the handler, but the wire key stays advisory_id here since the handler
// performs that fallback itself from the raw query values.
func findingsRequest(args routecontract.Arguments) routecontract.Request {
	query := map[string]string{
		"advisory_id":        args.String("advisory_id"),
		"after_finding_id":   args.String("after_finding_id"),
		"cve_id":             args.String("cve_id"),
		"ecosystem":          args.String("ecosystem"),
		"environment":        args.String("environment"),
		"ghsa_id":            args.String("ghsa_id"),
		"image_ref":          args.String("image_ref"),
		"impact_status":      args.String("impact_status"),
		"limit":              strconv.Itoa(args.IntOr("limit", 50)),
		"min_priority_score": strconv.Itoa(args.IntOr("min_priority_score", 0)),
		"osv_id":             args.String("osv_id"),
		"package_id":         args.String("package_id"),
		"priority_bucket":    args.String("priority_bucket"),
		"profile":            args.String("profile"),
		"repository_id":      args.String("repository_id"),
		"service_id":         args.String("service_id"),
		"severity":           args.String("severity"),
		"sort":               args.String("sort"),
		"subject_digest":     args.String("subject_digest"),
		"suppression_state":  args.String("suppression_state"),
		"workload_id":        args.String("workload_id"),
	}
	// include_suppressed is omitted when the caller did not set it so the
	// query string stays free of the empty key; the handler accepts a missing
	// value as the documented default (false) and only rejects a non-true/false
	// string.
	if encoded := boolStr(args, "include_suppressed"); encoded != "" {
		query["include_suppressed"] = encoded
	}
	return routecontract.Request{Method: "GET", Path: "/api/v0/supply-chain/impact/findings", Query: query}
}

// explanationRequest maps explain_supply_chain_impact to the single-row
// read-only route GET /api/v0/supply-chain/impact/explain, which
// query.SupplyChainHandler.explainImpact serves.
//
// The handler requires finding_id alone, or advisory_id/cve_id plus one of
// package_id, repository_id, subject_digest, image_ref, workload_id, or
// service_id; losing the scope leg without dropping the advisory/CVE anchor
// (or the reverse) turns a bounded explanation into a 400. This route carries
// no limit or paging key: the handler answers exactly one finding, one
// no-evidence explanation, or one ambiguous-scope refusal.
func explanationRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/supply-chain/impact/explain", Query: map[string]string{
		"advisory_id":    args.String("advisory_id"),
		"cve_id":         args.String("cve_id"),
		"finding_id":     args.String("finding_id"),
		"image_ref":      args.String("image_ref"),
		"package_id":     args.String("package_id"),
		"repository_id":  args.String("repository_id"),
		"service_id":     args.String("service_id"),
		"subject_digest": args.String("subject_digest"),
		"workload_id":    args.String("workload_id"),
	}}
}

// aggregateCountRequest maps count_supply_chain_impact_findings to the cheap
// summary route GET /api/v0/supply-chain/impact/findings/count, which
// query.SupplyChainHandler.countImpactFindings serves.
//
// The route carries the same filter set as the inventory below and neither a
// limit nor an offset: the handler returns whole-scope totals by priority
// bucket and severity, and it reads no paging key at all. Adding one for
// symmetry with the listing would not cap anything the endpoint enforces.
func aggregateCountRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/supply-chain/impact/findings/count", Query: aggregateFilterQuery(args)}
}

// aggregateInventoryRequest maps get_supply_chain_impact_inventory to the
// grouped summary route GET /api/v0/supply-chain/impact/inventory, which
// query.SupplyChainHandler.impactInventory serves.
//
// The group_by fallback to impact_status is not what makes an omitted
// dimension work: the handler independently defaults an empty group_by to
// impact_status and rejects anything outside impact_status, priority_bucket,
// severity, repository_id, and ecosystem with a 400. The fallback keeps the
// selected wire value stable, so changing it to another dimension would
// change the grouping every ungrouped caller receives. limit defaults to 100
// and offset to 0, matching the handler's own defaults; the handler owns the
// 1-500 limit bound and the 10000 offset ceiling.
func aggregateInventoryRequest(args routecontract.Arguments) routecontract.Request {
	groupBy := args.String("group_by")
	if groupBy == "" {
		groupBy = "impact_status"
	}
	query := aggregateFilterQuery(args)
	query["group_by"] = groupBy
	query["limit"] = strconv.Itoa(args.IntOr("limit", 100))
	query["offset"] = strconv.Itoa(args.IntOr("offset", 0))
	return routecontract.Request{Method: "GET", Path: "/api/v0/supply-chain/impact/inventory", Query: query}
}

// aggregateFilterQuery builds the eighteen-key filter set the count and
// inventory routes share. Both routes require nothing: a filter dropped here
// returns 200 over a wider scope and drops that key from the scope block the
// response echoes back, rather than failing loudly the way the listing's
// missing limit or scope anchor does.
func aggregateFilterQuery(args routecontract.Arguments) map[string]string {
	query := map[string]string{
		"advisory_id":        args.String("advisory_id"),
		"cve_id":             args.String("cve_id"),
		"ecosystem":          args.String("ecosystem"),
		"environment":        args.String("environment"),
		"ghsa_id":            args.String("ghsa_id"),
		"image_ref":          args.String("image_ref"),
		"impact_status":      args.String("impact_status"),
		"min_priority_score": strconv.Itoa(args.IntOr("min_priority_score", 0)),
		"osv_id":             args.String("osv_id"),
		"package_id":         args.String("package_id"),
		"priority_bucket":    args.String("priority_bucket"),
		"profile":            args.String("profile"),
		"repository_id":      args.String("repository_id"),
		"service_id":         args.String("service_id"),
		"severity":           args.String("severity"),
		"subject_digest":     args.String("subject_digest"),
		"suppression_state":  args.String("suppression_state"),
		"workload_id":        args.String("workload_id"),
	}
	if encoded := boolStr(args, "include_suppressed"); encoded != "" {
		query["include_suppressed"] = encoded
	}
	return query
}

// boolStr returns the bool argument encoded as the string "true" or "false"
// when the caller set it, and the empty string otherwise. routecontract.
// Arguments.BoolOr cannot express this: it collapses "absent" into the
// caller's fallback, which is indistinguishable from an explicit false. The
// findings and aggregate routes need the three-state distinction so
// include_suppressed stays unset -- and the handler's documented false
// default applies -- rather than the route forcing an explicit value onto
// every request.
func boolStr(args routecontract.Arguments, key string) string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return ""
	}
	if v, ok := raw.(bool); ok {
		return strconv.FormatBool(v)
	}
	return ""
}
