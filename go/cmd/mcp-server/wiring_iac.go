// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func newMCPQueryIaCHandler(
	db *sql.DB,
	contentReader query.ContentStore,
	graph query.GraphQuery,
	profile query.QueryProfile,
) *query.IaCHandler {
	return &query.IaCHandler{
		Content:      contentReader,
		Reachability: query.NewPostgresIaCReachabilityStore(db),
		Management:   query.NewPostgresIaCManagementStore(db),
		Inventory:    query.NewPostgresIaCInventoryStore(db),
		Graph:        graph,
		Profile:      profile,
	}
}
