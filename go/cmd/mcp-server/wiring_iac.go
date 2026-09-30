// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/iac"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func newMCPQueryIaCHandlerWithReadStore(
	reader db.Queryer,
	contentReader query.ContentStore,
	graph query.GraphQuery,
	profile query.QueryProfile,
) *query.IaCHandler {
	return &query.IaCHandler{
		Content:      contentReader,
		Reachability: iac.NewPostgresIaCReachabilityStoreWithReadStore(reader),
		Management:   iac.NewPostgresIaCManagementStoreWithReadStore(reader),
		Inventory:    iac.NewPostgresIaCInventoryStoreWithReadStore(reader),
		Graph:        graph,
		Profile:      profile,
	}
}
