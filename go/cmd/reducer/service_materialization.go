// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/lock"
)

// platformGraphLockerForReducer builds the Postgres-backed platform graph lock
// the deployment-mapping handler uses, or nil when the database does not expose a
// transaction beginner.
func platformGraphLockerForReducer(database db.ExecQueryer) reducer.PlatformGraphLocker {
	beginner := reducerBeginner(database)
	if beginner == nil {
		return nil
	}
	return lockstore.PlatformGraphLocker{DB: beginner}
}

// reducerBeginner adapts the shared reducer database into the transaction beginner
// the materialization and lock writers need, or nil when the database does not
// support transactions.
func reducerBeginner(database db.ExecQueryer) db.Beginner {
	if beginner, ok := database.(db.Beginner); ok {
		return beginner
	}
	return nil
}

// serviceMaterializationWriterFor builds the additive per-service ownership
// generation lineage writer (#1943) over the shared reducer database. When the
// database does not expose a transaction beginner the writer is nil, so the
// service-catalog correlation handler keeps its existing behavior unchanged.
func serviceMaterializationWriterFor(database db.ExecQueryer) reducer.ServiceMaterializationWriter {
	beginner := reducerBeginner(database)
	if beginner == nil {
		return nil
	}
	return reducer.PostgresServiceMaterializationWriter{
		DB: postgres.ServiceMaterializationBeginner{Beginner: beginner},
	}
}

// containerImageIdentityWriterFor builds the digest-v3 identity support writer
// over the shared reducer database. Each publication installs one complete,
// immutable support set and moves active_set_id in the same statement after
// validating the exact claim and activation epoch. A database without
// transaction support leaves the domain unwired rather than silently
// publishing without convergence.
func containerImageIdentityWriterFor(
	database db.ExecQueryer,
) reducer.ContainerImageIdentityWriter {
	beginner := reducerBeginner(database)
	if database == nil || beginner == nil {
		return nil
	}
	return reducer.PostgresContainerImageIdentitySupportWriter{
		ActivationLookup:  postgres.NewContainerImageIdentityScopeStateStore(database),
		HeldSupportLoader: postgres.NewContainerImageIdentityHeldSupportStore(database),
		ClaimedExecer:     postgres.ContainerImageIdentityClaimedExecer{DB: database},
	}
}

// serviceDocumentationEvidenceLoaderFor builds the service-scoped documentation
// evidence loader (#1988) over the shared reducer database. It backs the docs
// evidence family from the active-generation documentation facts in
// fact_records; the family is purely additive, so wiring it never blocks the
// prior service evidence families.
func serviceDocumentationEvidenceLoaderFor(
	database db.ExecQueryer,
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
	database db.ExecQueryer,
) reducer.ServiceScopedIncidentEvidenceLoader {
	if database == nil {
		return nil
	}
	return postgres.NewServiceIncidentEvidenceLoader(database)
}
