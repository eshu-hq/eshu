// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"database/sql"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	producerreadiness "github.com/eshu-hq/eshu/go/internal/storage/postgres/readiness/producer"
)

// supplyChainImpactAttachmentNotReadyClass is the #11105 failure class the
// impact pass defers with while its own attachment is in flight.
const supplyChainImpactAttachmentNotReadyClass = supplychaincore.SBOMAttachmentNotReadyFailureClass

// wireAttachmentReadiness wires the #11105 attachment readiness port the way
// cmd/reducer does (producerreadiness.ImpactStore over the same database).
func wireAttachmentReadiness(handler *supplychaincore.SupplyChainImpactHandler, db *sql.DB) {
	handler.AttachmentReadiness = producerreadiness.ImpactStore{DB: postgres.SQLDB{DB: db}}
}
