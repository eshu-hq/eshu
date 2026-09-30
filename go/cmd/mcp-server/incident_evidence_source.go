// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"database/sql"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/serviceintelhttp"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// newIncidentEvidenceSourceWithReadStore builds the durable incident evidence source for the
// service intelligence report's incidents_support section: a catalog-service-id
// resolver plus an incident evidence loader, both over the shared Postgres query
// surface. The logger surfaces ambiguous-catalog and load failures to operators.
func newIncidentEvidenceSourceWithReadStore(reader db.Queryer, logger *slog.Logger) serviceintelhttp.IncidentEvidenceSource {
	return serviceintelhttp.NewDurableIncidentEvidenceSource(
		pgstatus.NewServiceCatalogIDResolver(reader),
		pgstatus.NewServiceIncidentEvidenceLoader(reader),
		logger,
	)
}

// newSupplyChainEvidenceSource builds the durable supply-chain evidence source
// for the service intelligence report's supply_chain section over the shared
// Postgres aggregate read model. The logger surfaces load failures to operators.
func newSupplyChainEvidenceSource(db *sql.DB, logger *slog.Logger) serviceintelhttp.SupplyChainEvidenceSource {
	return newSupplyChainEvidenceSourceWithReadStore(pgstatus.NewSQLReadStore(db), logger)
}

func newSupplyChainEvidenceSourceWithReadStore(reader db.ReadStore, logger *slog.Logger) serviceintelhttp.SupplyChainEvidenceSource {
	return serviceintelhttp.NewDurableSupplyChainEvidenceSource(
		impact.NewPostgresAggregateStoreWithReadStore(reader),
		logger,
	)
}
