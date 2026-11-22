// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The reducer fact owner probe (#11213) needs a plain db.Queryer. These four
// transaction wrappers reach reducer fact writers but own a QueryContext of a
// different shape, or none, so each lends the probe a read on its transaction
// through factwrite.OwnerProbeQueryer. TestFactOwnerWriteHandlesCanProbe pins
// them and TestEveryTransactionWrapperIsAFactOwnerWriteHandle fails when a new
// db.Transaction wrapper is added without being listed.

// QueryFactOwners lends the owner probe a read on this transaction.
func (tx containerImageIdentityTx) QueryFactOwners(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return tx.tx.QueryContext(ctx, query, args...)
}

// QueryFactOwners lends the owner probe a read on this transaction.
func (t awsCloudRuntimeDriftAdmissionTx) QueryFactOwners(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return t.tx.QueryContext(ctx, query, args...)
}

// QueryFactOwners lends the owner probe a read on this transaction.
func (t serviceMaterializationTx) QueryFactOwners(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return t.tx.QueryContext(ctx, query, args...)
}

// QueryFactOwners lends the owner probe a read on this transaction. Its
// QueryContext returns the package-specific SupplyChainImpactRows, so it cannot
// serve as a db.Queryer.
func (t supplyChainImpactTx) QueryFactOwners(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return t.tx.QueryContext(ctx, query, args...)
}
