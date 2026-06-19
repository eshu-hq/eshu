package main

import (
	"database/sql"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/serviceintelhttp"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// newIncidentEvidenceSource builds the durable incident evidence source for the
// service intelligence report's incidents_support section: a catalog-service-id
// resolver plus an incident evidence loader, both over the shared Postgres query
// surface. The logger surfaces ambiguous-catalog and load failures to operators.
func newIncidentEvidenceSource(db *sql.DB, logger *slog.Logger) serviceintelhttp.IncidentEvidenceSource {
	queryer := pgstatus.SQLQueryer{DB: db}
	return serviceintelhttp.NewDurableIncidentEvidenceSource(
		pgstatus.NewServiceCatalogIDResolver(queryer),
		pgstatus.NewServiceIncidentEvidenceLoader(queryer),
		logger,
	)
}

// newSupplyChainEvidenceSource builds the durable supply-chain evidence source
// for the service intelligence report's supply_chain section over the shared
// Postgres aggregate read model. The logger surfaces load failures to operators.
func newSupplyChainEvidenceSource(db *sql.DB, logger *slog.Logger) serviceintelhttp.SupplyChainEvidenceSource {
	return serviceintelhttp.NewDurableSupplyChainEvidenceSource(
		query.NewPostgresSupplyChainImpactAggregateStore(db),
		logger,
	)
}
