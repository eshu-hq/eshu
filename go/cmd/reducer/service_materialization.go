// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// platformGraphLockerForReducer builds the Postgres-backed platform graph lock
// the deployment-mapping handler uses, or nil when the database does not expose a
// transaction beginner.
func platformGraphLockerForReducer(database postgres.ExecQueryer) reducer.PlatformGraphLocker {
	beginner := reducerBeginner(database)
	if beginner == nil {
		return nil
	}
	return postgres.PlatformGraphLocker{DB: beginner}
}

// reducerBeginner adapts the shared reducer database into the transaction beginner
// the materialization and lock writers need, or nil when the database does not
// support transactions.
func reducerBeginner(database postgres.ExecQueryer) postgres.Beginner {
	if beginner, ok := database.(postgres.Beginner); ok {
		return beginner
	}
	return nil
}

// serviceMaterializationWriterFor builds the additive per-service ownership
// generation lineage writer (#1943) over the shared reducer database. When the
// database does not expose a transaction beginner the writer is nil, so the
// service-catalog correlation handler keeps its existing behavior unchanged.
func serviceMaterializationWriterFor(database postgres.ExecQueryer) reducer.ServiceMaterializationWriter {
	beginner := reducerBeginner(database)
	if beginner == nil {
		return nil
	}
	return reducer.PostgresServiceMaterializationWriter{
		DB: postgres.ServiceMaterializationBeginner{Beginner: beginner},
	}
}

// serviceDocumentationEvidenceLoaderFor builds the service-scoped documentation
// evidence loader (#1988) over the shared reducer database. It backs the docs
// evidence family from the active-generation documentation facts in
// fact_records; the family is purely additive, so wiring it never blocks the
// prior service evidence families.
func serviceDocumentationEvidenceLoaderFor(
	database postgres.ExecQueryer,
) reducer.ServiceScopedDocumentationEvidenceLoader {
	if database == nil {
		return nil
	}
	return postgres.NewServiceDocumentationEvidenceLoader(database)
}

// serviceIncidentEvidenceLoaderFor builds the service-scoped incident routing
// evidence loader (#1989) over the shared reducer database. It resolves provider
// service ids to Eshu catalog service ids through durable reducer correlations
// and remains purely additive: wiring it never blocks the prior service evidence
// families.
func serviceIncidentEvidenceLoaderFor(
	database postgres.ExecQueryer,
) reducer.ServiceScopedIncidentEvidenceLoader {
	if database == nil {
		return nil
	}
	return postgres.NewServiceIncidentEvidenceLoader(database)
}
