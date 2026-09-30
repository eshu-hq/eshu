// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package drift

import (
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel"
)

// NewPostgresFindingStoreWithReadStore routes finding and count queries through
// a guarded query-only port while retaining the existing Postgres query signal.
func NewPostgresFindingStoreWithReadStore(reader db.Queryer) *PostgresFindingStore {
	instrumented := &postgres.InstrumentedQueryer{
		Inner: reader, Tracer: otel.Tracer(telemetry.DefaultSignalName),
		StoreName: "terraform_config_state_drift",
	}
	return &PostgresFindingStore{store: postgres.NewTerraformConfigStateDriftFindingReader(instrumented)}
}
