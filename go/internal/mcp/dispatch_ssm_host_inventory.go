// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

// ssmHostInventoryRoute maps the SSM host inventory readback tool to its
// internal GET route. It forwards only the bounded, non-sensitive filter
// parameters; the handler validates status against the closed set and gates
// the capability by runtime profile.
func ssmHostInventoryRoute(toolName string, args map[string]any) (*routecontract.Request, bool) {
	if toolName != "list_ssm_host_inventory" {
		return nil, false
	}
	return &routecontract.Request{
		Method: "GET",
		Path:   "/api/v0/cloud/ssm-host-inventory",
		Query:  ssmHostInventoryQuery(args),
	}, true
}

func ssmHostInventoryQuery(args map[string]any) map[string]string {
	query := map[string]string{}
	for _, key := range []string{
		"instance_id",
		"account_id",
		"region",
		"status",
		"cursor",
	} {
		if value := str(args, key); value != "" {
			query[key] = value
		}
	}
	if limit := intOr(args, "limit", 10); limit > 0 {
		query["limit"] = strconv.Itoa(limit)
	}
	return query
}
