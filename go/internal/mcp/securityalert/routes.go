// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalerttools

import (
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// Route selects the internal HTTP request for a security-alert
// reconciliation tool without executing it. It reports handled only for the
// three tools this package owns.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "list_security_alert_reconciliations":
		return reconciliationsRequest(args), true
	case "count_security_alert_reconciliations":
		return aggregateCountRequest(args), true
	case "get_security_alert_reconciliation_inventory":
		return aggregateInventoryRequest(args), true
	default:
		return routecontract.Request{}, false
	}
}

// reconciliationsRequest maps list_security_alert_reconciliations to the
// cursor-paged read-only route
// GET /api/v0/supply-chain/security-alerts/reconciliations, which
// query.SupplyChainHandler.listSecurityAlertReconciliations serves. Rows
// carry Eshu-owned dependency evidence under eshu_package alongside the
// provider's own alert state. limit is required by the handler and defaults
// to 50 here when the caller omits it; the handler also requires one of
// repository_id, provider, package_id, cve_id, or ghsa_id as a scope anchor,
// except that an empty scoped-token grant is answered with an empty page
// before the anchor is checked.
func reconciliationsRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/supply-chain/security-alerts/reconciliations", Query: map[string]string{
		"after_reconciliation_id": args.String("after_reconciliation_id"),
		"cve_id":                  args.String("cve_id"),
		"ghsa_id":                 args.String("ghsa_id"),
		"limit":                   strconv.Itoa(args.IntOr("limit", 50)),
		"package_id":              args.String("package_id"),
		"provider":                args.String("provider"),
		"provider_state":          args.String("provider_state"),
		"reconciliation_status":   args.String("reconciliation_status"),
		"repository_id":           args.String("repository_id"),
	}}
}

// aggregateCountRequest maps count_security_alert_reconciliations to the
// cheap summary route
// GET /api/v0/supply-chain/security-alerts/reconciliations/count, which
// query.SupplyChainHandler.countSecurityAlertReconciliations serves. The
// route carries the same seven filters as the inventory below and no paging
// key: the handler returns whole-scope totals and reads no limit or offset.
func aggregateCountRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/supply-chain/security-alerts/reconciliations/count", Query: aggregateFilterQuery(args)}
}

// aggregateInventoryRequest maps get_security_alert_reconciliation_inventory
// to the grouped summary route
// GET /api/v0/supply-chain/security-alerts/reconciliations/inventory, which
// query.SupplyChainHandler.securityAlertReconciliationInventory serves. An
// omitted group_by falls back to reconciliation_status, matching the
// handler's own default; limit defaults to 100 and offset to 0.
func aggregateInventoryRequest(args routecontract.Arguments) routecontract.Request {
	groupBy := args.String("group_by")
	if groupBy == "" {
		groupBy = "reconciliation_status"
	}
	query := aggregateFilterQuery(args)
	query["group_by"] = groupBy
	query["limit"] = strconv.Itoa(args.IntOr("limit", 100))
	query["offset"] = strconv.Itoa(args.IntOr("offset", 0))
	return routecontract.Request{Method: "GET", Path: "/api/v0/supply-chain/security-alerts/reconciliations/inventory", Query: query}
}

// aggregateFilterQuery builds the seven-key filter set the count and the
// inventory routes share. Neither route requires anything: a filter dropped
// here returns 200 over a wider scope and drops that key from the scope
// block the response echoes back, rather than failing loudly the way the
// listing's missing limit does.
func aggregateFilterQuery(args routecontract.Arguments) map[string]string {
	return map[string]string{
		"cve_id":                args.String("cve_id"),
		"ghsa_id":               args.String("ghsa_id"),
		"package_id":            args.String("package_id"),
		"provider":              args.String("provider"),
		"provider_state":        args.String("provider_state"),
		"reconciliation_status": args.String("reconciliation_status"),
		"repository_id":         args.String("repository_id"),
	}
}
