// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"database/sql"

	"go.opentelemetry.io/otel"

	"github.com/eshu-hq/eshu/go/internal/query"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func newGovernanceAuditStore(
	db *sql.DB,
	instruments *telemetry.Instruments,
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
	store := pgstatus.NewGovernanceAuditStore(governanceAuditDB)
	return store
}
