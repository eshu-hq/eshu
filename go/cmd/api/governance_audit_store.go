// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"database/sql"
	"log/slog"

	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// newGovernanceAuditStore builds the Postgres governance audit store the API
// shares between its handlers. logger is the API's structured logger; the
// store's per-List unknown-enum warn (#6574) goes through it so the line lands
// in the same JSON log as every other API signal, not on Go's default text
// handler. A nil logger falls back to slog.Default.
func newGovernanceAuditStore(
	db *sql.DB,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) query.GovernanceAuditSummaryReader {
	if db == nil {
		return nil
	}
	governanceAuditDB := pgstatus.ExecQueryer(pgstatus.SQLDB{DB: db})
	if instruments != nil {
		governanceAuditDB = &pgstatus.InstrumentedDB{
			Inner:       governanceAuditDB,
			Tracer:      otel.Tracer(telemetry.DefaultSignalName),
			Instruments: instruments,
			StoreName:   "governance_audit",
		}
	}
	return pgstatus.NewGovernanceAuditStore(governanceAuditDB).WithLogger(logger)
}
